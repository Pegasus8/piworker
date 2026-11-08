package flow

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// transientFailNode always fails with a transient error and signals its first
// attempt.
type transientFailNode struct {
	*node.BaseNode
	first sync.Once
	tried chan struct{}
	calls atomic.Int32
}

func (n *transientFailNode) Process(context.Context, *Message) ([]*Message, error) {
	n.calls.Add(1)
	n.first.Do(func() { close(n.tried) })
	return nil, node.Transient(errors.New("blip"))
}
func (n *transientFailNode) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", DataType: DataTypeAny}}, []Port{{ID: "output", DataType: DataTypeAny}}
}

// TestRetryBackoffReleasesConcurrencySlot: with a single concurrency slot, a
// node sleeping in retry backoff must not keep other nodes from running.
func TestRetryBackoffReleasesConcurrencySlot(t *testing.T) {
	failing := &transientFailNode{BaseNode: node.NewBaseNode(nil), tried: make(chan struct{})}
	fast := &countNode{BaseNode: node.NewBaseNode(nil)}

	reg := createTestRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "failing", Name: "failing", Category: NodeCategoryProcessing},
		func(map[string]interface{}) (node.Node, error) { return failing, nil }))
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "fast", Name: "fast", Category: NodeCategoryProcessing},
		func(map[string]interface{}) (node.Node, error) { return fast, nil }))

	f := NewFlow("backoff")
	trigA := NewNode("mock-trigger", NodeCategoryInput)
	trigB := NewNode("mock-trigger", NodeCategoryInput)
	fn := passPorts(NewNode("failing", NodeCategoryProcessing))
	qn := passPorts(NewNode("fast", NodeCategoryProcessing))
	for _, n := range []*Node{trigA, trigB, fn, qn} {
		f.AddNode(*n)
	}
	f.AddConnection(*NewConnection(trigA.ID, "output", fn.ID, "input"))
	f.AddConnection(*NewConnection(trigB.ID, "output", qn.ID, "input"))

	const backoff = 600 * time.Millisecond
	rt, err := NewFlowRuntime(f, reg, WithLogger(zerolog.Nop()), WithMaxConcurrency(1),
		WithRetryConfig(RetryConfig{MaxRetries: 1, InitialWait: backoff, MaxWait: backoff, Multiplier: 1}))
	require.NoError(t, err)

	emit := func(out chan<- *Message) {
		m := NewMessage("x", DataTypeString)
		m.SourcePort = "output"
		out <- m
	}
	rt.triggers[trigA.ID].(*mockTriggerNode).sendFunc = emit
	sentB := make(chan time.Time, 1)
	rt.triggers[trigB.ID].(*mockTriggerNode).sendFunc = func(out chan<- *Message) {
		<-failing.tried // the failing node is now in its backoff sleep
		time.Sleep(20 * time.Millisecond)
		sentB <- time.Now()
		emit(out)
	}

	require.NoError(t, rt.Start(context.Background()))
	defer func() { _ = rt.Stop() }()

	start := <-sentB
	require.Eventually(t, func() bool { return fast.calls.Load() == 1 }, 2*time.Second, 2*time.Millisecond)
	assert.Less(t, time.Since(start), backoff/2,
		"the fast node must run while the failing node waits out its backoff, not after it")
	assert.Equal(t, int32(1), failing.calls.Load(), "still in backoff")

	require.Eventually(t, func() bool { return failing.calls.Load() == 2 }, 2*time.Second, 5*time.Millisecond,
		"the retry runs after re-acquiring a slot")
}
