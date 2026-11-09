package flow

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/types"
	flowvars "github.com/Pegasus8/piworker/internal/vars"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sandboxProbe records what the nodes of a dry-run saw and did.
type sandboxProbe struct {
	mu            sync.Mutex
	triggerStarts int
	created       map[string]int
	closed        map[string]int
	secretSeen    []string
	globalStore   *flowvars.Store
	flowStore     *flowvars.Store
	entered       chan struct{}
	release       chan struct{}
}

func newSandboxProbe() *sandboxProbe {
	return &sandboxProbe{created: map[string]int{}, closed: map[string]int{}, entered: make(chan struct{}, 16), release: make(chan struct{})}
}

type probeTrigger struct {
	*node.BaseNode
	p *sandboxProbe
}

func (n *probeTrigger) Process(context.Context, *Message) ([]*Message, error) { return nil, nil }
func (n *probeTrigger) Ports() (inputs, outputs []Port) {
	return nil, []Port{{ID: "output", DataType: DataTypeAny}}
}
func (n *probeTrigger) Start(context.Context, chan<- *Message) error {
	n.p.mu.Lock()
	n.p.triggerStarts++
	n.p.mu.Unlock()
	return nil
}
func (n *probeTrigger) Stop() error { return nil }

// probeNode is a safe processing node: "burst" emits config.count copies of
// its input, "block" waits for the probe's release, "sniff" records the
// secret and variable stores of its context; every kind passes its input on.
type probeNode struct {
	*node.BaseNode
	p    *sandboxProbe
	kind string
}

func (p *sandboxProbe) newNode(cfg map[string]any, kind string) *probeNode {
	p.mu.Lock()
	p.created[kind]++
	p.mu.Unlock()
	return &probeNode{BaseNode: node.NewBaseNode(cfg), p: p, kind: kind}
}

func (n *probeNode) Process(ctx context.Context, msg *Message) ([]*Message, error) {
	switch n.kind {
	case "burst":
		count := n.GetConfigInt("count", 1)
		out := make([]*Message, count)
		for i := range out {
			out[i] = msg.Clone()
			out[i].SourcePort = "output"
		}
		return out, nil
	case "block":
		n.p.entered <- struct{}{}
		select {
		case <-n.p.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	case "sniff":
		v, _ := secrets.FromContext(ctx).Get("probe_canary")
		n.p.mu.Lock()
		n.p.secretSeen = append(n.p.secretSeen, v)
		n.p.globalStore = flowvars.FromContext(ctx)
		n.p.flowStore = flowvars.FlowFromContext(ctx)
		n.p.mu.Unlock()
	}
	out := msg.Clone()
	out.SourcePort = "output"
	return []*Message{out}, nil
}
func (n *probeNode) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", DataType: DataTypeAny}}, []Port{{ID: "output", DataType: DataTypeAny}}
}
func (n *probeNode) Close() error {
	n.p.mu.Lock()
	n.p.closed[n.kind]++
	n.p.mu.Unlock()
	return nil
}

// probeRegistry: "probe-trigger" (input), safe "burst", "block", "sniff",
// "pass", and "probe-send", an external output node (stubbed in a dry-run).
func probeRegistry(t *testing.T, p *sandboxProbe) *node.Registry {
	t.Helper()
	reg := node.NewRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "probe-trigger", Category: NodeCategoryInput, Risk: node.RiskSafe,
		Outputs: []Port{{ID: "output", DataType: DataTypeAny}}},
		func(cfg map[string]any) (node.Node, error) {
			return &probeTrigger{BaseNode: node.NewBaseNode(cfg), p: p}, nil
		}))
	for _, kind := range []string{"burst", "block", "sniff", "pass"} {
		require.NoError(t, reg.Register(node.NodeTypeInfo{Type: kind, Category: NodeCategoryProcessing, Risk: node.RiskSafe,
			Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}},
			func(cfg map[string]any) (node.Node, error) { return p.newNode(cfg, kind), nil }))
	}
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "probe-send", Category: NodeCategoryOutput, Risk: node.RiskExternal,
		Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}},
		func(cfg map[string]any) (node.Node, error) { return p.newNode(cfg, "send"), nil }))
	return reg
}

// probeFlow builds probe-trigger "trig" → types[0] → types[1] → …; the node
// IDs are the types (configs from cfg, by type).
func probeFlow(cfg map[string]map[string]any, typs ...string) *Flow {
	f := NewFlow("probe")
	trig := NewNode("probe-trigger", NodeCategoryInput)
	trig.ID = "trig"
	f.AddNode(*trig)
	prev := "trig"
	for _, typ := range typs {
		n := NewNode(typ, NodeCategoryProcessing)
		n.ID = typ
		if c := cfg[typ]; c != nil {
			n.Config = c
		}
		f.AddNode(*n)
		f.AddConnection(*NewConnection(prev, "output", typ, "input"))
		prev = typ
	}
	return f
}

// settledGoroutines waits until the goroutine count stops changing.
func settledGoroutines() int {
	n := runtime.NumGoroutine()
	for i := 0; i < 50; i++ {
		time.Sleep(10 * time.Millisecond)
		m := runtime.NumGoroutine()
		if m == n {
			return m
		}
		n = m
	}
	return n
}

// TestDryRunCapsExecutions: 201 executions → the 201st never runs; the
// result says capped.
func TestDryRunCapsExecutions(t *testing.T) {
	assert.Equal(t, 200, DryRunMaxExecutions)
	p := newSandboxProbe()
	f := probeFlow(map[string]map[string]any{"burst": {"count": 200}}, "burst", "pass")
	res, err := DryRun(context.Background(), f, probeRegistry(t, p), DryRunInput{TriggerNodeID: "trig", Payload: 1})
	require.NoError(t, err)
	assert.True(t, res.Capped)
	assert.False(t, res.TimedOut)
	executed := 0
	for _, s := range res.Trace {
		if s.NodeID != "trig" {
			executed++
		}
	}
	assert.Equal(t, DryRunMaxExecutions, executed, "exactly the cap ran")

	res, err = DryRun(context.Background(), probeFlow(map[string]map[string]any{"burst": {"count": 199}}, "burst", "pass"),
		probeRegistry(t, p), DryRunInput{TriggerNodeID: "trig", Payload: 1})
	require.NoError(t, err)
	assert.False(t, res.Capped, "200 executions are within the cap")
}

// TestDryRunOneAtATime: a second dry-run (or a node test) while one runs
// fails at once with an error wrapping ErrBusy.
func TestDryRunOneAtATime(t *testing.T) {
	p := newSandboxProbe()
	reg := probeRegistry(t, p)
	done := make(chan error, 1)
	go func() {
		_, err := DryRun(context.Background(), probeFlow(nil, "block"), reg, DryRunInput{TriggerNodeID: "trig"})
		done <- err
	}()
	select {
	case <-p.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first dry-run never reached its blocking node")
	}

	start := time.Now()
	_, err := DryRun(context.Background(), probeFlow(nil, "pass"), reg, DryRunInput{TriggerNodeID: "trig"})
	require.ErrorIs(t, err, ErrBusy)
	assert.True(t, errors.Is(err, ErrSandboxBusy))
	_, err = TestNode(context.Background(), reg, "pass", nil, 1)
	require.ErrorIs(t, err, ErrBusy, "node tests share the slot")
	assert.Less(t, time.Since(start), time.Second, "refused at once")

	close(p.release)
	require.NoError(t, <-done)
	_, err = DryRun(context.Background(), probeFlow(nil, "pass"), reg, DryRunInput{TriggerNodeID: "trig"})
	require.NoError(t, err, "free again once the first one returned")
}

// TestDryRunIsolatesNodeContext: nodes run with an empty secret store and
// isolated variable stores, never the real ones.
func TestDryRunIsolatesNodeContext(t *testing.T) {
	require.NoError(t, secrets.DefaultStore.Set("probe_canary", "real"))
	t.Cleanup(func() { _ = secrets.DefaultStore.Delete("probe_canary") })
	p := newSandboxProbe()
	f := probeFlow(nil, "sniff")
	_, err := DryRun(context.Background(), f, probeRegistry(t, p), DryRunInput{TriggerNodeID: "trig", FlowVars: map[string]any{"k": "v"}})
	require.NoError(t, err)

	p.mu.Lock()
	defer p.mu.Unlock()
	assert.Equal(t, []string{""}, p.secretSeen, "the secret store is empty")
	require.NotNil(t, p.globalStore)
	assert.NotSame(t, flowvars.DefaultStore, p.globalStore)
	assert.Empty(t, p.globalStore.Snapshot())
	require.NotNil(t, p.flowStore)
	assert.Equal(t, map[string]any{"k": "v"}, p.flowStore.Snapshot())
}

// TestDryRunNeverStartsTriggersAndClosesNodes: no trigger of the flow is
// started (the payload is injected at the chosen one's output), stubbed
// nodes never run, and every node implementing node.Closer is closed.
func TestDryRunNeverStartsTriggersAndClosesNodes(t *testing.T) {
	p := newSandboxProbe()
	f := probeFlow(nil, "pass", "probe-send")
	other := NewNode("probe-trigger", NodeCategoryInput)
	other.ID = "other"
	f.AddNode(*other)
	f.AddConnection(*NewConnection("other", "output", "pass", "input"))

	reg := probeRegistry(t, p)
	p.mu.Lock()
	registered := map[string]int{"pass": p.created["pass"], "send": p.created["send"]} // Register's schema probes
	p.mu.Unlock()
	res, err := DryRun(context.Background(), f, reg, DryRunInput{TriggerNodeID: "trig", Payload: "x"})
	require.NoError(t, err)
	require.Len(t, res.WouldExecute, 1)
	assert.Equal(t, "probe-send", res.WouldExecute[0].NodeID)
	assert.Equal(t, node.RiskExternal, res.WouldExecute[0].Risk)

	p.mu.Lock()
	defer p.mu.Unlock()
	assert.Zero(t, p.triggerStarts, "no trigger was started")
	// The validation pass builds instances too: every instance is closed once.
	for _, kind := range []string{"pass", "send"} {
		built := p.created[kind] - registered[kind]
		assert.Positive(t, built)
		assert.Equal(t, built, p.closed[kind], "every %s instance the dry-run built was closed once", kind)
	}
}

// TestDryRunLeavesNoGoroutine: once DryRun returns (completed, capped,
// timed out or failed validation) no goroutine of it survives.
func TestDryRunLeavesNoGoroutine(t *testing.T) {
	p := newSandboxProbe()
	reg := probeRegistry(t, p)
	run := func(f *Flow) DryRunResult {
		t.Helper()
		res, err := DryRun(context.Background(), f, reg, DryRunInput{TriggerNodeID: "trig"})
		require.NoError(t, err)
		return res
	}
	run(probeFlow(nil, "pass")) // warm up lazily started runtime helpers
	before := settledGoroutines()

	run(probeFlow(nil, "pass", "probe-send"))
	assert.True(t, run(probeFlow(map[string]map[string]any{"burst": {"count": 300}}, "burst", "pass")).Capped)
	_, err := DryRun(context.Background(), probeFlow(nil, "pass", "missing-type"), reg, DryRunInput{TriggerNodeID: "trig"})
	require.Error(t, err)
	SetDryRunTimeout(t, 50*time.Millisecond)
	assert.True(t, run(probeFlow(nil, "block")).TimedOut)
	select {
	case <-p.entered:
	default:
		t.Fatal("the blocking node never ran")
	}

	assert.LessOrEqual(t, settledGoroutines(), before, "no goroutine of a dry-run survives it")
}

// TestDryRunCountsRunsWithOwnCorrelationID: a node that gives its outputs a
// correlation ID of their own does not end the dry-run before they ran.
func TestDryRunCountsRunsWithOwnCorrelationID(t *testing.T) {
	reg := node.NewRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "probe-trigger", Category: NodeCategoryInput, Risk: node.RiskSafe,
		Outputs: []Port{{ID: "output", DataType: DataTypeAny}}},
		func(cfg map[string]any) (node.Node, error) {
			return &probeTrigger{BaseNode: node.NewBaseNode(cfg), p: newSandboxProbe()}, nil
		}))
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "rebrand", Category: NodeCategoryProcessing, Risk: node.RiskSafe,
		Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}},
		func(cfg map[string]any) (node.Node, error) { return &rebrandNode{BaseNode: node.NewBaseNode(cfg)}, nil }))
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "slow", Category: NodeCategoryProcessing, Risk: node.RiskSafe,
		Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}},
		func(cfg map[string]any) (node.Node, error) { return &slowNode{BaseNode: node.NewBaseNode(cfg)}, nil }))

	res, err := DryRun(context.Background(), probeFlow(nil, "rebrand", "slow"), reg, DryRunInput{TriggerNodeID: "trig"})
	require.NoError(t, err)
	var ids []string
	for _, s := range res.Trace {
		ids = append(ids, s.NodeID)
	}
	assert.Equal(t, []string{"trig", "rebrand", "slow"}, ids)
}

type rebrandNode struct{ *node.BaseNode }

func (rebrandNode) Process(_ context.Context, msg *Message) ([]*Message, error) {
	out := msg.Clone()
	out.SourcePort = "output"
	out.SetMeta("correlationID", "another-run")
	return []*Message{out}, nil
}
func (rebrandNode) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", DataType: DataTypeAny}}, []Port{{ID: "output", DataType: DataTypeAny}}
}

type slowNode struct{ *node.BaseNode }

func (slowNode) Process(ctx context.Context, msg *Message) ([]*Message, error) {
	select {
	case <-time.After(50 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return nil, nil
}
func (slowNode) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", DataType: DataTypeAny}}, []Port{{ID: "output", DataType: DataTypeAny}}
}

// TestDryRunPanickingNode: a panic in a node is an error of its step.
func TestDryRunPanickingNode(t *testing.T) {
	reg := probeRegistry(t, newSandboxProbe())
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "boom", Category: NodeCategoryProcessing, Risk: node.RiskSafe,
		Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}},
		func(cfg map[string]any) (node.Node, error) { return &boomNode{BaseNode: node.NewBaseNode(cfg)}, nil }))
	res, err := DryRun(context.Background(), probeFlow(nil, "boom"), reg, DryRunInput{TriggerNodeID: "trig"})
	require.NoError(t, err)
	require.Len(t, res.Trace, 2)
	assert.Contains(t, res.Trace[1].Error, "panic")
}

type boomNode struct{ *node.BaseNode }

func (boomNode) Process(context.Context, *Message) ([]*Message, error) { panic("kaboom") }
func (boomNode) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", DataType: DataTypeAny}}, []Port{{ID: "output", DataType: DataTypeAny}}
}

// TestDryRunPayloadType: the injected message's data type follows the payload.
func TestDryRunPayloadType(t *testing.T) {
	assert.Equal(t, types.DataTypeObject, payloadDataType(map[string]any{}))
	assert.Equal(t, types.DataTypeArray, payloadDataType([]any{}))
	assert.Equal(t, types.DataTypeString, payloadDataType("s"))
	assert.Equal(t, types.DataTypeNumber, payloadDataType(1.5))
	assert.Equal(t, types.DataTypeBoolean, payloadDataType(true))
	assert.Equal(t, types.DataTypeAny, payloadDataType(nil))
}
