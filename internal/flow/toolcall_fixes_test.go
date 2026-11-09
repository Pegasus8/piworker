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

// blockingTrigger is a callable trigger whose hand-off waits for release (or
// ctx) before sending, like a trigger-tool whose output stays full.
type blockingTrigger struct {
	*node.BaseNode
	release chan struct{}
	mu      sync.Mutex
	out     chan<- *Message
}

func (b *blockingTrigger) Process(context.Context, *Message) ([]*Message, error) { return nil, nil }
func (b *blockingTrigger) Ports() (inputs, outputs []Port) {
	return nil, []Port{{ID: "output", Name: "Output", DataType: DataTypeObject}}
}
func (b *blockingTrigger) Validate() error { return nil }
func (b *blockingTrigger) Start(_ context.Context, out chan<- *Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.out = out
	return nil
}
func (b *blockingTrigger) Stop() error { return nil }
func (b *blockingTrigger) Emit(ctx context.Context, msg *Message) error {
	select {
	case <-b.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	b.mu.Lock()
	out := b.out
	b.mu.Unlock()
	msg.SourcePort = "output"
	select {
	case out <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// blockingFlow is blocking trigger "tool" → processing node "a" of type proc.
func blockingFlow(t *testing.T, proc node.NodeFactory) (*FlowRuntime, *blockingTrigger) {
	t.Helper()
	trig := &blockingTrigger{BaseNode: node.NewBaseNode(nil), release: make(chan struct{})}
	reg := callRegistryFor(t, map[string]node.NodeFactory{"proc": proc})
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "blocking", Name: "blocking", Category: NodeCategoryInput,
		Outputs: []Port{{ID: "output", DataType: DataTypeObject}}},
		func(map[string]interface{}) (node.Node, error) { return trig, nil }))
	f := NewFlow("blocking")
	tn := NewNode("blocking", NodeCategoryInput)
	tn.ID = "tool"
	a := NewNode("proc", NodeCategoryProcessing)
	a.ID = "a"
	f.AddNode(*tn)
	f.AddNode(*a)
	f.AddConnection(*NewConnection("tool", "output", "a", "input"))
	return startRuntime(t, f, reg), trig
}

type callOutcome struct {
	res CallResult
	err error
}

func callAsync(rt *FlowRuntime, ctx context.Context, req CallRequest) <-chan callOutcome {
	ch := make(chan callOutcome, 1)
	go func() {
		res, err := rt.Call(ctx, req)
		ch <- callOutcome{res, err}
	}()
	return ch
}

// A hand-off that does not complete within the call's timeout fails the call
// as busy instead of blocking the caller past its timeout; nothing is tracked.
func TestCallHandOffIsBoundedByTheTimeout(t *testing.T) {
	rt, trig := blockingFlow(t, funcFactory(passFn))
	defer close(trig.release)

	start := time.Now()
	var out callOutcome
	select {
	case out = <-callAsync(rt, context.Background(), CallRequest{TriggerNodeID: "tool", Timeout: 50 * time.Millisecond}):
	case <-time.After(3 * time.Second):
		t.Fatal("Call blocked on the trigger hand-off past its timeout")
	}
	require.ErrorIs(t, out.err, ErrTriggerBusy)
	assert.ErrorIs(t, out.err, ErrBusy, "callers map every busy refusal with one errors.Is")
	assert.Empty(t, out.res.RunID, "no run was started")
	assert.Less(t, time.Since(start), 2*time.Second)
	assert.Equal(t, 0, rt.calls.count(), "the waiter is removed")
}

// Every "busy, retry later" refusal of Call wraps ErrBusy, so the service maps
// them to the busy tool error with a single errors.Is; other errors do not.
func TestBusyRefusalsWrapErrBusy(t *testing.T) {
	assert.ErrorIs(t, ErrTooManyCalls, ErrBusy)
	assert.ErrorIs(t, ErrTriggerBusy, ErrBusy)
	for _, err := range []error{ErrFlowNotRunning, ErrNodeNotFound, ErrTriggerNotCallable, ErrNoActiveTargets} {
		assert.NotErrorIs(t, err, ErrBusy, "%v is not a busy refusal", err)
	}
}

// The caller's own cancellation during the hand-off is reported as such, not
// as busy.
func TestCallHandOffCancelledByCaller(t *testing.T) {
	rt, trig := blockingFlow(t, funcFactory(passFn))
	defer close(trig.release)

	ctx, cancel := context.WithCancel(context.Background())
	pending := callAsync(rt, ctx, CallRequest{TriggerNodeID: "tool", Timeout: 5 * time.Second})
	time.Sleep(20 * time.Millisecond)
	cancel()
	out := <-pending
	require.ErrorIs(t, out.err, context.Canceled)
	assert.NotErrorIs(t, out.err, ErrTriggerBusy)
	assert.Equal(t, 0, rt.calls.count())
}

// The timeout covers the hand-off and the wait together: a slow hand-off
// leaves less time to wait for the reply.
func TestCallTimeoutIncludesTheHandOff(t *testing.T) {
	gate := make(chan struct{})
	defer close(gate)
	rt, trig := blockingFlow(t, funcFactory(gateFn(gate, replyFn(echoPayload, false))))
	time.AfterFunc(500*time.Millisecond, func() { close(trig.release) })

	start := time.Now()
	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Timeout: 700 * time.Millisecond})
	require.NoError(t, err)
	assert.Equal(t, CallStatusRunning, res.Status)
	assert.Less(t, time.Since(start), 1100*time.Millisecond, "the reply wait must not restart the timeout after the hand-off")
}

// countingMarshaler counts how often it is serialized.
type countingMarshaler struct{ n *atomic.Int32 }

func (c countingMarshaler) MarshalJSON() ([]byte, error) {
	c.n.Add(1)
	return []byte(`1`), nil
}

// Replies outside a called run (most runs) cost no serialization.
func TestCallRegistryReplyWithoutWaiterDoesNotSerialize(t *testing.T) {
	reg := newTestRegistry(t)
	var n atomic.Int32
	assert.False(t, reg.Reply("not-a-call", countingMarshaler{&n}, false))
	assert.Equal(t, int32(0), n.Load())

	_, err := reg.register("run-1", time.Second)
	require.NoError(t, err)
	assert.True(t, reg.Reply("run-1", countingMarshaler{&n}, false))
	assert.Equal(t, int32(1), n.Load())
	assert.False(t, reg.Reply("run-1", countingMarshaler{&n}, false), "already final")
	assert.Equal(t, int32(1), n.Load(), "a final call is not serialized again")
}

// A message of a called run that is dropped before it executes makes the
// call an error (unless a reply already won), instead of a silent
// completed_no_reply.
func TestCallRegistryDropIsAnError(t *testing.T) {
	reg := newTestRegistry(t)
	w, err := reg.register("run-1", time.Second)
	require.NoError(t, err)

	reg.dropped("run-1", "slow", "type-a", "queue full")
	reg.dropped("run-1", "other", "type-b", "second drop")
	reg.dropped("not-a-call", "x", "y", "ignored")
	reg.runFinished("run-1")
	res := reg.snapshot(w)
	assert.Equal(t, CallStatusError, res.Status)
	assert.Equal(t, &CallError{NodeID: "slow", NodeType: "type-a", Message: "message dropped at node slow: queue full"}, res.Error)

	w2, err := reg.register("run-2", time.Second)
	require.NoError(t, err)
	require.True(t, reg.Reply("run-2", "ok", false))
	reg.dropped("run-2", "slow", "type-a", "queue full")
	reg.runFinished("run-2")
	assert.Equal(t, CallStatusCompleted, reg.snapshot(w2).Status, "a reply that already won stays")
}

// fanOut emits n copies of its input.
func fanOut(n int) func(context.Context, *Message) ([]*Message, error) {
	return func(_ context.Context, msg *Message) ([]*Message, error) {
		outs := make([]*Message, n)
		for i := range outs {
			outs[i] = msg.Clone()
			outs[i].SourcePort = "output"
		}
		return outs, nil
	}
}

func TestCallReportsMessagesDroppedByAFullQueue(t *testing.T) {
	gate := make(chan struct{})
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"fan":  funcFactory(fanOut(6)),
		"slow": funcFactory(gateFn(gate, func(context.Context, *Message) ([]*Message, error) { return nil, nil })),
	})
	f := callGraph(map[string]string{"tool": "tool", "fan": "fan", "slow": "slow"}, [2]string{"tool", "fan"}, [2]string{"fan", "slow"})
	rt := startRuntime(t, f, reg, WithNodeQueueLimit(1))

	pending := callAsync(rt, context.Background(), CallRequest{TriggerNodeID: "tool", Timeout: 5 * time.Second})
	require.Eventually(t, func() bool { return rt.DroppedMessages() > 0 }, 3*time.Second, 5*time.Millisecond)
	close(gate)
	out := <-pending
	require.NoError(t, out.err)
	assert.Equal(t, CallStatusError, out.res.Status)
	require.NotNil(t, out.res.Error)
	assert.Equal(t, "slow", out.res.Error.NodeID)
	assert.Equal(t, "slow", out.res.Error.NodeType)
	assert.Contains(t, out.res.Error.Message, "message dropped at node slow: ")
	assert.Contains(t, out.res.Error.Message, "queue is full")
}

func TestCallReplyWinsOverLaterDrops(t *testing.T) {
	gate := make(chan struct{})
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"replyfan": funcFactory(func(ctx context.Context, msg *Message) ([]*Message, error) {
			ReplyFromContext(ctx).Reply(corrOf(msg), "done", false)
			return fanOut(6)(ctx, msg)
		}),
		"slow": funcFactory(gateFn(gate, func(context.Context, *Message) ([]*Message, error) { return nil, nil })),
	})
	f := callGraph(map[string]string{"tool": "tool", "r": "replyfan", "slow": "slow"}, [2]string{"tool", "r"}, [2]string{"r", "slow"})
	rt := startRuntime(t, f, reg, WithNodeQueueLimit(1))

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Timeout: 5 * time.Second})
	require.NoError(t, err)
	assert.Equal(t, CallStatusCompleted, res.Status)
	require.Eventually(t, func() bool { return rt.DroppedMessages() > 0 }, 3*time.Second, 5*time.Millisecond)
	close(gate)
	waitClosed(t, rt.RunDone(res.RunID), "the run still finishes")
}

// A called run whose message the router drops on its send timeout (the
// dispatcher is stalled and the router output stays full) ends as an error
// naming the node and the reason, through the drop handler NewFlowRuntime
// wires into the router.
func TestCallReportsMessagesDroppedByARoutingTimeout(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"fan":  funcFactory(fanOut(3)),
		"sink": funcFactory(func(context.Context, *Message) ([]*Message, error) { return nil, nil }),
	})
	f := callGraph(map[string]string{"tool": "tool", "fan": "fan", "sink": "sink"}, [2]string{"tool", "fan"}, [2]string{"fan", "sink"})
	rt, err := NewFlowRuntime(f, reg, WithLogger(zerolog.Nop()))
	require.NoError(t, err)
	// Before Start (no goroutine reads them yet): a one-slot router output
	// and a short send timeout, so a stalled dispatcher makes the router drop.
	rt.router.output = make(chan RoutedMessage, 1)
	rt.router.sendTimeout = 20 * time.Millisecond
	require.NoError(t, rt.Start(context.Background()))
	t.Cleanup(func() { _ = rt.Stop() })

	// Stall the dispatcher on its hand-off to sink's queue.
	sinkQueue := rt.queues["sink"]
	sinkQueue.mu.Lock()
	locked := true
	defer func() {
		if locked {
			sinkQueue.mu.Unlock()
		}
	}()

	pending := callAsync(rt, context.Background(), CallRequest{TriggerNodeID: "tool", Timeout: 5 * time.Second})
	require.Eventually(t, func() bool { return rt.router.MessagesDropped() > 0 }, 3*time.Second, 5*time.Millisecond)
	sinkQueue.mu.Unlock()
	locked = false

	out := <-pending
	require.NoError(t, out.err)
	assert.Equal(t, CallStatusError, out.res.Status)
	require.NotNil(t, out.res.Error)
	assert.Equal(t, "sink", out.res.Error.NodeID)
	assert.Equal(t, "sink", out.res.Error.NodeType)
	assert.Equal(t, "message dropped at node sink: routing timed out (the flow is overloaded)", out.res.Error.Message)
	assert.Positive(t, rt.DroppedMessages(), "the runtime counts the router's drop")
	waitClosed(t, rt.RunDone(out.res.RunID), "the run still finishes")
}

// A called run whose message targets a node that is not running (no queue)
// ends as an error naming that node, through dispatch's unknown-target path.
func TestCallReportsMessagesForANodeThatIsNotRunning(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"sink": funcFactory(replyFn(echoPayload, false)),
	})
	f := callGraph(map[string]string{"tool": "tool", "sink": "sink"}, [2]string{"tool", "sink"})
	rt, err := NewFlowRuntime(f, reg, WithLogger(zerolog.Nop()))
	require.NoError(t, err)
	// The router still routes to sink, but Start creates no queue for it:
	// the state dispatch guards against (a target that is not running).
	delete(rt.nodes, "sink")
	require.NoError(t, rt.Start(context.Background()))
	t.Cleanup(func() { _ = rt.Stop() })

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Timeout: 5 * time.Second})
	require.NoError(t, err)
	assert.Equal(t, CallStatusError, res.Status)
	require.NotNil(t, res.Error)
	assert.Equal(t, "sink", res.Error.NodeID)
	assert.Equal(t, "sink", res.Error.NodeType)
	assert.Equal(t, "message dropped at node sink: the node is not running (not found or disabled)", res.Error.Message)
	assert.Equal(t, int64(1), rt.DroppedMessages())
}
