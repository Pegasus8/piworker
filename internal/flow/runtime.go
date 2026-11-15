package flow

import (
	"context"
	"fmt"
	flowvars "github.com/Pegasus8/piworker/internal/vars"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
)

// Default retry configuration values.
const (
	DefaultMaxRetries  = 3
	DefaultInitialWait = 100 * time.Millisecond
	DefaultMaxWait     = 5 * time.Second
	DefaultMultiplier  = 2.0
)

// Default concurrency configuration for node execution.
const (
	// DefaultMaxConcurrency bounds how many nodes execute simultaneously per flow.
	DefaultMaxConcurrency = 16
	// DefaultExecBuffer is the buffer size of the trigger seed channels.
	DefaultExecBuffer = 256
	// DefaultStopTimeout bounds how long Stop waits for the flow's goroutines.
	DefaultStopTimeout = 5 * time.Second
	// DefaultNodeQueueLimit caps how many messages may wait for one node. A
	// node whose backlog is full has further messages dropped (with a warning
	// and a dropped-message metric) instead of stalling the rest of the flow.
	DefaultNodeQueueLimit = 1000
)

// RetryConfig configures retry behavior for node execution.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts (0 = no retries).
	MaxRetries int
	// InitialWait is the initial wait time before the first retry.
	InitialWait time.Duration
	// MaxWait is the maximum wait time between retries.
	MaxWait time.Duration
	// Multiplier is the exponential backoff multiplier.
	Multiplier float64
}

// DefaultRetryConfig returns a RetryConfig with sensible defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:  DefaultMaxRetries,
		InitialWait: DefaultInitialWait,
		MaxWait:     DefaultMaxWait,
		Multiplier:  DefaultMultiplier,
	}
}

// Node execution phases reported by a NodeEventHook. The string values match the
// events package phases so a recorder can forward them without translation.
const (
	NodePhaseRunning = "running"
	NodePhaseSuccess = "success"
	NodePhaseError   = "error"
	// NodePhaseRunFinished is a run-level (not node-level) event emitted once, when
	// all in-flight work for a correlation ID drains. NodeID is empty; only FlowID
	// and CorrID are set. It lets the recorder finalize a run deterministically
	// instead of guessing via an idle timeout.
	NodePhaseRunFinished = "run-finished"
)

// NodeEvent is a structured observation of a single node execution. Unlike the
// legacy before/after hooks it carries the node type, correlation ID, timing,
// the input message and (on success) the outputs, which is everything the
// observability layer needs to persist history and stream live state to the
// editor — including attributing process-debug output to a canvas node.
type NodeEvent struct {
	FlowID   string
	NodeID   string
	NodeType string
	CorrID   string
	// Actor is the run's "actor" meta (who started it, e.g. "token:bot"),
	// copied by the executor before the node runs so observers never read
	// Input's meta while the node may still change it. "" when unknown.
	Actor string
	// FlowVersion is the stored version of the definition the runtime runs
	// (Flow.Version when the runtime was built; 0 when unknown), so the
	// history records which version each run ran (ADR-0022).
	FlowVersion int
	Phase       string // NodePhaseRunning | NodePhaseSuccess | NodePhaseError
	Input       *types.Message
	Outputs     []*types.Message
	Err         error
	Duration    time.Duration
}

// NodeEventHook receives NodeEvents. It runs inline on the node executor
// goroutine while a semaphore slot is held, so it MUST NOT block: implementations
// are expected to do a non-blocking hand-off (e.g. a buffered channel send) and
// return immediately. Blocking here directly stalls node throughput.
type NodeEventHook func(NodeEvent)

// FlowRuntime manages the execution of a single flow.
// Each active flow runs in its own goroutine with message passing via channels.
type FlowRuntime struct {
	flow *Flow
	// version is flow.Version when the runtime was built: what its node
	// events report, whatever later happens to the caller's copy.
	version int
	router  *MessageRouter
	logger  zerolog.Logger

	// Node instances keyed by node ID
	nodes map[string]node.Node

	// warnings are the deploy's warnings (deployWarnings), set before Start.
	warnings []DeployWarning

	// Trigger nodes that need to be started
	triggers map[string]node.TriggerNode

	// Node definitions keyed by node ID. Immutable for the runtime's lifetime,
	// so executeNode can fetch a node's Type/Outputs without taking the flow's
	// RWLock or linearly scanning the node slice on every message.
	nodeDefs map[string]*Node

	// Per-node FIFO queues. Each node drains its own queue serially
	// (preserving message order for stateful nodes like batch/dedupe) while
	// different nodes run concurrently. Pushing never blocks the dispatcher, so
	// one slow node can't stall unrelated branches; a full queue drops.
	queues     map[string]*nodeQueue
	queueLimit int

	// droppedMessages counts messages discarded by this runtime (node queue
	// overflow, router send timeouts, disabled/unknown targets).
	droppedMessages atomic.Int64

	// sem bounds how many nodes execute concurrently across the entire flow.
	sem            chan struct{}
	maxConcurrency int

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// State management
	mu      sync.RWMutex
	running bool
	state   FlowState

	// observer receives structured per-execution NodeEvents (running/success/
	// error). It is the bridge to the observability layer (history + live SSE).
	observer NodeEventHook

	// runInflight counts the messages still in flight per correlation ID (a "run").
	// Seeded when a trigger emits, incremented per routed child in executeNode, and
	// decremented when a message finishes. Reaching zero emits NodePhaseRunFinished.
	runInflight map[string]int
	runMu       sync.Mutex

	// Retry configuration for node execution
	retryConfig RetryConfig

	// Error channel for runtime errors
	errors chan error

	// Wait group for goroutine synchronization
	wg sync.WaitGroup

	// stopTimeout bounds how long Stop waits for wg. drained is closed once
	// every goroutine of the current run has exited; until then the runtime
	// may still be executing nodes even though it reports not running.
	stopTimeout time.Duration
	drained     chan struct{}

	// calls tracks runs started by Call (keyed by correlation ID). It lives on
	// r.ctx so a reply node can find it (ReplyFromContext).
	calls *callRegistry

	// lenientConfig skips the deploy-time schema check (Flow.CheckConfig), so
	// nodes clamp or default mismatched values as before it existed. Only
	// RuntimeManager.Restore sets it.
	lenientConfig bool

	// tools are the tools this flow publishes, computed by the risk analysis
	// at deploy (see validateDeployable) and immutable afterwards.
	tools []ToolInfo

	// nodeContext, when set, wraps the context every node of the runtime
	// gets (trigger Start and Process); see WithNodeContext.
	nodeContext func(context.Context) context.Context

	// flowVars are the per-flow variable stores the flow's own is taken
	// from (WithFlowVariables; flowvars.DefaultFlows when not set).
	flowVars *flowvars.FlowStores

	// noMetrics keeps the runtime out of the process-wide metrics (see
	// withoutMetrics).
	noMetrics bool

	// routeGate, when set, is asked before each message is routed (see
	// withRouteGate).
	routeGate func(msg *types.Message, copies int) bool
}

// NewFlowRuntime creates a new FlowRuntime for the given flow.
func NewFlowRuntime(f *Flow, registry *node.Registry, opts ...RuntimeOption) (*FlowRuntime, error) {
	logger := zerolog.Nop()

	rt := &FlowRuntime{
		flow:           f,
		version:        f.Version,
		logger:         logger,
		nodes:          make(map[string]node.Node),
		triggers:       make(map[string]node.TriggerNode),
		nodeDefs:       make(map[string]*Node),
		running:        false,
		state:          FlowStateInactive,
		retryConfig:    DefaultRetryConfig(),
		maxConcurrency: DefaultMaxConcurrency,
		queueLimit:     DefaultNodeQueueLimit,
		stopTimeout:    DefaultStopTimeout,
		errors:         make(chan error, 100),
		runInflight:    make(map[string]int),
		calls:          newCallRegistry(DefaultCallExpiryGrace),
	}

	// Apply options
	for _, opt := range opts {
		opt(rt)
	}

	// Mistyped config values fail the deploy with their path, before any node
	// is instantiated with a silently defaulted value.
	if !rt.lenientConfig {
		if err := f.CheckConfig(registry); err != nil {
			return nil, err
		}
	}

	// Instantiate and validate every enabled node (the collector Lint
	// shares); the first node that fails is the deploy error.
	instances, findings := instantiateNodes(f, registry, nil)
	if err := firstError(findings); err != nil {
		closeInstances(instances)
		return nil, err
	}
	for i := range f.Nodes {
		nodeInstance, ok := instances[i]
		if !ok {
			continue // disabled
		}
		n := &f.Nodes[i]

		n.Inputs, n.Outputs = nodeInstance.Ports()
		rt.nodes[n.ID] = nodeInstance
		rt.nodeDefs[n.ID] = n

		// Track trigger nodes separately
		if trigger, ok := nodeInstance.(node.TriggerNode); ok {
			rt.triggers[n.ID] = trigger
		}
	}

	// Create the message router. Connections touching a disabled (never
	// instantiated) node are left out, so nothing is routed to a node that
	// cannot execute and the run accounting (targetCount) never counts it.
	active := make([]Connection, 0, len(f.Connections))
	for _, c := range f.Connections {
		_, srcOK := rt.nodes[c.SourceNode]
		_, dstOK := rt.nodes[c.TargetNode]
		if srcOK && dstOK {
			active = append(active, c)
		}
	}
	routerOpts := []RouterOption{WithDropHandler(func(m RoutedMessage) {
		rt.releaseDropped(m, "routing timed out (the flow is overloaded)")
	})}
	if rt.noMetrics {
		routerOpts = append(routerOpts, withoutRouterMetrics())
	}
	rt.router = NewMessageRouter(active, rt.logger, routerOpts...)

	return rt, nil
}

// Start begins executing the flow.
// This method is non-blocking; the flow runs in background goroutines. Every
// trigger's Start is called synchronously, so when Start returns nil all
// triggers are running (and can e.g. be injected into immediately). If any
// trigger fails to start, the triggers already started are stopped, the
// runtime is torn down and the error is returned.
func (r *FlowRuntime) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return ErrFlowAlreadyRunning
	}

	// Use background context so the flow survives HTTP request completion.
	// The parent ctx is only used for initial validation, not for runtime lifecycle.
	nodeCtx := withCallRegistry(context.Background(), r.calls)
	if r.nodeContext != nil {
		if wrapped := r.nodeContext(nodeCtx); wrapped != nil {
			nodeCtx = wrapped
		}
	}
	// The flow's persistent variable store, unless the node context brings
	// its own (a sandboxed run must not load or touch the real one).
	if flowvars.FlowFromContext(nodeCtx) == nil {
		stores := r.flowVars
		if stores == nil {
			stores = flowvars.DefaultFlows
		}
		local, err := stores.Get(r.flow.ID)
		if err != nil {
			r.mu.Unlock()
			return fmt.Errorf("load flow variables: %w", err)
		}
		nodeCtx = flowvars.WithFlowStore(nodeCtx, local)
	}
	r.ctx, r.cancel = context.WithCancel(nodeCtx)
	r.calls.start()
	r.drained = make(chan struct{})
	r.running = true
	r.state = FlowStateRunning
	r.mu.Unlock()

	r.logger.Info().
		Str("flowID", r.flow.ID).
		Str("flowName", r.flow.Name).
		Msg("Starting flow runtime")

	// Initialise the concurrency limiter and one serial executor per node.
	r.sem = make(chan struct{}, r.maxConcurrency)
	r.queues = make(map[string]*nodeQueue, len(r.nodes))
	for nodeID := range r.nodes {
		q := newNodeQueue(r.queueLimit)
		r.queues[nodeID] = q
		r.wg.Add(1)
		go r.nodeExecutor(nodeID, q)
	}

	// Start the dispatcher that fans routed messages out to per-node executors.
	r.wg.Add(1)
	go r.dispatch()

	// Start all trigger nodes, in a stable order, synchronously.
	triggerIDs := make([]string, 0, len(r.triggers))
	for nodeID := range r.triggers {
		triggerIDs = append(triggerIDs, nodeID)
	}
	sort.Strings(triggerIDs)

	started := make([]string, 0, len(triggerIDs))
	for _, nodeID := range triggerIDs {
		trigger := r.triggers[nodeID]
		seedChan := r.startSeeder(nodeID)

		r.logger.Debug().
			Str("nodeID", nodeID).
			Msg("Starting trigger node")

		if err := trigger.Start(r.ctx, seedChan); err != nil {
			nodeType := "trigger"
			if def := r.nodeDefs[nodeID]; def != nil {
				nodeType = def.Type
			}
			r.logger.Error().
				Err(err).
				Str("flowID", r.flow.ID).
				Str("nodeID", nodeID).
				Msg("Trigger node failed to start; aborting flow start")
			r.shutdown(started)
			return NewNodeExecutionError(nodeID, nodeType, err)
		}
		started = append(started, nodeID)
	}

	return nil
}

// startSeeder returns the channel a trigger emits into. A goroutine reads it,
// assigns the correlation ID and counts the message's downstream targets
// (starting a new run) before forwarding to the router. Assigning the
// correlation ID here — not later in executeNode — lets the whole cascade be
// tracked under one run ID.
func (r *FlowRuntime) startSeeder(nodeID string) chan *types.Message {
	realChan := r.router.GetInputChannel(nodeID)
	seedChan := make(chan *types.Message, DefaultExecBuffer)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		for {
			select {
			case <-r.ctx.Done():
				return
			case msg, ok := <-seedChan:
				if !ok {
					return
				}
				corrID := r.ensureCorrelationID(msg)
				n := r.targetCount(nodeID, msg.SourcePort)
				if !r.routeAllowed(msg, n) {
					r.calls.runFinished(corrID)
					continue
				}
				if n > 0 {
					r.runAdd(corrID, n)
				} else {
					// Nothing will execute for this run: it is already over.
					r.calls.runFinished(corrID)
				}
				select {
				case <-r.ctx.Done():
					return
				case realChan <- msg:
				}
			}
		}
	}()
	return seedChan
}

// Stop gracefully stops the flow execution.
func (r *FlowRuntime) Stop() error {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return ErrFlowNotRunning
	}
	r.mu.Unlock()

	r.logger.Info().
		Str("flowID", r.flow.ID).
		Msg("Stopping flow runtime")

	ids := make([]string, 0, len(r.triggers))
	for nodeID := range r.triggers {
		ids = append(ids, nodeID)
	}
	sort.Strings(ids)
	r.shutdown(ids)
	return nil
}

// shutdown cancels the runtime, stops the given triggers, closes the router and
// waits (bounded) for every goroutine to exit before marking it stopped.
func (r *FlowRuntime) shutdown(triggerIDs []string) {
	// Release every caller still waiting for a called run ("flow stopped"),
	// then cancel the context to signal all goroutines to stop. In this order
	// a node that returns ctx.Err() cannot end a pending call with its own
	// error first.
	r.calls.stop()
	r.cancel()

	for _, nodeID := range triggerIDs {
		r.logger.Debug().
			Str("nodeID", nodeID).
			Msg("Stopping trigger node")

		if err := r.triggers[nodeID].Stop(); err != nil {
			r.logger.Error().
				Err(err).
				Str("nodeID", nodeID).
				Msg("Error stopping trigger node")
		}
	}

	// Close the router to stop message processing
	r.router.Close()

	// Wait for all goroutines to finish with a timeout. Close nodes only
	// after they have, so a node is never closed while still processing;
	// drained is closed last, once the instance is completely gone.
	r.mu.RLock()
	drained := r.drained
	r.mu.RUnlock()
	go func() {
		r.wg.Wait()
		r.closeNodes()
		close(drained)
	}()

	select {
	case <-drained:
		r.logger.Debug().Msg("All goroutines stopped gracefully")
	case <-time.After(r.stopTimeout):
		r.logger.Warn().
			Str("flowID", r.flow.ID).
			Dur("timeout", r.stopTimeout).
			Msg("Timeout waiting for flow goroutines to stop; the instance keeps draining in the background and the flow cannot be redeployed until it does")
		go func() {
			<-drained
			r.logger.Info().
				Str("flowID", r.flow.ID).
				Msg("Stopped flow instance finished draining")
		}()
	}

	r.mu.Lock()
	r.running = false
	r.state = FlowStateInactive
	r.mu.Unlock()
}

// closeNodes calls Close on every node (trigger or not) that implements
// node.Closer, logging failures. It must only run once executors have drained.
func (r *FlowRuntime) closeNodes() {
	ids := make([]string, 0, len(r.nodes))
	for nodeID := range r.nodes {
		ids = append(ids, nodeID)
	}
	sort.Strings(ids)
	for _, nodeID := range ids {
		c, ok := r.nodes[nodeID].(node.Closer)
		if !ok {
			continue
		}
		if err := c.Close(); err != nil {
			r.logger.Error().
				Err(err).
				Str("flowID", r.flow.ID).
				Str("nodeID", nodeID).
				Msg("Error closing node")
		}
	}
}

// Drained reports whether every goroutine of the last started instance has
// exited (true for a runtime that was never started). After a Stop that timed
// out it stays false until the stragglers return.
func (r *FlowRuntime) Drained() bool {
	r.mu.RLock()
	ch := r.drained
	r.mu.RUnlock()
	if ch == nil {
		return true
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// State returns the current state of the flow runtime.
func (r *FlowRuntime) State() FlowState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state
}

// IsRunning returns whether the flow is currently running.
func (r *FlowRuntime) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}

// Errors returns the error channel for monitoring runtime errors.
func (r *FlowRuntime) Errors() <-chan error {
	return r.errors
}

// Flow returns the flow being executed.
func (r *FlowRuntime) Flow() *Flow {
	return r.flow
}

// InjectableNode is an interface for nodes that can be manually triggered.
type InjectableNode interface {
	Inject(customPayload interface{}) error
	IsRunning() bool
}

// InjectTrigger triggers a manual trigger node by ID.
// Returns an error if the node doesn't exist, isn't a manual trigger, or isn't running.
func (r *FlowRuntime) InjectTrigger(nodeID string, payload interface{}) error {
	r.mu.RLock()
	trigger, exists := r.triggers[nodeID]
	r.mu.RUnlock()

	if !exists {
		return fmt.Errorf("node %s not found or not a trigger", nodeID)
	}

	injectable, ok := trigger.(InjectableNode)
	if !ok {
		return fmt.Errorf("node %s is not an injectable trigger (use trigger-manual)", nodeID)
	}

	if !injectable.IsRunning() {
		return fmt.Errorf("trigger %s is not running", nodeID)
	}

	return injectable.Inject(payload)
}

// GetTriggerNodeIDs returns the IDs of all trigger nodes in this flow.
func (r *FlowRuntime) GetTriggerNodeIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.triggers))
	for id := range r.triggers {
		ids = append(ids, id)
	}
	return ids
}
