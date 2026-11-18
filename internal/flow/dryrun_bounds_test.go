package flow

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// boundsProbe drives the nodes of the bounds tests.
type boundsProbe struct {
	entered  chan string   // a "hold" or "stubborn" node started
	release  chan struct{} // closed: "hold" and "stubborn" nodes return
	linger   chan struct{} // closed: "linger" goroutines return
	makeWait time.Duration // "slowmake" factory delay
	brief    time.Duration // how long a "brief" goroutine runs
	running  atomic.Int32  // "hold" nodes running now
	maxSeen  atomic.Int32  // most "hold" nodes seen running at once
}

func newBoundsProbe() *boundsProbe {
	return &boundsProbe{entered: make(chan string, 64), release: make(chan struct{}), linger: make(chan struct{})}
}

// boundsNode kinds: "big" emits a string of config.bytes bytes; "hold" waits
// for release or its context; "stubborn" waits for release only (ignores
// cancellation); "linger" starts a goroutine with node.Go that waits for
// linger, then returns at once; "pass" and "slowmake" pass their input on.
type boundsNode struct {
	*node.BaseNode
	p    *boundsProbe
	kind string
}

func (n *boundsNode) Process(ctx context.Context, msg *Message) ([]*Message, error) {
	out := msg.Clone()
	out.SourcePort = "output"
	switch n.kind {
	case "big":
		out.Payload = strings.Repeat("b", n.GetConfigInt("bytes", 1))
	case "hold":
		running := n.p.running.Add(1)
		defer n.p.running.Add(-1)
		for {
			seen := n.p.maxSeen.Load()
			if running <= seen || n.p.maxSeen.CompareAndSwap(seen, running) {
				break
			}
		}
		n.p.entered <- n.kind
		select {
		case <-n.p.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	case "stubborn":
		n.p.entered <- n.kind
		<-n.p.release
	case "linger":
		node.Go(ctx, func() { <-n.p.linger })
	case "brief":
		node.Go(ctx, func() { time.Sleep(n.p.brief) })
	}
	return []*Message{out}, nil
}

func (n *boundsNode) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", DataType: DataTypeAny}}, []Port{{ID: "output", DataType: DataTypeAny}}
}

func boundsRegistry(t *testing.T, p *boundsProbe) *node.Registry {
	t.Helper()
	reg := probeRegistry(t, newSandboxProbe())
	for _, kind := range []string{"big", "hold", "stubborn", "linger", "slowmake", "brief"} {
		require.NoError(t, reg.Register(node.NodeTypeInfo{Type: kind, Category: NodeCategoryProcessing, Risk: node.RiskSafe,
			Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}},
			func(cfg map[string]any) (node.Node, error) {
				if kind == "slowmake" {
					time.Sleep(p.makeWait)
				}
				return &boundsNode{BaseNode: node.NewBaseNode(cfg), p: p, kind: kind}, nil
			}))
	}
	return reg
}

// addNode adds a node of typ with id to f.
func addNode(f *Flow, id, typ string, cfg map[string]any) {
	n := NewNode(typ, NodeCategoryProcessing)
	n.ID = id
	if cfg != nil {
		n.Config = cfg
	}
	f.AddNode(*n)
}

func connect(f *Flow, src, dst string) {
	f.AddConnection(*NewConnection(src, "output", dst, "input"))
}

// TestDryRunRefusesDuplicateConnections: two connections with the same
// endpoints would route every message twice (and multiply the copies): the
// dry-run refuses them.
func TestDryRunRefusesDuplicateConnections(t *testing.T) {
	reg := boundsRegistry(t, newBoundsProbe())
	f := probeFlow(nil, "pass")
	connect(f, "trig", "pass") // the same endpoints, another connection ID
	_, err := DryRun(context.Background(), f, reg, DryRunInput{TriggerNodeID: "trig"})
	require.ErrorIs(t, err, ErrDryRunInput)
	assert.Contains(t, err.Error(), "duplicate connection")
}

// TestDryRunCapsConnections: a dry-run flow has at most
// DryRunMaxConnections connections.
func TestDryRunCapsConnections(t *testing.T) {
	assert.Equal(t, 4*DryRunMaxNodes, DryRunMaxConnections)
	reg := boundsRegistry(t, newBoundsProbe())
	f := probeFlow(nil)
	for i := 0; i < 30; i++ {
		addNode(f, fmt.Sprintf("p%d", i), "pass", nil)
		connect(f, "trig", fmt.Sprintf("p%d", i))
	}
	for i := 0; len(f.Connections) <= DryRunMaxConnections; i++ {
		for j := i + 1; j < 30 && len(f.Connections) <= DryRunMaxConnections; j++ {
			connect(f, fmt.Sprintf("p%d", i), fmt.Sprintf("p%d", j))
		}
	}
	_, err := DryRun(context.Background(), f, reg, DryRunInput{TriggerNodeID: "trig"})
	require.ErrorIs(t, err, ErrDryRunInput)
	assert.Contains(t, err.Error(), "connections")
}

// TestDryRunBoundsInputs: the payload, each mock and the flow variables are
// at most DryRunMaxInputBytes of JSON each.
func TestDryRunBoundsInputs(t *testing.T) {
	reg := boundsRegistry(t, newBoundsProbe())
	big := strings.Repeat("x", DryRunMaxInputBytes)
	for name, in := range map[string]DryRunInput{
		"payload":  {TriggerNodeID: "trig", Payload: big},
		"mock":     {TriggerNodeID: "trig", Mocks: map[string]any{"probe-send": big}},
		"flowVars": {TriggerNodeID: "trig", FlowVars: map[string]any{"a": big[:DryRunMaxInputBytes/2], "b": big[:DryRunMaxInputBytes/2]}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DryRun(context.Background(), probeFlow(nil, "probe-send"), reg, in)
			require.ErrorIs(t, err, ErrDryRunInput)
			assert.Contains(t, err.Error(), name)
		})
	}
	_, err := DryRun(context.Background(), probeFlow(nil, "probe-send"), reg, DryRunInput{TriggerNodeID: "trig",
		Payload: big[:DryRunMaxInputBytes/2], Mocks: map[string]any{"probe-send": "ok"}})
	require.NoError(t, err, "within the bounds")
}

// TestDryRunBoundsOutputs: an output over the per-value cap is an error of
// its step and goes nowhere.
func TestDryRunBoundsOutputs(t *testing.T) {
	reg := boundsRegistry(t, newBoundsProbe())
	f := probeFlow(map[string]map[string]any{"big": {"bytes": DryRunMaxValueBytes + 1}}, "big", "pass")
	res, err := DryRun(context.Background(), f, reg, DryRunInput{TriggerNodeID: "trig"})
	require.NoError(t, err)
	step, ok := stepFor(res, "big")
	require.True(t, ok)
	assert.Contains(t, step.Error, node.ErrSandboxLimit.Error())
	_, ran := stepFor(res, "pass")
	assert.False(t, ran, "the output went nowhere")
}

// TestDryRunBoundsRoutedData: every message routed is a copy of its payload;
// the copies draw on the dry-run's data budget and the run is capped when it
// is spent, long before the copies exhaust memory.
func TestDryRunBoundsRoutedData(t *testing.T) {
	reg := boundsRegistry(t, newBoundsProbe())
	f := probeFlow(map[string]map[string]any{"big": {"bytes": DryRunMaxValueBytes / 2}}, "big")
	for i := 0; i < 45; i++ {
		id := fmt.Sprintf("p%d", i)
		addNode(f, id, "pass", nil)
		connect(f, "big", id)
		connect(f, id, "pass-sink")
	}
	addNode(f, "pass-sink", "pass", nil)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	res, err := DryRun(context.Background(), f, reg, DryRunInput{TriggerNodeID: "trig"})
	runtime.ReadMemStats(&after)
	require.NoError(t, err)
	assert.True(t, res.Capped, "45×2 copies of 512 KiB are over the %d MiB budget", DryRunMaxDataBytes>>20)
	assert.False(t, res.TimedOut)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(256<<20))
}

// TestDryRunRoutingIsBounded: the reviewers' amplification (a fan-out of
// many connections carrying a large object payload) is refused or capped,
// with allocations far below what the copies would need.
func TestDryRunRoutingIsBounded(t *testing.T) {
	reg := boundsRegistry(t, newBoundsProbe())
	payload := map[string]any{}
	for i := 0; len(payload) < 2000; i++ {
		payload[fmt.Sprintf("key-%04d", i)] = strings.Repeat("v", 16)
	}
	f := probeFlow(nil)
	for i := 0; i < 48; i++ {
		id := fmt.Sprintf("p%d", i)
		addNode(f, id, "pass", nil)
		connect(f, "trig", id)
		for j := 0; j < i && j < 3; j++ {
			connect(f, id, fmt.Sprintf("p%d", j))
		}
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	res, err := DryRun(context.Background(), f, reg, DryRunInput{TriggerNodeID: "trig", Payload: payload})
	runtime.ReadMemStats(&after)
	require.NoError(t, err)
	assert.True(t, res.Capped)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(512<<20))

	dup := probeFlow(nil, "pass")
	for i := 0; i < 199; i++ {
		connect(dup, "trig", "pass")
	}
	_, err = DryRun(context.Background(), dup, reg, DryRunInput{TriggerNodeID: "trig", Payload: payload})
	require.ErrorIs(t, err, ErrDryRunInput, "duplicate connections are refused")
}

// TestDryRunRunsOneNodeAtATime: the private runtime executes one node at a
// time, so a dry-run keeps at most one core busy.
func TestDryRunRunsOneNodeAtATime(t *testing.T) {
	p := newBoundsProbe()
	reg := boundsRegistry(t, p)
	f := probeFlow(nil)
	addNode(f, "h1", "hold", nil)
	addNode(f, "h2", "hold", nil)
	connect(f, "trig", "h1")
	connect(f, "trig", "h2")
	done := make(chan error, 1)
	go func() {
		_, err := DryRun(context.Background(), f, reg, DryRunInput{TriggerNodeID: "trig"})
		done <- err
	}()
	<-p.entered
	select {
	case <-p.entered:
		t.Fatal("two nodes of one dry-run ran at once")
	case <-time.After(100 * time.Millisecond):
	}
	close(p.release)
	require.NoError(t, <-done)
	assert.Equal(t, int32(1), p.maxSeen.Load())
}

// TestDryRunHoldsSlotForTrackedGoroutines: work a node left running with
// node.Go keeps the sandbox slot until it returns.
func TestDryRunHoldsSlotForTrackedGoroutines(t *testing.T) {
	p := newBoundsProbe()
	reg := boundsRegistry(t, p)
	_, err := DryRun(context.Background(), probeFlow(nil, "linger"), reg, DryRunInput{TriggerNodeID: "trig"})
	require.NoError(t, err)

	_, err = DryRun(context.Background(), probeFlow(nil, "pass"), reg, DryRunInput{TriggerNodeID: "trig"})
	require.ErrorIs(t, err, ErrBusy, "the lingering goroutine holds the slot")
	_, err = TestNode(context.Background(), reg, "pass", nil, 1)
	require.ErrorIs(t, err, ErrBusy)

	close(p.linger)
	require.Eventually(t, func() bool {
		_, err := DryRun(context.Background(), probeFlow(nil, "pass"), reg, DryRunInput{TriggerNodeID: "trig"})
		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "free once it returned")
}

// TestSandboxWaitsBrieflyForTrackedGoroutines: work that ends soon after a
// dry-run or node test (a goroutine a node started with node.Go, a runtime
// still draining) is waited for, bounded, before the call returns, so the
// next one right after does not find the slot taken.
func TestSandboxWaitsBrieflyForTrackedGoroutines(t *testing.T) {
	p := newBoundsProbe()
	p.brief = 100 * time.Millisecond
	reg := boundsRegistry(t, p)
	ctx := context.Background()
	for range 3 {
		_, err := DryRun(ctx, probeFlow(nil, "brief"), reg, DryRunInput{TriggerNodeID: "trig"})
		require.NoError(t, err)
		_, err = TestNode(ctx, reg, "brief", nil, 1)
		require.NoError(t, err, "the dry-run's goroutine had returned")
		_, err = DryRun(ctx, probeFlow(nil, "pass"), reg, DryRunInput{TriggerNodeID: "trig"})
		require.NoError(t, err, "the node test's goroutine had returned")
	}
}

// TestDryRunDeadlineCoversSetup: the wall-clock cap starts when DryRun is
// called (building the nodes counts), and a node that ignores cancellation
// delays the return by about a second at most (the private runtime's stop
// timeout); the slot stays held until that node returns.
func TestDryRunDeadlineCoversSetup(t *testing.T) {
	p := newBoundsProbe()
	p.makeWait = 150 * time.Millisecond
	reg := boundsRegistry(t, p)
	SetDryRunTimeout(t, 400*time.Millisecond)

	start := time.Now()
	res, err := DryRun(context.Background(), probeFlow(nil, "slowmake", "hold"), reg, DryRunInput{TriggerNodeID: "trig"})
	require.NoError(t, err)
	assert.True(t, res.TimedOut)
	assert.Less(t, time.Since(start), 650*time.Millisecond, "the setup's 300 ms count against the 400 ms cap")
	<-p.entered

	start = time.Now()
	res, err = DryRun(context.Background(), probeFlow(nil, "stubborn"), reg, DryRunInput{TriggerNodeID: "trig"})
	require.NoError(t, err)
	assert.True(t, res.TimedOut)
	assert.Less(t, time.Since(start), 400*time.Millisecond+2500*time.Millisecond, "not the default 5 s stop timeout")
	_, err = TestNode(context.Background(), reg, "pass", nil, 1)
	require.ErrorIs(t, err, ErrBusy, "the stubborn node still holds the slot")
	close(p.release)
	require.Eventually(t, func() bool {
		_, err := TestNode(context.Background(), reg, "pass", nil, 1)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
}

// TestTestNodeHardDeadline: a node that ignores cancellation does not keep
// TestNode past its timeout; the slot stays held until the node returns.
func TestTestNodeHardDeadline(t *testing.T) {
	p := newBoundsProbe()
	reg := boundsRegistry(t, p)
	SetTestNodeTimeout(t, 100*time.Millisecond)
	start := time.Now()
	res, err := TestNode(context.Background(), reg, "stubborn", nil, 1)
	require.NoError(t, err)
	assert.True(t, res.TimedOut)
	assert.NotEmpty(t, res.Error)
	assert.Empty(t, res.Outputs)
	assert.Less(t, time.Since(start), time.Second)

	_, err = DryRun(context.Background(), probeFlow(nil, "pass"), reg, DryRunInput{TriggerNodeID: "trig"})
	require.ErrorIs(t, err, ErrBusy, "the stubborn node still holds the slot")
	close(p.release)
	require.Eventually(t, func() bool {
		_, err := TestNode(context.Background(), reg, "pass", nil, 1)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
}

// TestTestNodeBoundsInputs: the input and the config are at most
// DryRunMaxInputBytes of JSON each; an output over the per-value cap is the
// result's error.
func TestTestNodeBoundsInputs(t *testing.T) {
	reg := boundsRegistry(t, newBoundsProbe())
	big := strings.Repeat("x", DryRunMaxInputBytes)
	_, err := TestNode(context.Background(), reg, "pass", nil, big)
	require.ErrorIs(t, err, ErrDryRunInput)
	_, err = TestNode(context.Background(), reg, "pass", map[string]any{"x": big}, 1)
	require.ErrorIs(t, err, ErrDryRunInput)

	res, err := TestNode(context.Background(), reg, "big", map[string]any{"bytes": DryRunMaxValueBytes + 1}, 1)
	require.NoError(t, err)
	assert.Contains(t, res.Error, node.ErrSandboxLimit.Error())
	assert.Empty(t, res.Outputs)
}

// TestDryRunLeavesMetricsAlone: dry-run executions, errors and drops are not
// counted in the process-wide metrics.
func TestDryRunLeavesMetricsAlone(t *testing.T) {
	reg := boundsRegistry(t, newBoundsProbe())
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "boom", Category: NodeCategoryProcessing, Risk: node.RiskSafe,
		Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}},
		func(cfg map[string]any) (node.Node, error) { return &boomNode{BaseNode: node.NewBaseNode(cfg)}, nil }))
	before := nodeMetrics(t)
	res, err := DryRun(context.Background(), probeFlow(nil, "pass", "boom"), reg, DryRunInput{TriggerNodeID: "trig"})
	require.NoError(t, err)
	require.Len(t, res.Trace, 3)
	SetDryRunTimeout(t, 50*time.Millisecond)
	p := newBoundsProbe()
	res, err = DryRun(context.Background(), probeFlow(nil, "hold"), boundsRegistry(t, p), DryRunInput{TriggerNodeID: "trig"})
	require.NoError(t, err)
	require.True(t, res.TimedOut)
	assert.Equal(t, before, nodeMetrics(t))
}

// nodeMetrics sums the node execution metrics of the default registry.
func nodeMetrics(t *testing.T) map[string]float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	sums := map[string]float64{}
	for _, mf := range families {
		switch mf.GetName() {
		case "piworker_messages_processed_total", "piworker_messages_dropped_total",
			"piworker_node_execution_errors_total", "piworker_node_retries_total":
			for _, m := range mf.GetMetric() {
				sums[mf.GetName()] += m.GetCounter().GetValue()
			}
		case "piworker_node_execution_duration_seconds":
			for _, m := range mf.GetMetric() {
				sums[mf.GetName()] += float64(m.GetHistogram().GetSampleCount())
			}
		}
	}
	return sums
}

func stepFor(res DryRunResult, nodeID string) (TraceStep, bool) {
	for _, s := range res.Trace {
		if s.NodeID == nodeID {
			return s, true
		}
	}
	return TraceStep{}, false
}
