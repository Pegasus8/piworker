package flow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// closingNode is a processing node holding a resource released by Close.
type closingNode struct {
	*node.BaseNode
	closed   atomic.Int32
	closeErr error
}

func (n *closingNode) Process(context.Context, *Message) ([]*Message, error) { return nil, nil }
func (n *closingNode) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", DataType: DataTypeAny}}, []Port{{ID: "output", DataType: DataTypeAny}}
}
func (n *closingNode) Close() error {
	n.closed.Add(1)
	return n.closeErr
}

// closingTrigger is a trigger that also implements node.Closer.
type closingTrigger struct {
	lifecycleTrigger
	closed atomic.Int32
}

func (t *closingTrigger) Close() error { t.closed.Add(1); return nil }

var (
	_ node.Closer = (*closingNode)(nil)
	_ node.Closer = (*closingTrigger)(nil)
)

func TestStopClosesNodesImplementingCloser(t *testing.T) {
	ok := &closingNode{BaseNode: node.NewBaseNode(nil)}
	failing := &closingNode{BaseNode: node.NewBaseNode(nil), closeErr: errors.New("close failed")}
	trig := &closingTrigger{lifecycleTrigger: lifecycleTrigger{BaseNode: node.NewBaseNode(nil)}}

	reg := node.NewRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "trig", Name: "trig", Category: NodeCategoryInput},
		func(map[string]interface{}) (node.Node, error) { return trig, nil }))
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "ok", Name: "ok", Category: NodeCategoryOutput},
		func(map[string]interface{}) (node.Node, error) { return ok, nil }))
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "failing", Name: "failing", Category: NodeCategoryOutput},
		func(map[string]interface{}) (node.Node, error) { return failing, nil }))

	f := NewFlow("closer")
	f.AddNode(*NewNode("trig", NodeCategoryInput))
	f.AddNode(*NewNode("failing", NodeCategoryOutput))
	f.AddNode(*NewNode("ok", NodeCategoryOutput))

	rt, err := NewFlowRuntime(f, reg, WithLogger(zerolog.Nop()))
	require.NoError(t, err)
	require.NoError(t, rt.Start(context.Background()))
	assert.Zero(t, ok.closed.Load(), "nodes are not closed while running")

	require.NoError(t, rt.Stop(), "a Close error is logged, not returned")
	assert.Equal(t, int32(1), ok.closed.Load())
	assert.Equal(t, int32(1), failing.closed.Load(), "a failing Close doesn't stop the others")
	assert.Equal(t, int32(1), trig.closed.Load(), "trigger nodes implementing Closer are closed too")
}

func TestFailedStartClosesNodes(t *testing.T) {
	n := &closingNode{BaseNode: node.NewBaseNode(nil)}
	bad := &lifecycleTrigger{BaseNode: node.NewBaseNode(nil), startErr: errors.New("nope")}
	reg := lifecycleRegistry(t, map[string]*lifecycleTrigger{"bad": bad})
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "res", Name: "res", Category: NodeCategoryOutput},
		func(map[string]interface{}) (node.Node, error) { return n, nil }))

	f := NewFlow("closer-fail")
	f.AddNode(*NewNode("bad", NodeCategoryInput))
	f.AddNode(*NewNode("res", NodeCategoryOutput))
	rt, err := NewFlowRuntime(f, reg, WithLogger(zerolog.Nop()))
	require.NoError(t, err)
	require.Error(t, rt.Start(context.Background()))
	assert.Equal(t, int32(1), n.closed.Load(), "resources are released when start is aborted")
}
