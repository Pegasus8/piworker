package flow

import (
	"context"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Node events carry the run's actor as a plain string, read by the executor
// from the input message before the node runs, so observers (the history
// recorder runs on its own goroutine) never touch the message's meta while
// the node may still change it.
func TestNodeEventsCarryTheRunActor(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{
		"mutate": funcFactory(func(_ context.Context, msg *Message) ([]*Message, error) {
			msg.SetMeta("actor", "changed by the node")
			return passFn(nil, msg)
		}),
		"pass": funcFactory(passFn),
	})
	f := callGraph(map[string]string{"tool": "tool", "a": "mutate", "b": "pass"}, [2]string{"tool", "a"}, [2]string{"a", "b"})
	hook, read := collectObserver()
	rt := startRuntime(t, f, reg, WithObserver(hook))

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Actor: "token:bot", Timeout: 3 * time.Second})
	require.NoError(t, err)
	require.Equal(t, CallStatusCompletedNoReply, res.Status)

	var sawA bool
	for _, e := range read() {
		if e.NodeID == "a" {
			sawA = true
			assert.Equal(t, "token:bot", e.Actor, "phase %s: the actor is captured before the node runs", e.Phase)
		}
	}
	assert.True(t, sawA)
}

func TestActorOfIgnoresNonStringMeta(t *testing.T) {
	msg := NewMessage("x", DataTypeString)
	assert.Equal(t, "", actorOf(msg))
	assert.Equal(t, "", actorOf(nil))
	msg.SetMeta("actor", 42)
	assert.Equal(t, "", actorOf(msg))
	msg.SetMeta("actor", "editor")
	assert.Equal(t, "editor", actorOf(msg))
}

// Node events carry the version of the definition the runtime runs, as it
// was when the runtime was built: the history records it with each run, so
// what a run ran is known whatever happens to the flow or its deployment
// marker afterwards (ADR-0022).
func TestNodeEventsCarryTheRuntimesFlowVersion(t *testing.T) {
	reg := callRegistryFor(t, map[string]node.NodeFactory{"pass": funcFactory(passFn)})
	f := callGraph(map[string]string{"tool": "tool", "a": "pass"}, [2]string{"tool", "a"})
	f.Version = 7
	hook, read := collectObserver()
	rt := startRuntime(t, f, reg, WithObserver(hook))
	f.Version = 8 // the caller's copy moves on (a later save); the runtime does not

	res, err := rt.Call(context.Background(), CallRequest{TriggerNodeID: "tool", Timeout: 3 * time.Second})
	require.NoError(t, err)
	require.Equal(t, CallStatusCompletedNoReply, res.Status)

	events := read()
	require.NotEmpty(t, events)
	for _, e := range events {
		assert.Equal(t, 7, e.FlowVersion, "%s %s", e.NodeID, e.Phase)
	}
}
