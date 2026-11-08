package flow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/Pegasus8/piworker/internal/node"
	dto "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNegativeMaxRetriesStillExecutesOnce guards against a retry config with a
// negative MaxRetries skipping execution entirely and reporting (nil, nil).
func TestNegativeMaxRetriesStillExecutesOnce(t *testing.T) {
	rt, cn := buildRetryFlow(t, nil)
	WithRetryConfig(RetryConfig{MaxRetries: -1, InitialWait: time.Millisecond, MaxWait: time.Millisecond, Multiplier: 2})(rt)
	require.NoError(t, rt.Start(context.Background()))
	defer func() { _ = rt.Stop() }()

	require.Eventually(t, func() bool { return atomic.LoadInt32(&cn.calls) == 1 },
		time.Second, 5*time.Millisecond, "a negative MaxRetries must still run the node once")
}

func TestNegativeMaxRetriesReturnsError(t *testing.T) {
	rt, cn := buildRetryFlow(t, errors.New("boom"))
	rt.ctx, rt.cancel = context.WithCancel(context.Background())
	defer rt.cancel()
	rt.retryConfig.MaxRetries = -5

	def := rt.nodeDefs[findNodeIDByType(t, rt, "counting")]
	_, err := rt.executeNodeWithRetry(cn, RoutedMessage{Message: NewMessage("x", DataTypeString), TargetNode: def.ID}, def, zerolog.Nop(), nil)
	assert.EqualError(t, err, "boom")
	assert.Equal(t, int32(1), atomic.LoadInt32(&cn.calls))
}

func findNodeIDByType(t *testing.T, rt *FlowRuntime, typ string) string {
	t.Helper()
	for id, def := range rt.nodeDefs {
		if def.Type == typ {
			return id
		}
	}
	t.Fatalf("no node of type %s", typ)
	return ""
}

// freshOutputNode emits a brand-new message (no metadata) instead of cloning
// its input, as many real nodes do.
type freshOutputNode struct{ *node.BaseNode }

func newFreshOutputNode(config map[string]interface{}) (node.Node, error) {
	return &freshOutputNode{BaseNode: node.NewBaseNode(config)}, nil
}
func (freshOutputNode) Process(_ context.Context, msg *Message) ([]*Message, error) {
	out := NewMessage(msg.Payload, DataTypeAny)
	out.SourcePort = "output"
	return []*Message{out}, nil
}
func (freshOutputNode) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}},
		[]Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
}

// testRegistry registers the emit trigger plus the given processing types.
func testRegistry(t *testing.T, emits int, procs map[string]node.NodeFactory) *node.Registry {
	t.Helper()
	reg := node.NewRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "emit", Name: "emit", Category: NodeCategoryInput,
		Outputs: []Port{{ID: "output", DataType: DataTypeAny}}}, newEmitTriggerFactory(emits)))
	for typ, f := range procs {
		require.NoError(t, reg.Register(node.NodeTypeInfo{Type: typ, Name: typ, Category: NodeCategoryProcessing,
			Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}}, f))
	}
	return reg
}

// chainFlow builds trigger -> types[0] -> types[1] -> ... and returns the flow
// plus the node IDs in order (trigger first).
func chainFlow(trigType string, types ...string) (*Flow, []string) {
	f := NewFlow("chain")
	trig := NewNode(trigType, NodeCategoryInput)
	trig.Outputs = []Port{{ID: "output", DataType: DataTypeAny}}
	f.AddNode(*trig)
	ids := []string{trig.ID}
	prev := trig.ID
	for _, typ := range types {
		n := NewNode(typ, NodeCategoryProcessing)
		n.Inputs = []Port{{ID: "input", DataType: DataTypeAny}}
		n.Outputs = []Port{{ID: "output", DataType: DataTypeAny}}
		f.AddNode(*n)
		f.AddConnection(*NewConnection(prev, "output", n.ID, "input"))
		ids = append(ids, n.ID)
		prev = n.ID
	}
	return f, ids
}

// TestOutputsInheritCorrelationID verifies that a node emitting a fresh message
// (no correlationID meta) keeps the run's correlation ID, so the whole cascade
// is one run and the run counter balances.
func TestOutputsInheritCorrelationID(t *testing.T) {
	reg := testRegistry(t, 1, map[string]node.NodeFactory{"fresh": newFreshOutputNode, "pass": passNode()})
	f, _ := chainFlow("emit", "fresh", "pass")

	hook, read := collectAll()
	events := deployAndWait(t, f, reg, read, hook, 1)

	corr := map[string]bool{}
	for _, e := range events {
		if e.Phase == NodePhaseSuccess {
			corr[e.CorrID] = true
		}
	}
	assert.Equal(t, 2, countPhase(events, NodePhaseSuccess))
	assert.Len(t, corr, 1, "all node executions in a cascade share the run's correlation ID")
	assert.Equal(t, 1, countPhase(events, NodePhaseRunFinished), "exactly one run, finished once")
}

// TestStopAllDecrementsRunningGauge checks StopAll keeps the flows_running
// gauge consistent with Deploy/Undeploy.
func TestStopAllDecrementsRunningGauge(t *testing.T) {
	reg := createTestRegistry()
	m := NewRuntimeManager(reg, zerolog.Nop())
	before := gaugeValue(t)

	require.NoError(t, m.Deploy(context.Background(), createSimpleFlow()))
	require.NoError(t, m.Deploy(context.Background(), createSimpleFlow()))
	assert.Equal(t, before+2, gaugeValue(t))

	m.StopAll()
	assert.Equal(t, before, gaugeValue(t))
}

func gaugeValue(t *testing.T) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, metrics.FlowsRunning.Write(&m))
	return m.GetGauge().GetValue()
}
