package flow

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gateNode blocks every Process call until release is closed (or ctx ends).
type gateNode struct {
	*node.BaseNode
	release chan struct{}
	calls   atomic.Int32
}

func (n *gateNode) Process(ctx context.Context, msg *Message) ([]*Message, error) {
	n.calls.Add(1)
	select {
	case <-n.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return nil, nil
}
func (n *gateNode) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", DataType: DataTypeAny}}, []Port{{ID: "output", DataType: DataTypeAny}}
}

// countNode counts the messages it processed.
type countNode struct {
	*node.BaseNode
	calls atomic.Int32
}

func (n *countNode) Process(context.Context, *Message) ([]*Message, error) {
	n.calls.Add(1)
	return nil, nil
}
func (n *countNode) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", DataType: DataTypeAny}}, []Port{{ID: "output", DataType: DataTypeAny}}
}

// fanOutSlowFast wires trigger -> {slow gate node, fast counting node}.
func fanOutSlowFast(t *testing.T, emits int, opts ...RuntimeOption) (*FlowRuntime, *gateNode, *countNode, func() []NodeEvent) {
	t.Helper()
	slow := &gateNode{BaseNode: node.NewBaseNode(nil), release: make(chan struct{})}
	fast := &countNode{BaseNode: node.NewBaseNode(nil)}
	reg := testRegistry(t, emits, map[string]node.NodeFactory{
		"slow": func(map[string]interface{}) (node.Node, error) { return slow, nil },
		"fast": func(map[string]interface{}) (node.Node, error) { return fast, nil },
	})

	f := NewFlow("fanout")
	trig := NewNode("emit", NodeCategoryInput)
	s := passPorts(NewNode("slow", NodeCategoryProcessing))
	q := passPorts(NewNode("fast", NodeCategoryProcessing))
	f.AddNode(*trig)
	f.AddNode(*s)
	f.AddNode(*q)
	f.AddConnection(*NewConnection(trig.ID, "output", s.ID, "input"))
	f.AddConnection(*NewConnection(trig.ID, "output", q.ID, "input"))

	hook, read := collectAll()
	opts = append([]RuntimeOption{WithLogger(zerolog.Nop()), WithObserver(hook)}, opts...)
	rt, err := NewFlowRuntime(f, reg, opts...)
	require.NoError(t, err)
	return rt, slow, fast, read
}

// TestSlowNodeDoesNotBlockSiblingBranch is the head-of-line regression: a
// blocked node with a full input must not stop messages reaching an unrelated
// sibling branch.
func TestSlowNodeDoesNotBlockSiblingBranch(t *testing.T) {
	const emits = 600 // well beyond the old 256-slot per-node channel
	rt, slow, fast, _ := fanOutSlowFast(t, emits)
	require.NoError(t, rt.Start(context.Background()))
	var once sync.Once
	releaseSlow := func() { once.Do(func() { close(slow.release) }) }
	defer func() { releaseSlow(); _ = rt.Stop() }()

	require.Eventually(t, func() bool { return fast.calls.Load() == emits }, 3*time.Second, 5*time.Millisecond,
		"fast branch must process every message while the slow branch is blocked")
	assert.Equal(t, int32(1), slow.calls.Load(), "slow node is still stuck on its first message")
}

// TestNodeQueueOverflowDropsAndFinishesRuns caps a node's queue: overflow is
// dropped (and counted) rather than blocking, and every run still finishes.
func TestNodeQueueOverflowDropsAndFinishesRuns(t *testing.T) {
	const emits = 20
	rt, slow, fast, read := fanOutSlowFast(t, emits, WithNodeQueueLimit(3))
	require.NoError(t, rt.Start(context.Background()))
	defer func() { _ = rt.Stop() }()

	// The slow node holds one message and queues 3; the rest of its branch
	// overflows. (The fast node's small queue may overflow too under a burst.)
	require.Eventually(t, func() bool { return rt.DroppedMessages() >= emits-4 }, 3*time.Second, 5*time.Millisecond)

	close(slow.release)
	require.Eventually(t, func() bool { return countPhase(read(), NodePhaseRunFinished) == emits },
		3*time.Second, 5*time.Millisecond, "every run must finish even when some of its messages were dropped")
	time.Sleep(20 * time.Millisecond)
	processed := int64(slow.calls.Load() + fast.calls.Load())
	assert.Equal(t, int64(2*emits), processed+rt.DroppedMessages(), "each routed message is either processed or dropped")
	assert.Equal(t, emits, countPhase(read(), NodePhaseRunFinished), "each run finishes exactly once")
}
