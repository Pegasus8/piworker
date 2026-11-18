package flow

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The error port (ADR-0036): a node's failure is a message on its "error"
// output when that port is connected, so the flow can handle it.

// catcher records the messages a handler node receives.
type catcher struct {
	mu   sync.Mutex
	msgs []*Message
}

func (c *catcher) fn(_ context.Context, msg *Message) ([]*Message, error) {
	c.mu.Lock()
	c.msgs = append(c.msgs, msg)
	c.mu.Unlock()
	return nil, nil
}

func (c *catcher) got() []*Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*Message(nil), c.msgs...)
}

func failFn(err error) func(context.Context, *Message) ([]*Message, error) {
	return func(context.Context, *Message) ([]*Message, error) { return nil, err }
}

// errorFlow is emit -> "bad" -> (output) "next", with bad's error port
// connected to "handler".
func errorFlow() (*Flow, []string) {
	f, ids := chainFlow("emit", "bad", "next")
	h := NewNode("handler", NodeCategoryProcessing)
	h.Inputs = []Port{{ID: "input", DataType: DataTypeAny}}
	h.Outputs = []Port{{ID: "output", DataType: DataTypeAny}}
	f.AddNode(*h)
	f.AddConnection(*NewConnection(ids[1], ErrorPort, h.ID, "input"))
	return f, append(ids, h.ID)
}

func TestErrorPortRoutesTheFailureToItsHandler(t *testing.T) {
	handled, next := &catcher{}, &catcher{}
	reg := testRegistry(t, 1, map[string]node.NodeFactory{
		"bad":     funcFactory(failFn(errors.New("sensor unplugged"))),
		"next":    funcFactory(next.fn),
		"handler": funcFactory(handled.fn),
	})
	f, ids := errorFlow()
	hook, read := collectAll()
	events := deployAndWait(t, f, reg, read, hook, 1)

	require.Len(t, handled.got(), 1, "the handler runs once")
	assert.Empty(t, next.got(), "nothing leaves the normal output")
	msg := handled.got()[0]
	payload, ok := msg.Payload.(map[string]interface{})
	require.True(t, ok, "%T", msg.Payload)
	assert.Equal(t, "sensor unplugged", payload["message"])
	assert.Equal(t, ids[1], payload["nodeId"])
	assert.Equal(t, "bad", payload["nodeType"])
	assert.Equal(t, 0, payload["input"], "the payload the node failed on")
	assert.Equal(t, "error", msg.Topic)
	assert.Equal(t, DataTypeObject, msg.PayloadType)

	// One run: the failure is still recorded as the node's error, the
	// handler belongs to the same run, and the run ends after it.
	var badErr, handlerOK int
	corr := map[string]bool{}
	for _, e := range events {
		if e.CorrID != "" {
			corr[e.CorrID] = true
		}
		if e.NodeID == ids[1] && e.Phase == NodePhaseError {
			badErr++
		}
		if e.NodeID == ids[3] && e.Phase == NodePhaseSuccess {
			handlerOK++
		}
	}
	assert.Equal(t, 1, badErr)
	assert.Equal(t, 1, handlerOK)
	assert.Len(t, corr, 1, "one correlation ID")
	assert.Equal(t, 1, countPhase(events, NodePhaseRunFinished))
}

func TestErrorPortCatchesAPanic(t *testing.T) {
	handled := &catcher{}
	reg := testRegistry(t, 1, map[string]node.NodeFactory{
		"bad":     funcFactory(func(context.Context, *Message) ([]*Message, error) { panic("kaboom") }),
		"next":    funcFactory(passFn),
		"handler": funcFactory(handled.fn),
	})
	f, _ := errorFlow()
	hook, read := collectAll()
	events := deployAndWait(t, f, reg, read, hook, 1)

	require.Len(t, handled.got(), 1)
	payload := handled.got()[0].Payload.(map[string]interface{})
	assert.Equal(t, "panic: kaboom", payload["message"])
	assert.Equal(t, 1, countPhase(events, NodePhaseRunFinished), "the run still ends exactly once")
}

// The error message is the handler's own copy: changing it does not change
// what the failed node received.
func TestErrorPortMessageDoesNotAliasTheInput(t *testing.T) {
	handled := &catcher{}
	var seen *Message
	reg := testRegistry(t, 1, map[string]node.NodeFactory{
		"bad": funcFactory(func(_ context.Context, msg *Message) ([]*Message, error) {
			seen = msg
			msg.SetMeta("trace", "abc")
			return nil, errors.New("no")
		}),
		"next":    funcFactory(passFn),
		"handler": funcFactory(handled.fn),
	})
	f, _ := errorFlow()
	hook, read := collectAll()
	deployAndWait(t, f, reg, read, hook, 1)
	require.Len(t, handled.got(), 1)
	got := handled.got()[0]
	assert.NotSame(t, seen, got)
	v, _ := got.GetMeta("trace")
	assert.Equal(t, "abc", v, "the input's metadata travels with the failure")
}

// Without a connection on the error port nothing changes: the node fails,
// nothing is routed.
func TestUnconnectedErrorPortRoutesNothing(t *testing.T) {
	next := &catcher{}
	reg := testRegistry(t, 1, map[string]node.NodeFactory{
		"bad":  funcFactory(failFn(errors.New("no"))),
		"next": funcFactory(next.fn),
	})
	f, _ := chainFlow("emit", "bad", "next")
	hook, read := collectAll()
	events := deployAndWait(t, f, reg, read, hook, 1)
	assert.Empty(t, next.got())
	assert.Equal(t, 1, countPhase(events, NodePhaseError))
}

// A called run whose failure is handled is not ended by the failure: the
// handler decides the reply. Unhandled, the call ends with the node error.
func TestCallWithHandledErrorIsAnsweredByTheHandler(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"bad":     funcFactory(failFn(errors.New("upstream down"))),
		"handler": funcFactory(replyFn(func(msg *Message) any { return msg.Payload.(map[string]interface{})["message"] }, true)),
	})
	f := callGraph(map[string]string{"t": "tool", "bad": "bad", "handler": "handler"}, [2]string{"t", "bad"})
	f.AddConnection(*NewConnection("bad", ErrorPort, "handler", "input"))
	rt := startRuntime(t, f, reg)
	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "t", Args: map[string]any{}, Timeout: 2 * time.Second})
	require.NoError(t, err)
	assert.Equal(t, CallStatusError, res.Status)
	assert.Nil(t, res.Error, "the handler's reply, not the raw node error")
	assert.JSONEq(t, `"upstream down"`, string(res.Result))

	unhandled := callGraph(map[string]string{"t": "tool", "bad": "bad"}, [2]string{"t", "bad"})
	rt2 := startRuntime(t, unhandled, reg)
	res, err = rt2.Call(context.Background(), CallRequest{TriggerNodeID: "t", Args: map[string]any{}, Timeout: 2 * time.Second})
	require.NoError(t, err)
	assert.Equal(t, CallStatusError, res.Status)
	require.NotNil(t, res.Error)
	assert.Equal(t, "bad", res.Error.NodeID)
}

// The error port exists on every node that processes messages and on no
// trigger; deploy and Lint agree.
func TestErrorPortConnectionsValidate(t *testing.T) {
	reg := testRegistry(t, 0, map[string]node.NodeFactory{"pass": passNode(), "handler": passNode()})
	f, ids := chainFlow("emit", "pass")
	h := NewNode("handler", NodeCategoryProcessing)
	f.AddNode(*h)
	f.AddConnection(*NewConnection(ids[1], ErrorPort, h.ID, "input"))
	for _, is := range Lint(f, reg) {
		assert.NotEqual(t, IssueError, is.Severity, "%+v", is)
	}
	rt, err := NewFlowRuntime(f, reg)
	require.NoError(t, err)
	_, _, err = validateDeployable(rt, reg)
	require.NoError(t, err)

	// A trigger never processes a message, so it has no failure to route.
	bad, ids := chainFlow("emit", "pass")
	h2 := NewNode("handler", NodeCategoryProcessing)
	bad.AddNode(*h2)
	bad.AddConnection(*NewConnection(ids[0], ErrorPort, h2.ID, "input"))
	var codes []string
	for _, is := range Lint(bad, reg) {
		if is.Severity == IssueError {
			codes = append(codes, is.Code)
		}
	}
	assert.Contains(t, codes, IssueConnectionUnknownPort)
	rt, err = NewFlowRuntime(bad, reg)
	require.NoError(t, err)
	_, _, err = validateDeployable(rt, reg)
	require.Error(t, err)
}
