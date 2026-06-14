package flow

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPassthroughNode is a clean action that echoes its input, with none of the
// WaitGroup bookkeeping of mockActionNode (which panics if driven directly).
type mockPassthroughNode struct{ *node.BaseNode }

func newMockPassthroughNode(config map[string]interface{}) (node.Node, error) {
	return &mockPassthroughNode{BaseNode: node.NewBaseNode(config)}, nil
}

func (n *mockPassthroughNode) Process(ctx context.Context, msg *Message) ([]*Message, error) {
	out := msg.Clone()
	out.SourcePort = "output"
	return []*Message{out}, nil
}

func (n *mockPassthroughNode) Ports() (inputs []Port, outputs []Port) {
	return []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}},
		[]Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
}

// mockPanicNode panics inside Process to exercise the recovery path.
type mockPanicNode struct{ *node.BaseNode }

func newMockPanicNode(config map[string]interface{}) (node.Node, error) {
	return &mockPanicNode{BaseNode: node.NewBaseNode(config)}, nil
}

func (n *mockPanicNode) Process(ctx context.Context, msg *Message) ([]*Message, error) {
	panic("kaboom")
}

func (n *mockPanicNode) Ports() (inputs []Port, outputs []Port) {
	return []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}},
		[]Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
}

func registerActionNode(t *testing.T, typ string, factory node.NodeFactory) *node.Registry {
	t.Helper()
	registry := node.NewRegistry()
	require.NoError(t, registry.Register(node.NodeTypeInfo{
		Type: typ, Name: typ, Category: NodeCategoryOutput,
		Inputs:  []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}},
		Outputs: []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}},
	}, factory))
	return registry
}

// collectObserver returns an observer hook plus a function to read what it saw.
func collectObserver() (NodeEventHook, func() []NodeEvent) {
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

func TestObserverEmitsRunningThenSuccess(t *testing.T) {
	registry := registerActionNode(t, "mock-pass", newMockPassthroughNode)

	f := NewFlow("pass flow")
	action := NewNode("mock-pass", NodeCategoryOutput)
	f.AddNode(*action)

	hook, read := collectObserver()
	rt, err := NewFlowRuntime(f, registry, WithLogger(zerolog.Nop()), WithObserver(hook))
	require.NoError(t, err)
	rt.ctx = context.Background()

	rt.executeNode(RoutedMessage{Message: NewMessage("hello", DataTypeString), TargetNode: action.ID, TargetPort: "input"})

	events := read()
	require.Len(t, events, 2)
	assert.Equal(t, NodePhaseRunning, events[0].Phase)
	assert.Equal(t, NodePhaseSuccess, events[1].Phase)

	done := events[1]
	assert.Equal(t, action.ID, done.NodeID)
	assert.Equal(t, "mock-pass", done.NodeType)
	assert.Equal(t, f.ID, done.FlowID)
	assert.NotEmpty(t, done.CorrID, "correlation id must be propagated")
	assert.NotNil(t, done.Input, "input message must be carried for the debug inspector")
	require.Len(t, done.Outputs, 1)
	assert.GreaterOrEqual(t, done.Duration, time.Duration(0))
	assert.NoError(t, done.Err)
}

func TestObserverEmitsErrorOnPanic(t *testing.T) {
	registry := registerActionNode(t, "mock-panic", newMockPanicNode)

	f := NewFlow("panic flow")
	panicNode := NewNode("mock-panic", NodeCategoryOutput)
	f.AddNode(*panicNode)

	hook, read := collectObserver()
	rt, err := NewFlowRuntime(f, registry, WithLogger(zerolog.Nop()), WithObserver(hook))
	require.NoError(t, err)
	rt.ctx = context.Background()

	// A panic inside Process must be recovered AND surfaced as a terminal error
	// event, so the editor never shows the node stuck as "running".
	assert.NotPanics(t, func() {
		rt.executeNode(RoutedMessage{Message: NewMessage(nil, DataTypeAny), TargetNode: panicNode.ID, TargetPort: "input"})
	})

	events := read()
	require.Len(t, events, 2)
	assert.Equal(t, NodePhaseRunning, events[0].Phase)
	assert.Equal(t, NodePhaseError, events[1].Phase)
	require.Error(t, events[1].Err)
	assert.Contains(t, events[1].Err.Error(), "kaboom")
}

func TestObserverEmitsErrorPhase(t *testing.T) {
	registry := node.NewRegistry()
	require.NoError(t, registry.Register(node.NodeTypeInfo{
		Type: "mock-error", Name: "Mock Error", Category: NodeCategoryOutput,
		Inputs:  []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}},
		Outputs: []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}},
	}, newMockErrorNode(errors.New("boom"))))

	f := NewFlow("err flow")
	errNode := NewNode("mock-error", NodeCategoryOutput)
	f.AddNode(*errNode)

	hook, read := collectObserver()
	rt, err := NewFlowRuntime(f, registry, WithLogger(zerolog.Nop()), WithObserver(hook))
	require.NoError(t, err)
	rt.ctx = context.Background()

	rt.executeNode(RoutedMessage{Message: NewMessage(nil, DataTypeAny), TargetNode: errNode.ID, TargetPort: "input"})

	events := read()
	require.Len(t, events, 2)
	assert.Equal(t, NodePhaseRunning, events[0].Phase)
	assert.Equal(t, NodePhaseError, events[1].Phase)
	require.Error(t, events[1].Err)
	assert.Contains(t, events[1].Err.Error(), "boom")
	assert.Empty(t, events[1].Outputs)
}

func TestManagerForwardsObserverToDeployedRuntime(t *testing.T) {
	registry := createTestRegistry()
	hook, _ := collectObserver()
	mgr := NewRuntimeManager(registry, zerolog.Nop(), WithManagerObserver(hook))

	f := createSimpleFlow()
	require.NoError(t, mgr.Deploy(context.Background(), f))
	t.Cleanup(func() { _ = mgr.Undeploy(f.ID) })

	rt, ok := mgr.GetRuntime(f.ID)
	require.True(t, ok)
	assert.NotNil(t, rt.observer, "deployed runtime must receive the manager's observer")
}

func TestObserverAbsentIsNoop(t *testing.T) {
	registry := createTestRegistry()
	f := createSimpleFlow()
	rt, err := NewFlowRuntime(f, registry, WithLogger(zerolog.Nop()))
	require.NoError(t, err)
	rt.ctx = context.Background()

	assert.NotPanics(t, func() {
		rt.executeNode(RoutedMessage{Message: NewMessage("x", DataTypeString), TargetNode: f.Nodes[1].ID, TargetPort: "input"})
	})
}
