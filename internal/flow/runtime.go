package flow

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/google/uuid"
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
	// DefaultExecBuffer is the buffer size of each per-node execution channel.
	DefaultExecBuffer = 256
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

// NodeExecutionHook is a callback function invoked before/after node execution.
type NodeExecutionHook func(nodeID string, msg *types.Message, err error)

// Node execution phases reported by a NodeEventHook. The string values match the
// events package phases so a recorder can forward them without translation.
const (
	NodePhaseRunning = "running"
	NodePhaseSuccess = "success"
	NodePhaseError   = "error"
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
	Phase    string // NodePhaseRunning | NodePhaseSuccess | NodePhaseError
	Input    *types.Message
	Outputs  []*types.Message
	Err      error
	Duration time.Duration
}

// NodeEventHook receives NodeEvents. It runs inline on the node executor
// goroutine while a semaphore slot is held, so it MUST NOT block: implementations
// are expected to do a non-blocking hand-off (e.g. a buffered channel send) and
// return immediately. Blocking here directly stalls node throughput.
type NodeEventHook func(NodeEvent)

// FlowRuntime manages the execution of a single flow.
// Each active flow runs in its own goroutine with message passing via channels.
type FlowRuntime struct {
	flow   *Flow
	router *MessageRouter
	logger zerolog.Logger

	// Node instances keyed by node ID
	nodes map[string]node.Node

	// Trigger nodes that need to be started
	triggers map[string]node.TriggerNode

	// Node definitions keyed by node ID. Immutable for the runtime's lifetime,
	// so executeNode can fetch a node's Type/Outputs without taking the flow's
	// RWLock or linearly scanning the node slice on every message.
	nodeDefs map[string]*Node

	// Per-node execution channels. Each node drains its own channel serially
	// (preserving message order for stateful nodes like batch/dedupe) while
	// different nodes run concurrently, so one blocking node no longer stalls
	// the whole flow.
	execChans map[string]chan RoutedMessage

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

	// Hooks for monitoring and debugging
	beforeExec NodeExecutionHook
	afterExec  NodeExecutionHook

	// observer receives structured per-execution NodeEvents (running/success/
	// error). It is the bridge to the observability layer (history + live SSE).
	observer NodeEventHook

	// Retry configuration for node execution
	retryConfig RetryConfig

	// Error channel for runtime errors
	errors chan error

	// Wait group for goroutine synchronization
	wg sync.WaitGroup
}

// RuntimeOption is a functional option for configuring the FlowRuntime.
type RuntimeOption func(*FlowRuntime)

// WithBeforeExecutionHook sets a hook to be called before node execution.
func WithBeforeExecutionHook(hook NodeExecutionHook) RuntimeOption {
	return func(r *FlowRuntime) {
		r.beforeExec = hook
	}
}

// WithAfterExecutionHook sets a hook to be called after node execution.
func WithAfterExecutionHook(hook NodeExecutionHook) RuntimeOption {
	return func(r *FlowRuntime) {
		r.afterExec = hook
	}
}

// WithObserver sets the structured per-execution observer hook. The hook must
// not block (see NodeEventHook).
func WithObserver(hook NodeEventHook) RuntimeOption {
	return func(r *FlowRuntime) {
		r.observer = hook
	}
}

// WithLogger sets a custom logger for the runtime.
func WithLogger(logger zerolog.Logger) RuntimeOption {
	return func(r *FlowRuntime) {
		r.logger = logger
	}
}

// WithRetryConfig sets the retry configuration for node execution.
func WithRetryConfig(config RetryConfig) RuntimeOption {
	return func(r *FlowRuntime) {
		r.retryConfig = config
	}
}

// WithMaxConcurrency sets the maximum number of nodes that may execute
// concurrently within the flow. Values <= 0 are ignored.
func WithMaxConcurrency(n int) RuntimeOption {
	return func(r *FlowRuntime) {
		if n > 0 {
			r.maxConcurrency = n
		}
	}
}

// NewFlowRuntime creates a new FlowRuntime for the given flow.
func NewFlowRuntime(f *Flow, registry *node.Registry, opts ...RuntimeOption) (*FlowRuntime, error) {
	logger := zerolog.Nop()

	rt := &FlowRuntime{
		flow:           f,
		logger:         logger,
		nodes:          make(map[string]node.Node),
		triggers:       make(map[string]node.TriggerNode),
		nodeDefs:       make(map[string]*Node),
		running:        false,
		state:          FlowStateInactive,
		retryConfig:    DefaultRetryConfig(),
		maxConcurrency: DefaultMaxConcurrency,
		errors:         make(chan error, 100),
	}

	// Apply options
	for _, opt := range opts {
		opt(rt)
	}

	// Instantiate all nodes from the registry
	for i := range f.Nodes {
		n := &f.Nodes[i]
		if !n.Enabled {
			continue
		}

		nodeInstance, err := registry.Create(n.Type, n.Config)
		if err != nil {
			return nil, NewNodeExecutionError(n.ID, n.Type, err)
		}

		// Enforce the documented contract that a node's configuration is
		// validated before deploy, so an invalid config fails fast and visibly
		// instead of running with silently-coerced values.
		if err := nodeInstance.Validate(); err != nil {
			return nil, NewNodeExecutionError(n.ID, n.Type, err)
		}

		rt.nodes[n.ID] = nodeInstance
		rt.nodeDefs[n.ID] = n

		// Track trigger nodes separately
		if trigger, ok := nodeInstance.(node.TriggerNode); ok {
			rt.triggers[n.ID] = trigger
		}
	}

	// Create the message router
	rt.router = NewMessageRouter(f.Connections, rt.logger)

	return rt, nil
}

// Start begins executing the flow.
// This method is non-blocking; the flow runs in background goroutines.
func (r *FlowRuntime) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return ErrFlowAlreadyRunning
	}

	// Use background context so the flow survives HTTP request completion.
	// The parent ctx is only used for initial validation, not for runtime lifecycle.
	r.ctx, r.cancel = context.WithCancel(context.Background())
	r.running = true
	r.state = FlowStateRunning
	r.mu.Unlock()

	r.logger.Info().
		Str("flowID", r.flow.ID).
		Str("flowName", r.flow.Name).
		Msg("Starting flow runtime")

	// Initialise the concurrency limiter and one serial executor per node.
	r.sem = make(chan struct{}, r.maxConcurrency)
	r.execChans = make(map[string]chan RoutedMessage, len(r.nodes))
	for nodeID := range r.nodes {
		ch := make(chan RoutedMessage, DefaultExecBuffer)
		r.execChans[nodeID] = ch
		r.wg.Add(1)
		go r.nodeExecutor(nodeID, ch)
	}

	// Start the dispatcher that fans routed messages out to per-node executors.
	r.wg.Add(1)
	go r.dispatch()

	// Start all trigger nodes
	for nodeID, trigger := range r.triggers {
		nodeID := nodeID // capture for goroutine
		trigger := trigger

		outChan := r.router.GetInputChannel(nodeID)

		r.wg.Add(1)
		go func() {
			defer r.wg.Done()

			r.logger.Debug().
				Str("nodeID", nodeID).
				Msg("Starting trigger node")

			if err := trigger.Start(r.ctx, outChan); err != nil {
				r.logger.Error().
					Err(err).
					Str("nodeID", nodeID).
					Msg("Trigger node error")

				select {
				case r.errors <- NewNodeExecutionError(nodeID, "trigger", err):
				default:
					// Error channel full, log and continue
				}
			}
		}()
	}

	return nil
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

	// Cancel the context to signal all goroutines to stop
	r.cancel()

	// Stop all trigger nodes
	for nodeID, trigger := range r.triggers {
		r.logger.Debug().
			Str("nodeID", nodeID).
			Msg("Stopping trigger node")

		if err := trigger.Stop(); err != nil {
			r.logger.Error().
				Err(err).
				Str("nodeID", nodeID).
				Msg("Error stopping trigger node")
		}
	}

	// Close the router to stop message processing
	r.router.Close()

	// Wait for all goroutines to finish with a timeout
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		r.logger.Debug().Msg("All goroutines stopped gracefully")
	case <-time.After(5 * time.Second):
		r.logger.Warn().Msg("Timeout waiting for goroutines to stop")
	}

	r.mu.Lock()
	r.running = false
	r.state = FlowStateInactive
	r.mu.Unlock()

	return nil
}

// dispatch reads routed messages from the router and hands each one to its
// target node's serial executor. Because distinct nodes execute concurrently,
// a slow or blocking node (a delay, a long HTTP call) no longer stalls
// unrelated branches of the flow.
func (r *FlowRuntime) dispatch() {
	defer r.wg.Done()

	for {
		select {
		case <-r.ctx.Done():
			r.logger.Debug().Msg("Dispatcher stopped")
			return

		case routedMsg, ok := <-r.router.Output():
			if !ok {
				r.logger.Debug().Msg("Router output channel closed")
				return
			}

			ch, exists := r.execChans[routedMsg.TargetNode]
			if !exists {
				r.logger.Error().
					Str("nodeID", routedMsg.TargetNode).
					Msg("Target node not found or disabled; dropping message")
				continue
			}

			select {
			case ch <- routedMsg:
			case <-r.ctx.Done():
				return
			}
		}
	}
}

// nodeExecutor drains a single node's channel serially. Serial per-node
// execution preserves message ordering for stateful nodes (batch, dedupe,
// throttle), while the shared sem caps how many nodes run at once across the
// whole flow so an unbounded fan-out can't spawn unbounded work.
func (r *FlowRuntime) nodeExecutor(nodeID string, ch chan RoutedMessage) {
	defer r.wg.Done()

	for {
		select {
		case <-r.ctx.Done():
			return

		case routedMsg := <-ch:
			// Acquire a global concurrency slot, respecting cancellation.
			select {
			case r.sem <- struct{}{}:
			case <-r.ctx.Done():
				return
			}

			func() {
				defer func() { <-r.sem }()
				r.executeNode(routedMsg)
			}()
		}
	}
}

// executeNode processes a message through the target node and routes the output.
// It uses exponential backoff retry for transient failures.
func (r *FlowRuntime) executeNode(routedMsg RoutedMessage) {
	nodeInstance, ok := r.nodes[routedMsg.TargetNode]
	if !ok {
		r.logger.Error().
			Str("nodeID", routedMsg.TargetNode).
			Msg("Target node not found")
		return
	}

	// Get the flow node definition for metadata (precomputed, lock-free).
	flowNode, ok := r.nodeDefs[routedMsg.TargetNode]
	if !ok || flowNode == nil {
		r.logger.Error().
			Str("nodeID", routedMsg.TargetNode).
			Msg("Flow node definition not found")
		return
	}

	// Ensure correlation ID exists for tracing. Computed before the recovery
	// defer so a panic can still be reported as a terminal error NodeEvent.
	correlationID := r.ensureCorrelationID(routedMsg.Message)

	// Shared fields for every NodeEvent this execution emits; each fire point
	// copies it and sets only Phase/Duration/Outputs/Err.
	baseEvent := NodeEvent{
		FlowID:   r.flow.ID,
		NodeID:   routedMsg.TargetNode,
		NodeType: flowNode.Type,
		CorrID:   correlationID,
		Input:    routedMsg.Message,
	}

	// A panic inside a node's Process must not take down the executor goroutine
	// (and through it the whole runtime). Recover, log with a stack trace, and
	// record it as a node error so it is observable instead of silent. We also
	// emit a terminal error NodeEvent so a panicking node doesn't leave the
	// observer (and the editor) stuck showing it as perpetually "running".
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error().
				Interface("panic", rec).
				Str("nodeID", routedMsg.TargetNode).
				Str("nodeType", flowNode.Type).
				Bytes("stack", debug.Stack()).
				Msg("Recovered from panic during node execution")
			metrics.RecordNodeError(flowNode.Type)
			if r.observer != nil {
				ev := baseEvent
				ev.Phase = NodePhaseError
				ev.Err = fmt.Errorf("panic: %v", rec)
				r.observer(ev)
			}
		}
	}()

	// Create a logger with correlation context
	logger := r.logger.With().
		Str("correlationID", correlationID).
		Str("flowID", r.flow.ID).
		Str("nodeID", routedMsg.TargetNode).
		Str("nodeType", flowNode.Type).
		Logger()

	// Call before execution hook
	if r.beforeExec != nil {
		r.beforeExec(routedMsg.TargetNode, routedMsg.Message, nil)
	}

	// Emit the "running" event. Both hook calls are non-blocking by contract, so
	// they never stall the executor goroutine that holds a semaphore slot.
	if r.observer != nil {
		ev := baseEvent
		ev.Phase = NodePhaseRunning
		r.observer(ev)
	}

	logger.Debug().
		Str("msgID", routedMsg.Message.ID).
		Msg("Executing node")

	// Execute the node with retry logic
	startTime := time.Now()
	outputs, err := r.executeNodeWithRetry(nodeInstance, routedMsg, flowNode, logger)
	duration := time.Since(startTime)

	// Record metrics
	metrics.RecordNodeExecution(flowNode.Type, duration.Seconds())
	metrics.RecordMessageProcessed(flowNode.Type, err == nil)

	// Call after execution hook
	if r.afterExec != nil {
		r.afterExec(routedMsg.TargetNode, routedMsg.Message, err)
	}

	// Emit the terminal event (success/error) with timing and, on success, the
	// outputs (used to attribute process-debug output to its canvas node).
	if r.observer != nil {
		ev := baseEvent
		ev.Phase = NodePhaseSuccess
		ev.Duration = duration
		if err != nil {
			ev.Phase = NodePhaseError
			ev.Err = err
		} else {
			ev.Outputs = outputs
		}
		r.observer(ev)
	}

	if err != nil {
		metrics.RecordNodeError(flowNode.Type)
		logger.Error().
			Err(err).
			Dur("duration", duration).
			Msg("Node execution failed after all retries")

		select {
		case r.errors <- NewNodeExecutionError(routedMsg.TargetNode, flowNode.Type, err):
		default:
			logger.Warn().Msg("Runtime error channel full; dropping error notification (failure already logged)")
		}
		return
	}

	logger.Debug().
		Int("outputCount", len(outputs)).
		Dur("duration", duration).
		Msg("Node execution completed")

	// Route output messages to connected nodes
	for _, output := range outputs {
		output.SourceNode = routedMsg.TargetNode

		// Auto-assign the source port only when the node has exactly one output.
		// For multi-output nodes (switch/filter with true/false ports) a missing
		// SourcePort is a bug: silently defaulting to Outputs[0] would route the
		// message down the wrong branch, so warn and skip instead of guessing.
		if output.SourcePort == "" {
			if len(flowNode.Outputs) == 1 {
				output.SourcePort = flowNode.Outputs[0].ID
			} else if len(flowNode.Outputs) > 1 {
				logger.Warn().
					Str("nodeType", flowNode.Type).
					Msg("Node produced an output with no SourcePort but declares multiple output ports; message not routed")
				continue
			}
		}

		r.router.Route(routedMsg.TargetNode, output)
	}
}

// executeNodeWithRetry executes a node with exponential backoff retry logic.
// It returns the outputs on success or the last error after all retries are exhausted.
func (r *FlowRuntime) executeNodeWithRetry(nodeInstance node.Node, routedMsg RoutedMessage, flowNode *Node, logger zerolog.Logger) ([]*types.Message, error) {
	var lastErr error
	wait := r.retryConfig.InitialWait

	for attempt := 0; attempt <= r.retryConfig.MaxRetries; attempt++ {
		outputs, err := nodeInstance.Process(r.ctx, routedMsg.Message)

		if err == nil {
			if attempt > 0 {
				logger.Info().
					Int("attempt", attempt).
					Msg("Node execution succeeded after retry")
			}
			return outputs, nil
		}

		lastErr = err

		// Don't retry if context is cancelled
		if r.ctx.Err() != nil {
			return nil, r.ctx.Err()
		}

		// Only transient errors are retried. Errors are permanent by default
		// (node.IsRetryable == false), so side-effecting actions (HTTP POST,
		// shell commands) are never blindly re-run — preventing duplicate side
		// effects — and permanent failures (4xx, bad config) fail fast.
		if !node.IsRetryable(err) {
			return nil, err
		}

		// Don't retry if we've exhausted all attempts
		if attempt >= r.retryConfig.MaxRetries {
			break
		}

		// Record retry metric
		metrics.RecordNodeRetry(flowNode.Type)

		logger.Warn().
			Err(err).
			Int("attempt", attempt).
			Int("maxRetries", r.retryConfig.MaxRetries).
			Dur("nextWait", wait).
			Msg("Node execution failed, retrying")

		// Wait before retrying (with context cancellation support)
		select {
		case <-time.After(wait):
			// Calculate next wait time with exponential backoff
			wait = time.Duration(float64(wait) * r.retryConfig.Multiplier)
			if wait > r.retryConfig.MaxWait {
				wait = r.retryConfig.MaxWait
			}
		case <-r.ctx.Done():
			return nil, r.ctx.Err()
		}
	}

	return nil, lastErr
}

// ensureCorrelationID ensures the message has a correlation ID for tracing.
// If one doesn't exist, it generates a new one.
func (r *FlowRuntime) ensureCorrelationID(msg *types.Message) string {
	if val, ok := msg.GetMeta("correlationID"); ok {
		if id, ok := val.(string); ok && id != "" {
			return id
		}
	}

	// Generate new correlation ID
	correlationID := uuid.New().String()
	msg.SetMeta("correlationID", correlationID)
	return correlationID
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

// RuntimeManager manages multiple flow runtimes.
type RuntimeManager struct {
	runtimes map[string]*FlowRuntime
	registry *node.Registry
	logger   zerolog.Logger
	observer NodeEventHook
	mu       sync.RWMutex
}

// ManagerOption configures a RuntimeManager.
type ManagerOption func(*RuntimeManager)

// WithManagerObserver sets a NodeEventHook applied to every flow this manager
// deploys, so the whole process shares one observability sink.
func WithManagerObserver(hook NodeEventHook) ManagerOption {
	return func(m *RuntimeManager) {
		m.observer = hook
	}
}

// NewRuntimeManager creates a new RuntimeManager.
func NewRuntimeManager(registry *node.Registry, logger zerolog.Logger, opts ...ManagerOption) *RuntimeManager {
	m := &RuntimeManager{
		runtimes: make(map[string]*FlowRuntime),
		registry: registry,
		logger:   logger,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Deploy creates and starts a runtime for the given flow.
func (m *RuntimeManager) Deploy(ctx context.Context, f *Flow) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already running
	if rt, exists := m.runtimes[f.ID]; exists && rt.IsRunning() {
		return ErrFlowAlreadyRunning
	}

	// Validate the flow
	if err := f.Validate(); err != nil {
		metrics.RecordFlowDeployment(false)
		return err
	}

	// Create the runtime, forwarding the process-wide observer if one is set.
	rtOpts := []RuntimeOption{WithLogger(m.logger)}
	if m.observer != nil {
		rtOpts = append(rtOpts, WithObserver(m.observer))
	}
	rt, err := NewFlowRuntime(f, m.registry, rtOpts...)
	if err != nil {
		metrics.RecordFlowDeployment(false)
		return err
	}

	// Start the runtime
	if err := rt.Start(ctx); err != nil {
		metrics.RecordFlowDeployment(false)
		return err
	}

	m.runtimes[f.ID] = rt

	// Record metrics
	metrics.RecordFlowDeployment(true)
	metrics.IncrementFlowsRunning()

	m.logger.Info().
		Str("flowID", f.ID).
		Str("flowName", f.Name).
		Msg("Flow deployed successfully")

	return nil
}

// Undeploy stops and removes the runtime for the given flow.
func (m *RuntimeManager) Undeploy(flowID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rt, exists := m.runtimes[flowID]
	if !exists {
		return ErrFlowNotFound
	}

	wasRunning := rt.IsRunning()
	if wasRunning {
		if err := rt.Stop(); err != nil {
			return err
		}
		// Decrement running flows metric
		metrics.DecrementFlowsRunning()
	}

	delete(m.runtimes, flowID)

	m.logger.Info().
		Str("flowID", flowID).
		Msg("Flow undeployed successfully")

	return nil
}

// GetRuntime returns the runtime for the given flow ID.
func (m *RuntimeManager) GetRuntime(flowID string) (*FlowRuntime, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rt, exists := m.runtimes[flowID]
	return rt, exists
}

// ListRunning returns a list of all running flow IDs.
func (m *RuntimeManager) ListRunning() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	running := make([]string, 0)
	for id, rt := range m.runtimes {
		if rt.IsRunning() {
			running = append(running, id)
		}
	}
	return running
}

// StopAll stops all running flows.
func (m *RuntimeManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, rt := range m.runtimes {
		if rt.IsRunning() {
			if err := rt.Stop(); err != nil {
				m.logger.Error().
					Err(err).
					Str("flowID", id).
					Msg("Error stopping flow")
			}
		}
	}

	m.logger.Info().Msg("All flows stopped")
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
