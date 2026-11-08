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
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toolTrigger is a callable trigger (node.Emitter), standing in for
// trigger-tool: Emit is a blocking, ctx-bounded send into the trigger's output.
type toolTrigger struct {
	*node.BaseNode
	mu     sync.Mutex
	out    chan<- *Message
	runCtx context.Context
	// spare adds a second output port "spare" and emits on it.
	spare bool
}

var _ node.Emitter = (*toolTrigger)(nil)

func newToolTrigger(config map[string]interface{}) (node.Node, error) {
	return &toolTrigger{BaseNode: node.NewBaseNode(config)}, nil
}

func (n *toolTrigger) Process(context.Context, *Message) ([]*Message, error) { return nil, nil }
func (n *toolTrigger) Ports() (inputs, outputs []Port) {
	outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeObject}}
	if n.spare {
		outputs = append(outputs, Port{ID: "spare", Name: "Spare", DataType: DataTypeObject})
	}
	return nil, outputs
}
func (n *toolTrigger) Validate() error { return nil }
func (n *toolTrigger) Start(ctx context.Context, out chan<- *Message) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.out, n.runCtx = out, ctx
	return nil
}
func (n *toolTrigger) Stop() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.out = nil
	return nil
}
func (n *toolTrigger) Emit(ctx context.Context, msg *Message) error {
	n.mu.Lock()
	out, runCtx := n.out, n.runCtx
	n.mu.Unlock()
	if out == nil {
		return errors.New("tool trigger is not running")
	}
	msg.SourcePort = "output"
	if n.spare {
		msg.SourcePort = "spare"
	}
	msg.SetMeta("toolName", "test_tool")
	select {
	case out <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-runCtx.Done():
		return runCtx.Err()
	}
}

// funcNode is a processing node whose Process is a test-supplied function.
type funcNode struct {
	*node.BaseNode
	fn func(ctx context.Context, msg *Message) ([]*Message, error)
}

func funcFactory(fn func(ctx context.Context, msg *Message) ([]*Message, error)) node.NodeFactory {
	return func(config map[string]interface{}) (node.Node, error) {
		return &funcNode{BaseNode: node.NewBaseNode(config), fn: fn}, nil
	}
}

func (n *funcNode) Process(ctx context.Context, msg *Message) ([]*Message, error) {
	return n.fn(ctx, msg)
}
func (n *funcNode) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}},
		[]Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
}

func corrOf(msg *Message) string {
	v, _ := msg.GetMeta("correlationID")
	s, _ := v.(string)
	return s
}

// replyFn replies with payload(msg) and emits nothing.
func replyFn(payload func(*Message) any, isError bool) func(context.Context, *Message) ([]*Message, error) {
	return func(ctx context.Context, msg *Message) ([]*Message, error) {
		ReplyFromContext(ctx).Reply(corrOf(msg), payload(msg), isError)
		return nil, nil
	}
}

func echoPayload(msg *Message) any { return msg.Payload }

// gateFn blocks until gate is closed (or the flow stops), then runs next.
func gateFn(gate <-chan struct{}, next func(context.Context, *Message) ([]*Message, error)) func(context.Context, *Message) ([]*Message, error) {
	return func(ctx context.Context, msg *Message) ([]*Message, error) {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return next(ctx, msg)
	}
}

func passFn(_ context.Context, msg *Message) ([]*Message, error) {
	out := msg.Clone()
	out.SourcePort = "output"
	return []*Message{out}, nil
}

// callRegistryFor registers the callable "tool" trigger, a non-callable "emit"
// trigger (emits nothing) and the given processing types.
func callRegistryFor(t *testing.T, procs map[string]node.NodeFactory) *node.Registry {
	t.Helper()
	reg := node.NewRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "tool", Name: "tool", Category: NodeCategoryInput,
		Outputs: []Port{{ID: "output", DataType: DataTypeObject}}}, newToolTrigger))
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "emit", Name: "emit", Category: NodeCategoryInput,
		Outputs: []Port{{ID: "output", DataType: DataTypeAny}}}, newEmitTriggerFactory(0)))
	for typ, f := range procs {
		require.NoError(t, reg.Register(node.NodeTypeInfo{Type: typ, Name: typ, Category: NodeCategoryProcessing,
			Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}}, f))
	}
	return reg
}

// callGraph builds a flow whose node IDs are the given names (name -> type)
// with output->input edges between them.
func callGraph(nodes map[string]string, edges ...[2]string) *Flow {
	f := NewFlow("call")
	names := make([]string, 0, len(nodes))
	for name := range nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		typ := nodes[name]
		cat := NodeCategoryProcessing
		if typ == "tool" || typ == "emit" {
			cat = NodeCategoryInput
		}
		n := NewNode(typ, cat)
		n.ID = name
		f.AddNode(*n)
	}
	for _, e := range edges {
		f.AddConnection(*NewConnection(e[0], "output", e[1], "input"))
	}
	return f
}

func startRuntime(t *testing.T, f *Flow, reg *node.Registry, opts ...RuntimeOption) *FlowRuntime {
	t.Helper()
	opts = append([]RuntimeOption{WithLogger(zerolog.Nop())}, opts...)
	rt, err := NewFlowRuntime(f, reg, opts...)
	require.NoError(t, err)
	require.NoError(t, rt.Start(context.Background()))
	t.Cleanup(func() { _ = rt.Stop() })
	return rt
}

func waitClosed(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal(msg)
	}
}

func TestCallReplyCompletes(t *testing.T) {
	metaCh := make(chan map[string]any, 1)
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"reply": funcFactory(func(ctx context.Context, msg *Message) ([]*Message, error) {
			metaCh <- msg.Meta
			ReplyFromContext(ctx).Reply(corrOf(msg), map[string]any{"echo": msg.Payload}, false)
			return nil, nil
		}),
	})
	f := callGraph(map[string]string{"tool": "tool", "r": "reply"}, [2]string{"tool", "r"})
	rt := startRuntime(t, f, reg)

	res, err := rt.Call(context.Background(), CallRequest{
		TriggerNodeID: "tool", Args: map[string]any{"x": 1.0}, Actor: "token:bot", Timeout: 3 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, CallStatusCompleted, res.Status)
	assert.JSONEq(t, `{"echo":{"x":1}}`, string(res.Result))
	assert.Nil(t, res.Error)
	assert.GreaterOrEqual(t, res.DurationMs, int64(0))
	_, perr := uuid.Parse(res.RunID)
	assert.NoError(t, perr, "the run ID is a server-generated UUID")

	meta := <-metaCh
	assert.Equal(t, res.RunID, meta["correlationID"], "the run ID is the run's correlation ID")
	assert.Equal(t, "token:bot", meta["actor"])
	assert.Equal(t, "test_tool", meta["toolName"], "the emitter adds its own meta")

	waitClosed(t, rt.RunDone(res.RunID), "RunDone must close once the run finished")
	// Call handed the result over: once the run is over nothing is kept.
	require.Eventually(t, func() bool { return rt.calls.count() == 0 }, 2*time.Second, 5*time.Millisecond)
	again, err := rt.Wait(context.Background(), res.RunID)
	require.NoError(t, err)
	assert.Equal(t, CallStatusExpired, again.Status, "a delivered result is not kept for Wait")
}

func TestCallArgsAreCopied(t *testing.T) {
	gate := make(chan struct{})
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"reply": funcFactory(gateFn(gate, replyFn(echoPayload, false))),
	})
	f := callGraph(map[string]string{"tool": "tool", "r": "reply"}, [2]string{"tool", "r"})
	rt := startRuntime(t, f, reg)

	args := map[string]any{"x": "original"}
	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Args: args, Timeout: 10 * time.Millisecond})
	require.NoError(t, err)
	require.Equal(t, CallStatusRunning, res.Status)
	args["x"] = "mutated by the caller" // the caller owns its map again
	close(gate)

	final, err := rt.Wait(context.Background(), res.RunID)
	require.NoError(t, err)
	assert.JSONEq(t, `{"x":"original"}`, string(final.Result))
}

func TestCallWithoutReplyCompletesNoReply(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{"pass": funcFactory(passFn)})
	f := callGraph(map[string]string{"tool": "tool", "a": "pass", "b": "pass"}, [2]string{"tool", "a"}, [2]string{"a", "b"})
	rt := startRuntime(t, f, reg)

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Args: map[string]any{}, Timeout: 3 * time.Second})
	require.NoError(t, err)
	assert.Equal(t, CallStatusCompletedNoReply, res.Status)
	assert.Nil(t, res.Result)
	assert.Nil(t, res.Error)
	assert.True(t, isClosed(rt.RunDone(res.RunID)))
}

func TestCallCollectsNodeError(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"boom": newMockErrorNode(errors.New("boom: device unreachable")),
	})
	f := callGraph(map[string]string{"tool": "tool", "b": "boom"}, [2]string{"tool", "b"})
	rt := startRuntime(t, f, reg)

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Args: map[string]any{}, Timeout: 3 * time.Second})
	require.NoError(t, err)
	assert.Equal(t, CallStatusError, res.Status)
	require.NotNil(t, res.Error)
	assert.Equal(t, "b", res.Error.NodeID)
	assert.Equal(t, "boom", res.Error.NodeType)
	assert.Contains(t, res.Error.Message, "device unreachable")
}

func TestCallCollectsNodePanic(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{"panic": newMockPanicNode})
	f := callGraph(map[string]string{"tool": "tool", "p": "panic"}, [2]string{"tool", "p"})
	rt := startRuntime(t, f, reg)

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Args: map[string]any{}, Timeout: 3 * time.Second})
	require.NoError(t, err)
	assert.Equal(t, CallStatusError, res.Status)
	require.NotNil(t, res.Error)
	assert.Equal(t, "p", res.Error.NodeID)
	assert.Contains(t, res.Error.Message, "kaboom")
}

func TestCallReplyErrorMode(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"reply": funcFactory(replyFn(func(*Message) any { return "invalid sensor id" }, true)),
	})
	f := callGraph(map[string]string{"tool": "tool", "r": "reply"}, [2]string{"tool", "r"})
	rt := startRuntime(t, f, reg)

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Args: map[string]any{}, Timeout: 3 * time.Second})
	require.NoError(t, err)
	assert.Equal(t, CallStatusError, res.Status)
	assert.JSONEq(t, `"invalid sensor id"`, string(res.Result))
}

func TestCallFirstReplyWins(t *testing.T) {
	const branches = 8
	var wins atomic.Int32
	var winner atomic.Value
	procs := map[string]node.NodeFactory{}
	nodes := map[string]string{"tool": "tool"}
	var edges [][2]string
	for i := 0; i < branches; i++ {
		i := i
		typ := fmt.Sprintf("reply%d", i)
		procs[typ] = funcFactory(func(ctx context.Context, msg *Message) ([]*Message, error) {
			if ReplyFromContext(ctx).Reply(corrOf(msg), i, false) {
				wins.Add(1)
				winner.Store(i)
			}
			return nil, nil
		})
		nodes[typ] = typ
		edges = append(edges, [2]string{"tool", typ})
	}
	rt := startRuntime(t, callGraph(nodes, edges...), callRegistryFor(t, procs))

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Args: map[string]any{}, Timeout: 3 * time.Second})
	require.NoError(t, err)
	waitClosed(t, rt.RunDone(res.RunID), "run must finish")

	assert.Equal(t, int32(1), wins.Load(), "exactly one reply is accepted")
	assert.Equal(t, CallStatusCompleted, res.Status)
	assert.JSONEq(t, fmt.Sprint(winner.Load()), string(res.Result))
}

func TestCallReplyIsDeepCopied(t *testing.T) {
	ctxCh := make(chan context.Context, 1)
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"reply": funcFactory(func(ctx context.Context, msg *Message) ([]*Message, error) {
			payload := map[string]any{"list": []any{"a"}, "n": 1}
			ReplyFromContext(ctx).Reply(corrOf(msg), payload, false)
			payload["n"] = 2
			payload["list"].([]any)[0] = "mutated"
			ctxCh <- ctx
			return nil, nil
		}),
	})
	f := callGraph(map[string]string{"tool": "tool", "r": "reply"}, [2]string{"tool", "r"})
	rt := startRuntime(t, f, reg)

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Args: map[string]any{}, Timeout: 3 * time.Second})
	require.NoError(t, err)
	waitClosed(t, rt.RunDone(res.RunID), "run must finish")
	assert.JSONEq(t, `{"list":["a"],"n":1}`, string(res.Result))

	// A late reply for a finished run is refused.
	assert.False(t, ReplyFromContext(<-ctxCh).Reply(res.RunID, "late", false))
}

func TestReplyInRunNotStartedByCall(t *testing.T) {
	got := make(chan bool, 1)
	reg := testRegistry(t, 1, map[string]node.NodeFactory{
		"reply": funcFactory(func(ctx context.Context, msg *Message) ([]*Message, error) {
			got <- ReplyFromContext(ctx).Reply(corrOf(msg), "nobody is waiting", false)
			return nil, nil
		}),
	})
	f, _ := chainFlow("emit", "reply")
	startRuntime(t, f, reg)

	select {
	case ok := <-got:
		assert.False(t, ok, "reply is a no-op when the run was not started by Call")
	case <-time.After(3 * time.Second):
		t.Fatal("reply node did not run")
	}
}

func TestCallTimeoutReturnsRunningThenWaitCompletes(t *testing.T) {
	gate := make(chan struct{})
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"slow": funcFactory(gateFn(gate, replyFn(func(*Message) any { return "done" }, false))),
	})
	f := callGraph(map[string]string{"tool": "tool", "s": "slow"}, [2]string{"tool", "s"})
	rt := startRuntime(t, f, reg)

	start := time.Now()
	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Args: map[string]any{}, Timeout: 30 * time.Millisecond})
	require.NoError(t, err)
	assert.Equal(t, CallStatusRunning, res.Status)
	assert.NotEmpty(t, res.RunID)
	assert.Nil(t, res.Result)
	assert.Less(t, time.Since(start), 2*time.Second)
	assert.False(t, isClosed(rt.RunDone(res.RunID)), "the run continues after the timeout")

	type waitOut struct {
		res CallResult
		err error
	}
	waited := make(chan waitOut, 1)
	go func() {
		r, err := rt.Wait(context.Background(), res.RunID)
		waited <- waitOut{r, err}
	}()
	select {
	case <-waited:
		t.Fatal("Wait must block while the run is still running")
	case <-time.After(50 * time.Millisecond):
	}

	close(gate)
	select {
	case out := <-waited:
		require.NoError(t, out.err)
		assert.Equal(t, CallStatusCompleted, out.res.Status)
		assert.Equal(t, res.RunID, out.res.RunID)
		assert.JSONEq(t, `"done"`, string(out.res.Result))
	case <-time.After(3 * time.Second):
		t.Fatal("Wait did not return after the run completed")
	}
	waitClosed(t, rt.RunDone(res.RunID), "RunDone must close once the run finished")

	// A result Call could not deliver stays retrievable (until it expires).
	again, err := rt.Wait(context.Background(), res.RunID)
	require.NoError(t, err)
	assert.Equal(t, CallStatusCompleted, again.Status)
	assert.JSONEq(t, `"done"`, string(again.Result))
}

func TestWaitHonoursContext(t *testing.T) {
	gate := make(chan struct{})
	defer close(gate)
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"slow": funcFactory(gateFn(gate, replyFn(echoPayload, false))),
	})
	f := callGraph(map[string]string{"tool": "tool", "s": "slow"}, [2]string{"tool", "s"})
	rt := startRuntime(t, f, reg)

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Args: map[string]any{}, Timeout: 10 * time.Millisecond})
	require.NoError(t, err)
	require.Equal(t, CallStatusRunning, res.Status)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	got, err := rt.Wait(ctx, res.RunID)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, CallStatusRunning, got.Status)
	assert.Equal(t, res.RunID, got.RunID)
}

func TestCallContextCancellation(t *testing.T) {
	gate := make(chan struct{})
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"slow": funcFactory(gateFn(gate, replyFn(func(*Message) any { return "late but fine" }, false))),
	})
	f := callGraph(map[string]string{"tool": "tool", "s": "slow"}, [2]string{"tool", "s"})
	rt := startRuntime(t, f, reg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	res, err := rt.Call(ctx, CallRequest{TriggerNodeID: "tool", Args: map[string]any{}, Timeout: 5 * time.Second})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, CallStatusRunning, res.Status, "the run was started and keeps going")
	require.NotEmpty(t, res.RunID)

	close(gate)
	final, err := rt.Wait(context.Background(), res.RunID)
	require.NoError(t, err)
	assert.Equal(t, CallStatusCompleted, final.Status)
	assert.JSONEq(t, `"late but fine"`, string(final.Result))
}

func TestStopCancelsWaiters(t *testing.T) {
	enteredA, enteredB := make(chan struct{}), make(chan struct{})
	block := func(entered chan struct{}) node.NodeFactory {
		return funcFactory(func(ctx context.Context, msg *Message) ([]*Message, error) {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		})
	}
	reg := callRegistryFor(t, map[string]node.NodeFactory{"blockA": block(enteredA), "blockB": block(enteredB)})
	f := callGraph(map[string]string{"toolA": "tool", "toolB": "tool", "a": "blockA", "b": "blockB"},
		[2]string{"toolA", "a"}, [2]string{"toolB", "b"})
	rt, err := NewFlowRuntime(f, reg, WithLogger(zerolog.Nop()))
	require.NoError(t, err)
	require.NoError(t, rt.Start(context.Background()))

	// Call A times out and keeps running; call B is still blocked in Call.
	resA, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "toolA", Args: map[string]any{}, Timeout: 10 * time.Millisecond})
	require.NoError(t, err)
	require.Equal(t, CallStatusRunning, resA.Status)

	type callOut struct {
		res CallResult
		err error
	}
	outB := make(chan callOut, 1)
	go func() {
		r, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "toolB", Args: map[string]any{}, Timeout: 30 * time.Second})
		outB <- callOut{r, err}
	}()
	waitClosed(t, enteredA, "node a did not start")
	waitClosed(t, enteredB, "node b did not start")

	require.NoError(t, rt.Stop())

	select {
	case out := <-outB:
		require.NoError(t, out.err)
		assert.Equal(t, CallStatusError, out.res.Status)
		require.NotNil(t, out.res.Error)
		assert.Equal(t, "flow stopped", out.res.Error.Message)
		assert.True(t, isClosed(rt.RunDone(out.res.RunID)))
	case <-time.After(3 * time.Second):
		t.Fatal("Stop must release a blocked Call")
	}

	gotA, err := rt.Wait(context.Background(), resA.RunID)
	require.NoError(t, err)
	assert.Equal(t, CallStatusError, gotA.Status)
	require.NotNil(t, gotA.Error)
	assert.Equal(t, "flow stopped", gotA.Error.Message)
	assert.True(t, isClosed(rt.RunDone(resA.RunID)))

	_, err = rt.Call(context.Background(), CallRequest{TriggerNodeID: "toolA", Args: map[string]any{}, Timeout: time.Second})
	assert.ErrorIs(t, err, ErrFlowNotRunning)
}

func TestCallExpires(t *testing.T) {
	gate := make(chan struct{})
	defer close(gate)
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"slow": funcFactory(gateFn(gate, replyFn(echoPayload, false))),
	})
	f := callGraph(map[string]string{"tool": "tool", "s": "slow"}, [2]string{"tool", "s"})
	rt := startRuntime(t, f, reg, WithCallExpiryGrace(40*time.Millisecond))

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Args: map[string]any{}, Timeout: 10 * time.Millisecond})
	require.NoError(t, err)
	require.Equal(t, CallStatusRunning, res.Status)

	got, err := rt.Wait(context.Background(), res.RunID)
	require.NoError(t, err)
	assert.Equal(t, CallStatusExpired, got.Status, "a waiter past timeout+grace expires")
	assert.Equal(t, res.RunID, got.RunID)

	again, err := rt.Wait(context.Background(), res.RunID)
	require.NoError(t, err)
	assert.Equal(t, CallStatusExpired, again.Status, "an expired waiter is forgotten")
	assert.True(t, isClosed(rt.RunDone(res.RunID)))
}

func TestWaitUnknownRunIsExpired(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{"pass": funcFactory(passFn)})
	rt := startRuntime(t, callGraph(map[string]string{"tool": "tool", "p": "pass"}, [2]string{"tool", "p"}), reg)

	got, err := rt.Wait(context.Background(), "no-such-run")
	require.NoError(t, err)
	assert.Equal(t, CallStatusExpired, got.Status)
	assert.Equal(t, "no-such-run", got.RunID)
	assert.True(t, isClosed(rt.RunDone("no-such-run")))
}

func TestCallTriggerWithoutActiveTargets(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{"pass": funcFactory(passFn)})

	cases := map[string]*Flow{
		"unconnected": callGraph(map[string]string{"tool": "tool", "p": "pass"}),
		"only to a disabled node": func() *Flow {
			f := callGraph(map[string]string{"tool": "tool", "p": "pass"}, [2]string{"tool", "p"})
			f.Nodes[0].Enabled = false // "p" sorts before "tool"
			require.Equal(t, "p", f.Nodes[0].ID)
			return f
		}(),
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			rt := startRuntime(t, f, reg)
			assert.Equal(t, 0, rt.ActiveTargets("tool"))

			start := time.Now()
			_, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Args: map[string]any{}, Timeout: 5 * time.Second})
			assert.ErrorIs(t, err, ErrNoActiveTargets)
			assert.Less(t, time.Since(start), time.Second, "fails immediately, not after the timeout")
		})
	}
}

// TestCallOnUnconnectedPortFinishesAtOnce: a run whose first message has no
// target is over immediately; the call must not wait for its timeout.
func TestCallOnUnconnectedPortFinishesAtOnce(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{"pass": funcFactory(passFn)})
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "spare-tool", Name: "spare-tool", Category: NodeCategoryInput},
		func(config map[string]interface{}) (node.Node, error) {
			return &toolTrigger{BaseNode: node.NewBaseNode(config), spare: true}, nil
		}))
	f := callGraph(map[string]string{"tool": "spare-tool", "p": "pass"}, [2]string{"tool", "p"})
	rt := startRuntime(t, f, reg)
	require.Equal(t, 1, rt.ActiveTargets("tool"), "the tool is connected, just not on the port it emits on")

	start := time.Now()
	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Args: map[string]any{}, Timeout: 5 * time.Second})
	require.NoError(t, err)
	assert.Equal(t, CallStatusCompletedNoReply, res.Status)
	assert.Less(t, time.Since(start), 2*time.Second)
	assert.True(t, isClosed(rt.RunDone(res.RunID)))
}

func TestActiveTargetsCountsEdges(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{"pass": funcFactory(passFn)})
	f := callGraph(map[string]string{"tool": "tool", "a": "pass", "b": "pass", "c": "pass"},
		[2]string{"tool", "a"}, [2]string{"tool", "b"}, [2]string{"a", "c"})
	rt, err := NewFlowRuntime(f, reg, WithLogger(zerolog.Nop()))
	require.NoError(t, err)
	assert.Equal(t, 2, rt.ActiveTargets("tool"))
	assert.Equal(t, 1, rt.ActiveTargets("a"))
	assert.Equal(t, 0, rt.ActiveTargets("c"))
	assert.Equal(t, 0, rt.ActiveTargets("missing"))
}

func TestCallRejectsBadTriggers(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{"pass": funcFactory(passFn)})
	f := callGraph(map[string]string{"tool": "tool", "emit": "emit", "a": "pass", "b": "pass"},
		[2]string{"tool", "a"}, [2]string{"emit", "b"})

	stopped, err := NewFlowRuntime(f, reg, WithLogger(zerolog.Nop()))
	require.NoError(t, err)
	_, err = stopped.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Timeout: time.Second})
	assert.ErrorIs(t, err, ErrFlowNotRunning)

	rt := startRuntime(t, f, reg)
	_, err = rt.Call(context.Background(), CallRequest{TriggerNodeID: "emit", Timeout: time.Second})
	assert.ErrorIs(t, err, ErrTriggerNotCallable)
	_, err = rt.Call(context.Background(), CallRequest{TriggerNodeID: "a", Timeout: time.Second})
	assert.ErrorIs(t, err, ErrNodeNotFound, "a processing node is not a trigger")
	_, err = rt.Call(context.Background(), CallRequest{TriggerNodeID: "missing", Timeout: time.Second})
	assert.ErrorIs(t, err, ErrNodeNotFound)
}

func TestCallWithNilArgsSendsEmptyObject(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{"reply": funcFactory(replyFn(echoPayload, false))})
	rt := startRuntime(t, callGraph(map[string]string{"tool": "tool", "r": "reply"}, [2]string{"tool", "r"}), reg)

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Timeout: 3 * time.Second})
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(res.Result))
}

func TestCallDefaultTimeout(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{"reply": funcFactory(replyFn(echoPayload, false))})
	rt := startRuntime(t, callGraph(map[string]string{"tool": "tool", "r": "reply"}, [2]string{"tool", "r"}), reg)

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Args: map[string]any{"a": "b"}})
	require.NoError(t, err)
	assert.Equal(t, CallStatusCompleted, res.Status, "a zero timeout uses the default, not an instant timeout")
}

// TestConcurrentCallsAreIsolated: many simultaneous calls on the same tool
// each get their own reply, never another run's.
func TestConcurrentCallsAreIsolated(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"reply": funcFactory(replyFn(echoPayload, false)),
		// A sibling branch that scribbles on its (cloned) copy of the payload.
		"mutate": funcFactory(func(_ context.Context, msg *Message) ([]*Message, error) {
			if m, ok := msg.Payload.(map[string]any); ok {
				m["i"] = -1
				m["nested"] = "clobbered"
			}
			return nil, nil
		}),
	})
	f := callGraph(map[string]string{"tool": "tool", "r": "reply", "m": "mutate"}, [2]string{"tool", "m"}, [2]string{"tool", "r"})
	rt := startRuntime(t, f, reg)

	const calls = 64
	var wg sync.WaitGroup
	results := make([]CallResult, calls)
	errs := make([]error, calls)
	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = rt.Call(context.Background(), CallRequest{
				TriggerNodeID: "tool",
				Args:          map[string]any{"i": i, "nested": map[string]any{"k": fmt.Sprint(i)}},
				Actor:         fmt.Sprintf("token:%d", i),
				Timeout:       10 * time.Second,
			})
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i := 0; i < calls; i++ {
		require.NoError(t, errs[i])
		require.Equal(t, CallStatusCompleted, results[i].Status, "call %d", i)
		assert.JSONEq(t, fmt.Sprintf(`{"i":%d,"nested":{"k":"%d"}}`, i, i), string(results[i].Result), "call %d", i)
		assert.False(t, seen[results[i].RunID], "run IDs are unique")
		seen[results[i].RunID] = true
	}
}

// TestCalledRunFinishesWithNonReplyBranches: a reply on one branch returns the
// call at once; RunDone waits for the other branches.
func TestCalledRunFinishesWithNonReplyBranches(t *testing.T) {
	gate := make(chan struct{})
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"reply": funcFactory(replyFn(func(*Message) any { return "fast" }, false)),
		"slow":  funcFactory(gateFn(gate, passFn)),
		"pass":  funcFactory(passFn),
	})
	f := callGraph(map[string]string{"tool": "tool", "r": "reply", "s": "slow", "p": "pass"},
		[2]string{"tool", "r"}, [2]string{"tool", "s"}, [2]string{"s", "p"})
	hook, read := collectAll()
	rt := startRuntime(t, f, reg, WithObserver(hook))

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Args: map[string]any{}, Timeout: 5 * time.Second})
	require.NoError(t, err)
	assert.Equal(t, CallStatusCompleted, res.Status)
	assert.JSONEq(t, `"fast"`, string(res.Result))

	runDone := rt.RunDone(res.RunID)
	time.Sleep(30 * time.Millisecond)
	assert.False(t, isClosed(runDone), "the slow branch is still running")

	close(gate)
	waitClosed(t, runDone, "the run finishes once every branch drained")
	require.Eventually(t, func() bool { return countPhase(read(), NodePhaseRunFinished) == 1 }, 2*time.Second, 5*time.Millisecond)
	for _, e := range read() {
		assert.Equal(t, res.RunID, e.CorrID, "every event belongs to the called run")
	}
}

// TestCallErrorOnOneBranchReplyOnAnother: a reply wins over a node error from
// a sibling branch.
func TestCallErrorOnOneBranchReplyOnAnother(t *testing.T) {
	failed := make(chan struct{})
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"boom":  newMockErrorNode(errors.New("boom")),
		"reply": funcFactory(gateFn(failed, replyFn(func(*Message) any { return "ok" }, false))),
	})
	f := callGraph(map[string]string{"tool": "tool", "b": "boom", "r": "reply"}, [2]string{"tool", "b"}, [2]string{"tool", "r"})
	var once sync.Once
	hook := func(e NodeEvent) {
		if e.Phase == NodePhaseError {
			once.Do(func() { close(failed) })
		}
	}
	rt := startRuntime(t, f, reg, WithObserver(hook))

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Args: map[string]any{}, Timeout: 5 * time.Second})
	require.NoError(t, err)
	assert.Equal(t, CallStatusCompleted, res.Status)
	assert.Nil(t, res.Error)
	assert.JSONEq(t, `"ok"`, string(res.Result))
}

func TestDeployRejectsCallableTriggerWithoutTargets(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{"pass": funcFactory(passFn)})
	m := NewRuntimeManager(reg, zerolog.Nop())
	defer m.StopAll()

	lone := callGraph(map[string]string{"tool": "tool", "p": "pass"})
	err := m.Deploy(context.Background(), lone)
	require.Error(t, err)
	var verr *ValidationError
	assert.True(t, errors.As(err, &verr), "a validation error (400), got %T: %v", err, err)
	assert.Contains(t, err.Error(), "tool")
	_, exists := m.GetRuntime(lone.ID)
	assert.False(t, exists)

	// A plain trigger without edges stays deployable (existing behaviour).
	plain := callGraph(map[string]string{"emit": "emit"})
	require.NoError(t, m.Deploy(context.Background(), plain))

	connected := callGraph(map[string]string{"tool": "tool", "p": "pass"}, [2]string{"tool", "p"})
	require.NoError(t, m.Deploy(context.Background(), connected))
}

// TestCompletedCallsReleaseTheirWaiters: a call whose result was returned by
// Call leaves nothing in the registry once its run is over, however many calls
// ran, sequentially or concurrently, and whatever their outcome.
func TestCompletedCallsReleaseTheirWaiters(t *testing.T) {
	big := strings.Repeat("x", 16<<10)
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"reply": funcFactory(func(ctx context.Context, msg *Message) ([]*Message, error) {
			args, _ := msg.Payload.(map[string]any)
			switch args["mode"] {
			case "reply":
				ReplyFromContext(ctx).Reply(corrOf(msg), big, false)
			case "fail":
				return nil, errors.New("boom")
			}
			return nil, nil // "none": completed_no_reply
		}),
	})
	rt := startRuntime(t, callGraph(map[string]string{"tool": "tool", "r": "reply"}, [2]string{"tool", "r"}), reg)

	modes := []string{"reply", "fail", "none"}
	want := map[string]CallStatus{"reply": CallStatusCompleted, "fail": CallStatusError, "none": CallStatusCompletedNoReply}
	call := func(i int) error {
		mode := modes[i%len(modes)]
		res, err := rt.Call(context.Background(), CallRequest{
			TriggerNodeID: "tool", Args: map[string]any{"mode": mode}, Timeout: 10 * time.Second,
		})
		if err != nil {
			return err
		}
		if res.Status != want[mode] {
			return fmt.Errorf("call %d (%s): status %s", i, mode, res.Status)
		}
		return nil
	}

	const sequential = 300
	for i := 0; i < sequential; i++ {
		require.NoError(t, call(i))
	}

	const workers, perWorker = 8, 40
	var wg sync.WaitGroup
	errs := make(chan error, workers*perWorker)
	for g := 0; g < workers; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				if err := call(g*perWorker + i); err != nil {
					errs <- err
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.Eventually(t, func() bool { return rt.calls.count() == 0 }, 3*time.Second, 5*time.Millisecond,
		"every delivered result is freed once its run ended (left: %d)", rt.calls.count())
}

func TestCallResultJSON(t *testing.T) {
	data, err := json.Marshal(CallResult{
		Status: CallStatusError, RunID: "r1", DurationMs: 3,
		Error: &CallError{NodeID: "n1", NodeType: "action-http", Message: "boom"},
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"status":"error","runId":"r1","durationMs":3,
		"error":{"nodeId":"n1","nodeType":"action-http","message":"boom"}}`, string(data))
}
