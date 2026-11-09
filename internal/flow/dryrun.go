package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/types"
	flowvars "github.com/Pegasus8/piworker/internal/vars"
	"github.com/google/uuid"
)

// Dry-run (docs/design/mcp.md §10): a flow runs once in a private runtime
// with every trigger and every node that is not safe replaced by a stub, and
// with isolated, empty secret and variable stores, so nothing it does leaves
// the sandbox. Limits:
const (
	// DryRunMaxNodes caps the nodes of a dry-run flow (disabled ones too).
	DryRunMaxNodes = 50
	// DryRunTimeout caps the wall-clock time of a dry-run.
	DryRunTimeout = 10 * time.Second
	// DryRunMaxExecutions caps the messages a dry-run routes (each is one
	// node execution), so its executions and the copies routing makes.
	DryRunMaxExecutions = 200
	// DryRunPreviewBytes caps each payload preview and error message.
	DryRunPreviewBytes = 2 << 10
	// DryRunMaxResultBytes caps the JSON of a whole result.
	DryRunMaxResultBytes = 64 << 10
	// DryRunMaxConnections caps the connections of a dry-run flow.
	DryRunMaxConnections = 4 * DryRunMaxNodes
	// DryRunMaxInputBytes caps the JSON of the payload, of each mock and of
	// the flow variables of a dry-run, and the input and config of a node
	// test.
	DryRunMaxInputBytes = 64 << 10
	// DryRunMaxValueBytes caps any single value a sandboxed node generates
	// (an output payload, a string an expression builds, a rendered
	// template).
	DryRunMaxValueBytes = 1 << 20
	// DryRunMaxDataBytes caps all the data a dry-run or node test generates:
	// the strings expressions build, rendered templates and every copy of a
	// routed message.
	DryRunMaxDataBytes = 32 << 20
)

// maxStepOutputs caps the outputs previewed per execution.
const maxStepOutputs = 64

// dryRunTimeout is DryRunTimeout; tests lower it (export_test.go).
var dryRunTimeout = DryRunTimeout

// dryRunStopTimeout bounds how long a dry-run waits, past its deadline, for
// its private runtime to stop (a node ignoring cancellation keeps the
// sandbox slot instead, see DryRun).
const dryRunStopTimeout = time.Second

// redactedConfigValue replaces the config values DryRunInput.Redact hides.
const redactedConfigValue = "<redacted>"

var (
	// ErrDryRunInput marks a dry-run or node test refused because of what it
	// was asked (not because the flow is invalid): an unknown or unsuitable
	// trigger, a mock for a node that is not stubbed, a flow over the node or
	// connection caps or with duplicate connections, inputs over
	// DryRunMaxInputBytes.
	ErrDryRunInput = errors.New("invalid dry-run input")
	// ErrSandboxBusy (wraps ErrBusy): another dry-run or node test is
	// running. One runs at a time, process-wide.
	ErrSandboxBusy error = &busyError{"another dry-run or node test is in progress"}
)

// sandboxSlot admits one dry-run or node test at a time, process-wide.
var sandboxSlot = make(chan struct{}, 1)

// acquireSandbox takes the sandbox slot, or fails at once with
// ErrSandboxBusy.
func acquireSandbox() (release func(), err error) {
	select {
	case sandboxSlot <- struct{}{}:
		return func() { <-sandboxSlot }, nil
	default:
		return nil, ErrSandboxBusy
	}
}

// sandboxContext gives ctx the isolated stores of a sandboxed execution: the
// global and flow variable stores and an empty secret store, so the nodes
// never see (or change) the real ones and every {{secret.NAME}} is missing;
// and the sandbox, which bounds the data the nodes generate and tracks the
// goroutines they leave running (node.Sandbox).
func sandboxContext(ctx context.Context, globals, locals *flowvars.Store, sb *node.Sandbox) context.Context {
	ctx = flowvars.WithStore(ctx, globals)
	ctx = flowvars.WithFlowStore(ctx, locals)
	ctx = node.WithSandbox(ctx, sb)
	return secrets.WithStore(ctx, secrets.NewStore())
}

// newSandbox returns the data bounds of one dry-run or node test.
func newSandbox() *node.Sandbox { return node.NewSandbox(DryRunMaxValueBytes, DryRunMaxDataBytes) }

// checkInputSize refuses an input over DryRunMaxInputBytes of JSON.
func checkInputSize(what string, v any) error {
	if node.EstimateSize(v, DryRunMaxInputBytes) > DryRunMaxInputBytes {
		return fmt.Errorf("%w: %s is over %d bytes", ErrDryRunInput, what, DryRunMaxInputBytes)
	}
	return nil
}

// checkDryRunShape refuses a flow over the node or connection caps, or with
// two connections between the same ports (each would route every message
// again).
func checkDryRunShape(f *Flow) error {
	if len(f.Nodes) > DryRunMaxNodes {
		return fmt.Errorf("%w: a dry-run takes at most %d nodes, the flow has %d", ErrDryRunInput, DryRunMaxNodes, len(f.Nodes))
	}
	if len(f.Connections) > DryRunMaxConnections {
		return fmt.Errorf("%w: a dry-run takes at most %d connections, the flow has %d", ErrDryRunInput, DryRunMaxConnections, len(f.Connections))
	}
	type endpoints struct{ srcNode, srcPort, dstNode, dstPort string }
	seen := make(map[endpoints]string, len(f.Connections))
	for _, c := range f.Connections {
		key := endpoints{c.SourceNode, c.SourcePort, c.TargetNode, c.TargetPort}
		if first, dup := seen[key]; dup {
			return fmt.Errorf("%w: duplicate connection %s (same ports as %s)", ErrDryRunInput, c.ID, first)
		}
		seen[key] = c.ID
	}
	return nil
}

// checkDryRunInputs bounds the payload, each mock and the flow variables.
func checkDryRunInputs(in DryRunInput) error {
	if err := checkInputSize("the payload", in.Payload); err != nil {
		return err
	}
	ids := make([]string, 0, len(in.Mocks))
	for id := range in.Mocks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := checkInputSize(fmt.Sprintf("the mock for %q", id), in.Mocks[id]); err != nil {
			return err
		}
	}
	return checkInputSize("flowVars", in.FlowVars)
}

// redactConfig replaces the config values redact selects with
// redactedConfigValue (see DryRunInput.Redact).
func redactConfig(f *Flow, redact func(n *Node, prop string) bool) {
	if redact == nil {
		return
	}
	for i := range f.Nodes {
		n := &f.Nodes[i]
		props := make([]string, 0, len(n.Config))
		for prop := range n.Config {
			props = append(props, prop)
		}
		sort.Strings(props)
		for _, prop := range props {
			if redact(n, prop) {
				n.Config[prop] = redactedConfigValue
			}
		}
	}
}

// DryRunInput says how to run a dry-run.
type DryRunInput struct {
	// TriggerNodeID is the enabled input node at whose output Payload is
	// injected. It may be empty when the flow has exactly one enabled input
	// node.
	TriggerNodeID string `json:"triggerNodeId"`
	// Payload is the payload of the injected message.
	Payload any `json:"payload,omitempty"`
	// Mocks are, by node ID, the payloads stubbed nodes emit on their first
	// output port; a stubbed node without one emits nothing.
	Mocks map[string]any `json:"mocks,omitempty"`
	// FlowVars seed the isolated flow variable store.
	FlowVars map[string]any `json:"flowVars,omitempty"`
	// Redact, when set, hides config values from the run: every config
	// property of a node for which it returns true is replaced with
	// "<redacted>" before the flow is validated and run, so a value the
	// caller may not see reaches no preview, not even transformed by the
	// nodes it flows through. A redacted value that makes its node invalid
	// fails the dry-run like any invalid flow.
	Redact func(n *Node, prop string) bool `json:"-"`
}

// PortPreview is one output of an execution: the port and a preview of the
// payload.
type PortPreview struct {
	Port    string `json:"port"`
	Preview string `json:"preview"`
}

// TraceStep is one node execution of a dry-run. Previews are the JSON of the
// payload, cut to DryRunPreviewBytes (a cut one ends with "…"); they are not
// redacted (the service layer does that for agents).
type TraceStep struct {
	NodeID string `json:"nodeId"`
	Type   string `json:"type"`
	// Input previews the input payload; empty for the trigger, whose
	// output is the injected payload.
	Input      string        `json:"input,omitempty"`
	Outputs    []PortPreview `json:"outputs"`
	Error      string        `json:"error,omitempty"`
	DurationMs float64       `json:"durationMs"`
	// Stubbed: the node did not run; a stub emits its mock, if any.
	Stubbed bool `json:"stubbed"`
}

// WouldExecute is a stubbed node a message reached: what would have run.
type WouldExecute struct {
	NodeID string    `json:"nodeId"`
	Type   string    `json:"type"`
	Risk   node.Risk `json:"risk"`
	Input  string    `json:"input"`
}

// DryRunResult is what a dry-run did, in execution order.
type DryRunResult struct {
	// Issues are Lint's issues of the flow as it would run (after Redact),
	// each message cut to DryRunPreviewBytes. An error other than an
	// EnvironmentIssue (undefined_secret) means the flow did not run: the
	// trace and wouldExecute are then empty.
	Issues       []Issue        `json:"issues"`
	Trace        []TraceStep    `json:"trace"`
	WouldExecute []WouldExecute `json:"wouldExecute"`
	// MissingSecrets are the secret names the nodes the run reached
	// reference: the sandbox has none of them.
	MissingSecrets []string `json:"missingSecrets"`
	// Truncated: a preview was cut or entries were left out to keep the
	// result within DryRunMaxResultBytes.
	Truncated bool `json:"truncated"`
	// TimedOut: the run was stopped after DryRunTimeout.
	TimedOut bool `json:"timedOut"`
	// Capped: the run was stopped at DryRunMaxExecutions routed messages
	// (each execution is one) or when its data budget (DryRunMaxDataBytes)
	// was spent.
	Capped bool `json:"capped"`
}

// DryRun runs f once in a sandbox and reports what happened. The flow is
// first linted (LintContext with ctx's secret store: deploy's own checks);
// its issues are the result's Issues, and an error issue other than an
// EnvironmentIssue (see BlockingIssues) means it does not run — the result
// then holds only the issues and DryRun returns no error. A valid flow is
// then instantiated in a private runtime that is never registered with a
// RuntimeManager, through reg.Derive: every node is wrapped so that triggers
// never start (Payload is injected at the chosen trigger's output) and every
// node whose type or instance risk is not safe is a stub that records itself
// in WouldExecute and emits its mock, if any. Nodes get isolated variable
// stores, an empty secret store and a node.Sandbox bounding the data they
// generate. The private runtime runs one node at a time, keeps out of the
// process metrics, and routes at most DryRunMaxExecutions messages. The run
// ends when all of its work is done, at that cap or when the data budget is
// spent (Capped), DryRunTimeout after DryRun was called (TimedOut), or when
// ctx ends (then the partial result is returned with ctx's error). Nodes are
// closed when DryRun returns; work still running then (a node ignoring
// cancellation, a goroutine a node started with node.Go) keeps the sandbox
// slot until it returns. One dry-run or node test runs at a time: otherwise
// the error wraps ErrBusy. f is not modified.
func DryRun(ctx context.Context, f *Flow, reg *node.Registry, in DryRunInput) (DryRunResult, error) {
	if f == nil || reg == nil {
		return DryRunResult{}, fmt.Errorf("%w: no flow", ErrDryRunInput)
	}
	if err := checkDryRunShape(f); err != nil {
		return DryRunResult{}, err
	}
	if err := checkDryRunInputs(in); err != nil {
		return DryRunResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return DryRunResult{}, err
	}
	release, err := acquireSandbox()
	if err != nil {
		return DryRunResult{}, err
	}
	held := true
	defer func() {
		if held {
			release()
		}
	}()
	// The wall-clock cap covers everything from here, building the nodes
	// included.
	runCtx, cancel := context.WithTimeout(ctx, dryRunTimeout)
	defer cancel()

	work, err := cloneFlowJSON(f)
	if err != nil {
		return DryRunResult{}, fmt.Errorf("%w: %v", ErrDryRunInput, err)
	}
	redactConfig(work, in.Redact)
	// Lint is the one validation (ADR-0007): the checks deploy runs, on the
	// flow as it would run. The secrets are checked against the caller's
	// store (the sandbox's is empty), and do not stop the run.
	issues := LintContext(ctx, work, reg)
	if len(BlockingIssues(issues)) > 0 {
		return (&dryRunSession{issues: issues}).result(work, false), nil
	}

	sb := newSandbox()
	sess := &dryRunSession{done: make(chan struct{}), sb: sb, issues: issues}
	globals, locals := flowvars.NewStore(), flowvars.NewStore()
	for k, v := range in.FlowVars {
		if err := locals.Set(k, v); err != nil {
			return DryRunResult{}, fmt.Errorf("%w: flow variable %q: %v", ErrDryRunInput, k, err)
		}
	}
	rt, err := NewFlowRuntime(work, reg.Derive(dryRunOverride(reg)),
		WithRetryConfig(RetryConfig{MaxRetries: 0, InitialWait: DefaultInitialWait, MaxWait: DefaultMaxWait, Multiplier: DefaultMultiplier}),
		WithObserver(sess.observe),
		WithNodeContext(func(c context.Context) context.Context { return sandboxContext(c, globals, locals, sb) }),
		WithMaxConcurrency(1),
		WithNodeQueueLimit(DryRunMaxExecutions),
		WithStopTimeout(dryRunStopTimeout),
		withoutMetrics(),
		withRouteGate(sess.admitRoute),
	)
	if err != nil {
		return DryRunResult{}, fmt.Errorf("invalid flow: %w", err)
	}
	trigger, port, err := sess.bind(rt, reg, in)
	if err != nil {
		rt.closeNodes()
		return DryRunResult{}, err
	}
	if runCtx.Err() != nil {
		// The setup took the whole wall-clock budget.
		rt.closeNodes()
		return sess.result(work, ctx.Err() == nil), ctx.Err()
	}
	if err := rt.Start(ctx); err != nil {
		return DryRunResult{}, err
	}

	sess.inject(runCtx, trigger, port, in.Payload)
	select {
	case <-sess.done:
	case <-runCtx.Done():
	}
	var callerErr error
	timedOut := false
	if !sess.isDone() {
		if ctx.Err() != nil {
			callerErr = ctx.Err()
		} else {
			timedOut = true
		}
	}

	_ = rt.Stop()
	if !rt.Drained() || sb.Busy() {
		// Work is still running (the runtime draining, a goroutine a node
		// left behind): wait for it, bounded, so the next dry-run or node
		// test does not find the slot taken; work still running then (a
		// node ignoring cancellation) keeps the slot until it returns, so
		// stuck dry-runs cannot pile up.
		drained := rt.drained
		held = settleSandbox(func() {
			<-drained
			sb.Wait()
		}, release)
	}
	return sess.result(work, timedOut), callerErr
}

// settleSandbox waits up to dryRunStopTimeout for wait (the end of a
// sandboxed execution's work) and reports whether it returned; if it did
// not, release runs once it does, and the caller must not release the slot.
func settleSandbox(wait func(), release func()) (settled bool) {
	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()
	timer := time.NewTimer(dryRunStopTimeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		go func() {
			<-done
			release()
		}()
		return false
	}
}

// cloneFlowJSON deep-copies a flow through its JSON form (so a flow that
// cannot be serialized is refused).
func cloneFlowJSON(f *Flow) (*Flow, error) {
	data, err := json.Marshal(f)
	if err != nil {
		return nil, fmt.Errorf("the flow cannot be serialized: %v", err)
	}
	return FromJSON(data)
}

// dryRunOverride makes the factories of the dry-run's derived registry: each
// builds the real node (its ports and Validate stay the real ones) and wraps
// it. Triggers and nodes whose instance risk (orig's rules) is not safe
// become stubs: their real instance is validated, closed and dropped, so it
// never runs or starts.
func dryRunOverride(orig *node.Registry) func(node.NodeTypeInfo, node.NodeFactory) node.NodeFactory {
	return func(info node.NodeTypeInfo, factory node.NodeFactory) node.NodeFactory {
		return func(cfg map[string]any) (node.Node, error) {
			inner, err := factory(cfg)
			if err != nil {
				return nil, err
			}
			ins, outs := inner.Ports()
			w := &dryRunNode{inner: inner, typ: info.Type, inputs: ins, outputs: outs}
			if _, isTrigger := inner.(node.TriggerNode); isTrigger || info.Category == NodeCategoryInput {
				w.risk = info.Risk
				w.stubOut()
				return &dryRunTrigger{dryRunNode: w}, nil
			}
			w.risk = InstanceRisk(&Node{Type: info.Type, Config: cfg}, orig)
			if w.risk != node.RiskSafe {
				w.stubOut()
			}
			return w, nil
		}
	}
}

// closeNode closes n when it implements node.Closer.
func closeNode(n node.Node) {
	if c, ok := n.(node.Closer); ok {
		_ = c.Close()
	}
}

// dryRunNode wraps every non-trigger node of a dry-run: it counts and traces
// executions, and either runs the real node or, as a stub, records what
// would have run and emits its mock.
type dryRunNode struct {
	inner           node.Node // nil for a stub
	stub            bool
	validateErr     error // a stub's Validate result
	typ             string
	risk            node.Risk
	inputs, outputs []types.Port

	// Set by bind before the runtime starts.
	id      string
	sess    *dryRunSession
	mock    any
	hasMock bool
}

var _ node.Closer = (*dryRunNode)(nil)

// stubOut turns w into a stub: the real instance is validated, closed and
// dropped.
func (w *dryRunNode) stubOut() {
	w.validateErr = w.inner.Validate()
	closeNode(w.inner)
	w.inner = nil
	w.stub = true
}

func (w *dryRunNode) Ports() (inputs, outputs []types.Port) { return w.inputs, w.outputs }

func (w *dryRunNode) Validate() error {
	if w.inner != nil {
		return w.inner.Validate()
	}
	return w.validateErr
}

// Close closes the real instance (a stub's was closed already).
func (w *dryRunNode) Close() error {
	if c, ok := w.inner.(node.Closer); ok {
		return c.Close()
	}
	return nil
}

func (w *dryRunNode) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	seq, ok := w.sess.admit()
	if !ok {
		return nil, nil
	}
	start := time.Now()
	step := TraceStep{NodeID: w.id, Type: w.typ, Input: w.sess.preview(msg.Payload), Stubbed: w.stub}
	var outs []*types.Message
	var err error
	var would *WouldExecute
	if w.stub {
		would = &WouldExecute{NodeID: w.id, Type: w.typ, Risk: w.risk, Input: step.Input}
		if w.hasMock && len(w.outputs) > 0 {
			out := msg.Clone()
			out.Payload = types.NewMessage(w.mock, types.DataTypeAny).Clone().Payload
			out.PayloadType = payloadDataType(out.Payload)
			out.SourcePort = w.outputs[0].ID
			outs = []*types.Message{out}
		}
	} else {
		outs, err = w.sess.runInner(ctx, w.inner, msg)
	}
	step.DurationMs = durationMs(time.Since(start))
	step.Outputs = w.sess.outputPreviews(outs, w.outputs)
	if err != nil {
		step.Error = w.sess.cut(err.Error())
	}
	w.sess.record(seq, step, would)
	return outs, err
}

// runInner runs a real node in the sandbox: an output over the per-value cap,
// or a limit the node ran into without returning an error (a template that
// stopped expanding), is an error and nothing is emitted.
func (s *dryRunSession) runInner(ctx context.Context, n node.Node, msg *types.Message) ([]*types.Message, error) {
	return sandboxProcess(ctx, s.sb, n, msg)
}

// sandboxProcess runs n.Process in sb (see runInner).
func sandboxProcess(ctx context.Context, sb *node.Sandbox, n node.Node, msg *types.Message) ([]*types.Message, error) {
	refused := sb.Refusals()
	outs, err := processSafely(ctx, n, msg)
	if err != nil {
		return nil, err
	}
	if sb.Refusals() > refused {
		return nil, fmt.Errorf("%w: the node stopped at a limit of the sandbox", node.ErrSandboxLimit)
	}
	for _, o := range outs {
		if o == nil {
			continue
		}
		if err := sb.CheckValue(o.Payload); err != nil {
			return nil, fmt.Errorf("output: %w", err)
		}
	}
	return outs, nil
}

// processSafely runs n.Process, turning a panic into an error.
func processSafely(ctx context.Context, n node.Node, msg *types.Message) (outs []*types.Message, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			outs, err = nil, fmt.Errorf("panic: %v", rec)
		}
	}()
	return n.Process(ctx, msg)
}

// dryRunTrigger stubs a trigger: Start only keeps the output channel, into
// which the dry-run injects its payload (for the chosen trigger).
type dryRunTrigger struct {
	*dryRunNode
	mu  sync.Mutex
	out chan<- *types.Message
}

func (t *dryRunTrigger) Start(_ context.Context, out chan<- *types.Message) error {
	t.mu.Lock()
	t.out = out
	t.mu.Unlock()
	return nil
}

func (t *dryRunTrigger) Stop() error { return nil }

// dryRunSession collects what one dry-run did.
type dryRunSession struct {
	rt         *FlowRuntime // set by bind, before the runtime starts
	sb         *node.Sandbox
	execs      atomic.Int64
	routed     atomic.Int64
	capped     atomic.Bool
	previewCut atomic.Bool
	issues     []Issue // Lint's, set before the run

	mu     sync.Mutex
	closed bool // the result was taken: later records are dropped
	steps  []orderedStep
	would  []orderedWould

	doneOnce sync.Once
	done     chan struct{} // closed when the run is over or capped
}

type orderedStep struct {
	seq  int64
	step TraceStep
}

type orderedWould struct {
	seq   int64
	would WouldExecute
}

// bind attaches the wrapped nodes of rt to the session, applies the mocks
// and returns the chosen trigger and the port its payload leaves on.
func (s *dryRunSession) bind(rt *FlowRuntime, reg *node.Registry, in DryRunInput) (*dryRunTrigger, string, error) {
	s.rt = rt
	var triggers []string
	for id, inst := range rt.nodes {
		switch w := inst.(type) {
		case *dryRunTrigger:
			w.id, w.sess = id, s
			triggers = append(triggers, id)
		case *dryRunNode:
			w.id, w.sess = id, s
		default:
			return nil, "", fmt.Errorf("dry-run: node %s was not wrapped", id)
		}
	}

	triggerID := in.TriggerNodeID
	if triggerID == "" {
		if len(triggers) != 1 {
			return nil, "", fmt.Errorf("%w: triggerNodeId is required (the flow has %d enabled input nodes)", ErrDryRunInput, len(triggers))
		}
		triggerID = triggers[0]
	}
	def := rt.flow.GetNode(triggerID)
	if def == nil {
		return nil, "", fmt.Errorf("%w: trigger node %q is not in the flow", ErrDryRunInput, triggerID)
	}
	trigger, ok := rt.nodes[triggerID].(*dryRunTrigger)
	if info, known := reg.Get(def.Type); !ok || !known || info.Category != NodeCategoryInput {
		if !def.Enabled {
			return nil, "", fmt.Errorf("%w: trigger node %q is disabled", ErrDryRunInput, triggerID)
		}
		return nil, "", fmt.Errorf("%w: node %q (%s) is not an input node", ErrDryRunInput, triggerID, def.Type)
	}
	if len(trigger.outputs) == 0 {
		return nil, "", fmt.Errorf("%w: trigger node %q has no output port", ErrDryRunInput, triggerID)
	}

	mockIDs := make([]string, 0, len(in.Mocks))
	for id := range in.Mocks {
		mockIDs = append(mockIDs, id)
	}
	sort.Strings(mockIDs)
	for _, id := range mockIDs {
		w, ok := rt.nodes[id].(*dryRunNode)
		_, isTrigger := rt.nodes[id].(*dryRunTrigger)
		switch {
		case isTrigger:
			return nil, "", fmt.Errorf("%w: mock for %q, an input node: a dry-run injects the payload at the trigger's output instead", ErrDryRunInput, id)
		case !ok:
			return nil, "", fmt.Errorf("%w: mock for %q, which is not an enabled node that a message can reach", ErrDryRunInput, id)
		case !w.stub:
			return nil, "", fmt.Errorf("%w: mock for %q (%s), which is safe and runs for real in a dry-run; only stubbed nodes take mocks", ErrDryRunInput, id, w.typ)
		case len(w.outputs) == 0:
			return nil, "", fmt.Errorf("%w: mock for %q (%s), which has no output port", ErrDryRunInput, id, w.typ)
		}
		w.mock, w.hasMock = in.Mocks[id], true
	}
	return trigger, trigger.outputs[0].ID, nil
}

// inject traces the trigger and sends the payload from its output, as the
// first message of the run.
func (s *dryRunSession) inject(ctx context.Context, t *dryRunTrigger, port string, payload any) {
	msg := types.NewMessage(payload, payloadDataType(payload)).Clone()
	msg.SourcePort = port
	msg.SetMeta("correlationID", uuid.New().String())
	msg.SetMeta("actor", "dry-run")
	s.record(0, TraceStep{
		NodeID: t.id, Type: t.typ, Stubbed: true,
		Outputs: []PortPreview{{Port: port, Preview: s.preview(msg.Payload)}},
	}, nil)
	if s.rt.targetCount(t.id, port) == 0 {
		s.finish()
		return
	}
	t.mu.Lock()
	out := t.out
	t.mu.Unlock()
	select {
	case out <- msg:
	case <-ctx.Done():
	}
}

// observe ends the run once no message of any run is in flight (a node may
// give its outputs a correlation ID of their own, which starts another run).
func (s *dryRunSession) observe(ev NodeEvent) {
	if ev.Phase == NodePhaseRunFinished && s.rt.inflightRuns() == 0 {
		s.finish()
	}
}

func (s *dryRunSession) finish() { s.doneOnce.Do(func() { close(s.done) }) }

func (s *dryRunSession) isDone() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// admitRoute is the private runtime's route gate: msg is about to be routed
// as copies messages. Past DryRunMaxExecutions routed messages, or when the
// copies would spend the data budget, it refuses (capped): the messages
// already routed still execute, and the run ends once they did.
func (s *dryRunSession) admitRoute(msg *types.Message, copies int) bool {
	if s.routed.Add(int64(copies)) > DryRunMaxExecutions {
		s.capped.Store(true)
		return false
	}
	size := node.EstimateSize(msg.Payload, DryRunMaxValueBytes)
	for range copies {
		if s.sb.Charge(size) != nil {
			s.capped.Store(true)
			return false
		}
	}
	return true
}

// admit counts an execution; past the cap it refuses it and ends the run.
func (s *dryRunSession) admit() (int64, bool) {
	n := s.execs.Add(1)
	if n > DryRunMaxExecutions {
		s.capped.Store(true)
		s.finish()
		return 0, false
	}
	return n, true
}

func (s *dryRunSession) record(seq int64, step TraceStep, would *WouldExecute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.steps = append(s.steps, orderedStep{seq, step})
	if would != nil {
		s.would = append(s.would, orderedWould{seq, *would})
	}
}

// preview returns the bounded JSON preview of a payload.
func (s *dryRunSession) preview(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<%T payload that is not JSON>", v)
	}
	return s.cut(string(data))
}

// cut bounds a preview or error message to DryRunPreviewBytes.
func (s *dryRunSession) cut(text string) string {
	out, cut := cutText(text, DryRunPreviewBytes)
	if cut {
		s.previewCut.Store(true)
	}
	return out
}

// outputPreviews previews the outputs of an execution, on the port the
// runtime routes them from (an output without one leaves on the node's only
// output port).
func (s *dryRunSession) outputPreviews(outs []*types.Message, ports []types.Port) []PortPreview {
	res := make([]PortPreview, 0, min(len(outs), maxStepOutputs))
	for _, o := range outs {
		if o == nil {
			continue
		}
		if len(res) == maxStepOutputs {
			s.previewCut.Store(true)
			break
		}
		port := o.SourcePort
		if port == "" && len(ports) == 1 {
			port = ports[0].ID
		}
		res = append(res, PortPreview{Port: port, Preview: s.preview(o.Payload)})
	}
	return res
}

// result builds the bounded result, in execution order: what would have run
// first, then the missing secrets, then the trace, while they fit.
func (s *dryRunSession) result(work *Flow, timedOut bool) DryRunResult {
	s.mu.Lock()
	s.closed = true
	steps, would := s.steps, s.would
	s.mu.Unlock()
	sort.SliceStable(steps, func(a, b int) bool { return steps[a].seq < steps[b].seq })
	sort.SliceStable(would, func(a, b int) bool { return would[a].seq < would[b].seq })

	res := DryRunResult{
		Issues: []Issue{}, Trace: []TraceStep{}, WouldExecute: []WouldExecute{}, MissingSecrets: []string{},
		Truncated: true, TimedOut: true, Capped: true,
	}
	budget := DryRunMaxResultBytes - jsonLen(res)
	res.Truncated, res.TimedOut, res.Capped = s.previewCut.Load(), timedOut, s.capped.Load()
	fits := func(v any) bool {
		n := jsonLen(v) + 1 // and a comma
		if n > budget {
			res.Truncated = true
			return false
		}
		budget -= n
		return true
	}

	for _, issue := range s.issues {
		issue.Message = s.cut(issue.Message)
		if !fits(issue) {
			return res
		}
		res.Issues = append(res.Issues, issue)
	}
	for _, w := range would {
		if !fits(w.would) {
			return res
		}
		res.WouldExecute = append(res.WouldExecute, w.would)
	}
	for _, name := range missingSecrets(work, steps) {
		if !fits(name) {
			return res
		}
		res.MissingSecrets = append(res.MissingSecrets, name)
	}
	for _, o := range steps {
		step := o.step
		outs := step.Outputs
		step.Outputs = []PortPreview{}
		if !fits(step) {
			return res
		}
		for _, p := range outs {
			if !fits(p) {
				res.Trace = append(res.Trace, step)
				return res
			}
			step.Outputs = append(step.Outputs, p)
		}
		res.Trace = append(res.Trace, step)
	}
	return res
}

// missingSecrets returns the sorted secret names referenced
// ({{secret.NAME}}) by the config of the nodes the run reached.
func missingSecrets(work *Flow, steps []orderedStep) []string {
	reached := map[string]bool{}
	for _, o := range steps {
		reached[o.step.NodeID] = true
	}
	names := map[string]bool{}
	for i := range work.Nodes {
		if reached[work.Nodes[i].ID] {
			collectSecretNames(work.Nodes[i].Config, names)
		}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// collectSecretNames adds the names of the {{secret.NAME}} placeholders in a
// config value (nested maps and lists included) to names.
func collectSecretNames(v any, names map[string]bool) {
	switch x := v.(type) {
	case string:
		for _, m := range placeholderRe.FindAllString(x, -1) {
			if isSecretPlaceholder(m) {
				inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(m, "{{"), "}}"))
				_, name, _ := strings.Cut(inner, ".")
				names[name] = true
			}
		}
	case map[string]any:
		for _, e := range x {
			collectSecretNames(e, names)
		}
	case []any:
		for _, e := range x {
			collectSecretNames(e, names)
		}
	}
}

// inflightRuns returns how many runs still have messages in flight.
func (r *FlowRuntime) inflightRuns() int {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	return len(r.runInflight)
}

// cutText cuts text to at most limit bytes on a rune boundary, ending it
// with "…" when it was cut.
func cutText(text string, limit int) (string, bool) {
	if len(text) <= limit {
		return text, false
	}
	const ellipsis = "…"
	end := limit - len(ellipsis)
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + ellipsis, true
}

// jsonLen is the length of v's JSON encoding.
func jsonLen(v any) int {
	data, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(data)
}

// durationMs is d in milliseconds, to the microsecond.
func durationMs(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// payloadDataType is the message data type of a payload.
func payloadDataType(v any) types.DataType {
	switch v.(type) {
	case nil:
		return types.DataTypeAny
	case string:
		return types.DataTypeString
	case bool:
		return types.DataTypeBoolean
	case float64, float32, int, int64, int32, json.Number:
		return types.DataTypeNumber
	case []any:
		return types.DataTypeArray
	case map[string]any:
		return types.DataTypeObject
	default:
		return types.DataTypeAny
	}
}
