package flow

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// emitTrigger emits `count` messages (each a separate run) on Start.
type emitTrigger struct {
	*node.BaseNode
	count int
}

func newEmitTriggerFactory(count int) node.NodeFactory {
	return func(config map[string]interface{}) (node.Node, error) {
		return &emitTrigger{BaseNode: node.NewBaseNode(config), count: count}, nil
	}
}

func (n *emitTrigger) Process(context.Context, *Message) ([]*Message, error) { return nil, nil }
func (n *emitTrigger) Ports() (inputs, outputs []Port) {
	return nil, []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
}
func (n *emitTrigger) Validate() error { return nil }
func (n *emitTrigger) Stop() error     { return nil }
func (n *emitTrigger) Start(ctx context.Context, out chan<- *Message) error {
	go func() {
		for i := 0; i < n.count; i++ {
			msg := NewMessage(i, DataTypeNumber)
			msg.SourcePort = "output"
			select {
			case out <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

// dropNode swallows its input (emits nothing), like a filter that rejects.
type dropNode struct{ *node.BaseNode }

func newDropNode(config map[string]interface{}) (node.Node, error) {
	return &dropNode{BaseNode: node.NewBaseNode(config)}, nil
}
func (dropNode) Process(context.Context, *Message) ([]*Message, error) { return nil, nil }
func (dropNode) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}},
		[]Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
}
func (dropNode) Validate() error { return nil }

func collectAll() (NodeEventHook, func() []NodeEvent) {
	var mu sync.Mutex
	var got []NodeEvent
	hook := func(e NodeEvent) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	}
	read := func() []NodeEvent {
		mu.Lock()
		defer mu.Unlock()
		out := make([]NodeEvent, len(got))
		copy(out, got)
		return out
	}
	return hook, read
}

func countPhase(events []NodeEvent, phase string) int {
	n := 0
	for _, e := range events {
		if e.Phase == phase {
			n++
		}
	}
	return n
}

func passNode() node.NodeFactory {
	return newMockPassthroughNode
}

func deployAndWait(t *testing.T, f *Flow, registry *node.Registry, read func() []NodeEvent, hook NodeEventHook, wantFinished int) []NodeEvent {
	t.Helper()
	rt, err := NewFlowRuntime(f, registry, WithLogger(zerolog.Nop()), WithObserver(hook))
	require.NoError(t, err)
	require.NoError(t, rt.Start(context.Background()))
	t.Cleanup(func() { _ = rt.Stop() })

	require.Eventually(t, func() bool {
		return countPhase(read(), NodePhaseRunFinished) >= wantFinished
	}, 2*time.Second, 5*time.Millisecond, "expected %d run-finished events", wantFinished)
	// brief settle so we can assert there are not MORE than expected
	time.Sleep(50 * time.Millisecond)
	return read()
}

func TestRunFinishedLinear(t *testing.T) {
	registry := node.NewRegistry()
	require.NoError(t, registry.Register(node.NodeTypeInfo{Type: "emit", Name: "emit", Category: NodeCategoryInput,
		Outputs: []Port{{ID: "output", DataType: DataTypeAny}}}, newEmitTriggerFactory(1)))
	require.NoError(t, registry.Register(node.NodeTypeInfo{Type: "pass", Name: "pass", Category: NodeCategoryProcessing,
		Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}}, passNode()))

	f := NewFlow("linear")
	trig := NewNode("emit", NodeCategoryInput)
	trig.Outputs = []Port{{ID: "output", DataType: DataTypeAny}}
	a := NewNode("pass", NodeCategoryProcessing)
	a.Inputs = []Port{{ID: "input", DataType: DataTypeAny}}
	a.Outputs = []Port{{ID: "output", DataType: DataTypeAny}}
	f.AddNode(*trig)
	f.AddNode(*a)
	f.AddConnection(*NewConnection(trig.ID, "output", a.ID, "input"))

	hook, read := collectAll()
	events := deployAndWait(t, f, registry, read, hook, 1)

	assert.Equal(t, 1, countPhase(events, NodePhaseRunFinished), "exactly one run-finished")
	assert.GreaterOrEqual(t, countPhase(events, NodePhaseSuccess), 1, "the node ran")
}

func TestRunFinishedFanOut(t *testing.T) {
	registry := node.NewRegistry()
	require.NoError(t, registry.Register(node.NodeTypeInfo{Type: "emit", Name: "emit", Category: NodeCategoryInput,
		Outputs: []Port{{ID: "output", DataType: DataTypeAny}}}, newEmitTriggerFactory(1)))
	require.NoError(t, registry.Register(node.NodeTypeInfo{Type: "pass", Name: "pass", Category: NodeCategoryProcessing,
		Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}}, passNode()))

	f := NewFlow("fanout")
	trig := NewNode("emit", NodeCategoryInput)
	trig.Outputs = []Port{{ID: "output", DataType: DataTypeAny}}
	a := NewNode("pass", NodeCategoryProcessing)
	a.Inputs = []Port{{ID: "input", DataType: DataTypeAny}}
	b := NewNode("pass", NodeCategoryProcessing)
	b.Inputs = []Port{{ID: "input", DataType: DataTypeAny}}
	f.AddNode(*trig)
	f.AddNode(*a)
	f.AddNode(*b)
	f.AddConnection(*NewConnection(trig.ID, "output", a.ID, "input"))
	f.AddConnection(*NewConnection(trig.ID, "output", b.ID, "input"))

	hook, read := collectAll()
	events := deployAndWait(t, f, registry, read, hook, 1)

	assert.Equal(t, 1, countPhase(events, NodePhaseRunFinished), "fan-out is still one run")
	assert.Equal(t, 2, countPhase(events, NodePhaseSuccess), "both branches ran")
}

func TestRunFinishedWithDrop(t *testing.T) {
	registry := node.NewRegistry()
	require.NoError(t, registry.Register(node.NodeTypeInfo{Type: "emit", Name: "emit", Category: NodeCategoryInput,
		Outputs: []Port{{ID: "output", DataType: DataTypeAny}}}, newEmitTriggerFactory(1)))
	require.NoError(t, registry.Register(node.NodeTypeInfo{Type: "drop", Name: "drop", Category: NodeCategoryProcessing,
		Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}}, newDropNode))
	require.NoError(t, registry.Register(node.NodeTypeInfo{Type: "pass", Name: "pass", Category: NodeCategoryProcessing,
		Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}}, passNode()))

	f := NewFlow("drop")
	trig := NewNode("emit", NodeCategoryInput)
	trig.Outputs = []Port{{ID: "output", DataType: DataTypeAny}}
	d := NewNode("drop", NodeCategoryProcessing)
	d.Inputs = []Port{{ID: "input", DataType: DataTypeAny}}
	d.Outputs = []Port{{ID: "output", DataType: DataTypeAny}}
	a := NewNode("pass", NodeCategoryProcessing)
	a.Inputs = []Port{{ID: "input", DataType: DataTypeAny}}
	f.AddNode(*trig)
	f.AddNode(*d)
	f.AddNode(*a)
	f.AddConnection(*NewConnection(trig.ID, "output", d.ID, "input"))
	f.AddConnection(*NewConnection(d.ID, "output", a.ID, "input"))

	hook, read := collectAll()
	events := deployAndWait(t, f, registry, read, hook, 1)

	// The drop node emits nothing, so the run still finishes and `pass` never runs.
	assert.Equal(t, 1, countPhase(events, NodePhaseRunFinished))
	assert.Equal(t, 1, countPhase(events, NodePhaseSuccess), "only the drop node ran (no downstream)")
}

func TestRunFinishedConcurrentRuns(t *testing.T) {
	const emissions = 8
	registry := node.NewRegistry()
	require.NoError(t, registry.Register(node.NodeTypeInfo{Type: "emit", Name: "emit", Category: NodeCategoryInput,
		Outputs: []Port{{ID: "output", DataType: DataTypeAny}}}, newEmitTriggerFactory(emissions)))
	require.NoError(t, registry.Register(node.NodeTypeInfo{Type: "pass", Name: "pass", Category: NodeCategoryProcessing,
		Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}}, passNode()))

	f := NewFlow("concurrent")
	trig := NewNode("emit", NodeCategoryInput)
	trig.Outputs = []Port{{ID: "output", DataType: DataTypeAny}}
	a := NewNode("pass", NodeCategoryProcessing)
	a.Inputs = []Port{{ID: "input", DataType: DataTypeAny}}
	f.AddNode(*trig)
	f.AddNode(*a)
	f.AddConnection(*NewConnection(trig.ID, "output", a.ID, "input"))

	hook, read := collectAll()
	events := deployAndWait(t, f, registry, read, hook, emissions)

	// Each trigger emission is an independent run; each finishes exactly once.
	assert.Equal(t, emissions, countPhase(events, NodePhaseRunFinished))
}
