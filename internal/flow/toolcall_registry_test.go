package flow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Deterministic unit tests of the waiter registry behind FlowRuntime.Call. The
// runtime wiring is covered by toolcall_test.go.

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func newTestRegistry(t *testing.T) *callRegistry {
	t.Helper()
	reg := newCallRegistry(time.Hour)
	reg.start()
	return reg
}

func TestCallRegistryReplyCompletes(t *testing.T) {
	reg := newTestRegistry(t)
	w, err := reg.register("run-1", time.Second)
	require.NoError(t, err)

	assert.True(t, reg.Reply("run-1", map[string]any{"ok": true}, false))
	require.True(t, isClosed(w.done), "a reply makes the result final")
	assert.False(t, isClosed(w.runDone), "the run itself has not finished yet")

	res := reg.snapshot(w)
	assert.Equal(t, CallStatusCompleted, res.Status)
	assert.Equal(t, "run-1", res.RunID)
	assert.JSONEq(t, `{"ok":true}`, string(res.Result))
	assert.Nil(t, res.Error)

	reg.runFinished("run-1")
	assert.True(t, isClosed(w.runDone))
	assert.Equal(t, CallStatusCompleted, reg.snapshot(w).Status, "run end keeps the reply")
}

func TestCallRegistryFirstReplyWins(t *testing.T) {
	reg := newTestRegistry(t)
	w, err := reg.register("run-1", time.Second)
	require.NoError(t, err)

	assert.True(t, reg.Reply("run-1", "first", false))
	assert.False(t, reg.Reply("run-1", "second", false))
	assert.False(t, reg.Reply("run-1", "third", true))
	assert.JSONEq(t, `"first"`, string(reg.snapshot(w).Result))
	assert.Equal(t, CallStatusCompleted, reg.snapshot(w).Status)
}

func TestCallRegistryReplyErrorMode(t *testing.T) {
	reg := newTestRegistry(t)
	w, err := reg.register("run-1", time.Second)
	require.NoError(t, err)

	assert.True(t, reg.Reply("run-1", "bad input", true))
	res := reg.snapshot(w)
	assert.Equal(t, CallStatusError, res.Status)
	assert.JSONEq(t, `"bad input"`, string(res.Result))
}

func TestCallRegistryReplyUnknownOrFinished(t *testing.T) {
	reg := newTestRegistry(t)
	assert.False(t, reg.Reply("nope", 1, false), "unknown correlation ID")

	_, err := reg.register("run-1", time.Second)
	require.NoError(t, err)
	reg.runFinished("run-1")
	assert.False(t, reg.Reply("run-1", 1, false), "the run already finished without a reply")
}

func TestCallRegistryReplyDeepCopies(t *testing.T) {
	reg := newTestRegistry(t)
	w, err := reg.register("run-1", time.Second)
	require.NoError(t, err)

	payload := map[string]any{"list": []any{"a"}, "n": 1}
	require.True(t, reg.Reply("run-1", payload, false))
	payload["n"] = 2
	payload["list"].([]any)[0] = "mutated"

	first := reg.snapshot(w)
	assert.JSONEq(t, `{"list":["a"],"n":1}`, string(first.Result))

	// Every snapshot owns its bytes: a caller scribbling on one cannot change
	// what the next caller sees.
	first.Result[0] = 'X'
	assert.JSONEq(t, `{"list":["a"],"n":1}`, string(reg.snapshot(w).Result))
}

func TestCallRegistryUnserializableReplyIsAnError(t *testing.T) {
	reg := newTestRegistry(t)
	w, err := reg.register("run-1", time.Second)
	require.NoError(t, err)

	assert.False(t, reg.Reply("run-1", make(chan int), false))
	assert.False(t, isClosed(w.done), "a failed reply is not a result")

	reg.runFinished("run-1")
	res := reg.snapshot(w)
	assert.Equal(t, CallStatusError, res.Status)
	require.NotNil(t, res.Error)
	assert.Contains(t, res.Error.Message, "reply")
}

func TestCallRegistryRunEndWithoutReply(t *testing.T) {
	reg := newTestRegistry(t)
	w, err := reg.register("run-1", time.Second)
	require.NoError(t, err)

	reg.runFinished("run-1")
	require.True(t, isClosed(w.done))
	require.True(t, isClosed(w.runDone))
	res := reg.snapshot(w)
	assert.Equal(t, CallStatusCompletedNoReply, res.Status)
	assert.Nil(t, res.Result)
	assert.Nil(t, res.Error)

	reg.runFinished("run-1") // idempotent: channels are closed only once
}

func TestCallRegistryFirstNodeErrorIsCollected(t *testing.T) {
	reg := newTestRegistry(t)
	w, err := reg.register("run-1", time.Second)
	require.NoError(t, err)

	reg.nodeError("run-1", "n1", "type-a", errors.New("first"))
	reg.nodeError("run-1", "n2", "type-b", errors.New("second"))
	reg.nodeError("other", "n3", "type-c", errors.New("not this run"))
	assert.False(t, isClosed(w.done), "a node error does not end the call")

	reg.runFinished("run-1")
	res := reg.snapshot(w)
	assert.Equal(t, CallStatusError, res.Status)
	assert.Equal(t, &CallError{NodeID: "n1", NodeType: "type-a", Message: "first"}, res.Error)
}

func TestCallRegistryReplyWinsOverNodeError(t *testing.T) {
	reg := newTestRegistry(t)
	w, err := reg.register("run-1", time.Second)
	require.NoError(t, err)

	reg.nodeError("run-1", "n1", "type-a", errors.New("branch failed"))
	require.True(t, reg.Reply("run-1", "ok", false))
	reg.runFinished("run-1")

	res := reg.snapshot(w)
	assert.Equal(t, CallStatusCompleted, res.Status)
	assert.Nil(t, res.Error)
	assert.JSONEq(t, `"ok"`, string(res.Result))
}

func TestCallRegistryPendingSnapshotIsRunning(t *testing.T) {
	reg := newTestRegistry(t)
	w, err := reg.register("run-1", time.Second)
	require.NoError(t, err)

	res := reg.snapshot(w)
	assert.Equal(t, CallStatusRunning, res.Status)
	assert.Equal(t, "run-1", res.RunID)
	assert.Nil(t, res.Result)
}

func TestCallRegistryStopCancelsWaiters(t *testing.T) {
	reg := newTestRegistry(t)
	pending, err := reg.register("pending", time.Second)
	require.NoError(t, err)
	done, err := reg.register("done", time.Second)
	require.NoError(t, err)
	require.True(t, reg.Reply("done", "kept", false))

	reg.stop()

	require.True(t, isClosed(pending.done))
	require.True(t, isClosed(pending.runDone))
	res := reg.snapshot(pending)
	assert.Equal(t, CallStatusError, res.Status)
	require.NotNil(t, res.Error)
	assert.Equal(t, "flow stopped", res.Error.Message)

	assert.Equal(t, CallStatusCompleted, reg.snapshot(done).Status, "a finished call keeps its result")
	assert.True(t, isClosed(done.runDone))

	_, err = reg.register("late", time.Second)
	assert.ErrorIs(t, err, ErrFlowNotRunning, "no new calls on a stopped flow")

	reg.start()
	_, err = reg.register("after-restart", time.Second)
	assert.NoError(t, err)
}

func TestCallRegistryExpiry(t *testing.T) {
	reg := newCallRegistry(30 * time.Millisecond)
	reg.start()
	w, err := reg.register("run-1", 10*time.Millisecond)
	require.NoError(t, err)

	select {
	case <-w.done:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not expire")
	}
	assert.Equal(t, CallStatusExpired, reg.snapshot(w).Status)
	assert.True(t, isClosed(w.runDone), "an expired waiter no longer holds the run open")
	assert.Nil(t, reg.get("run-1"), "an expired waiter is forgotten")
	assert.False(t, reg.Reply("run-1", 1, false))
	assert.True(t, isClosed(reg.runDoneChan("run-1")), "an unknown run reports done")
}

func TestCallRegistryFinishedWaiterIsForgottenAtExpiry(t *testing.T) {
	reg := newCallRegistry(20 * time.Millisecond)
	reg.start()
	w, err := reg.register("run-1", 10*time.Millisecond)
	require.NoError(t, err)
	require.True(t, reg.Reply("run-1", "ok", false))
	reg.runFinished("run-1")

	require.Eventually(t, func() bool { return reg.get("run-1") == nil }, 2*time.Second, 5*time.Millisecond)
	assert.Equal(t, CallStatusCompleted, reg.snapshot(w).Status, "holders of the waiter keep the final result")
}

func TestCallRegistryRemove(t *testing.T) {
	reg := newTestRegistry(t)
	w, err := reg.register("run-1", time.Second)
	require.NoError(t, err)
	reg.remove("run-1")
	assert.Nil(t, reg.get("run-1"))
	assert.True(t, isClosed(w.runDone))
	assert.False(t, reg.Reply("run-1", 1, false))
}

func TestCallRegistryDuplicateRunID(t *testing.T) {
	reg := newTestRegistry(t)
	_, err := reg.register("run-1", time.Second)
	require.NoError(t, err)
	_, err = reg.register("run-1", time.Second)
	assert.Error(t, err)
}

func TestReplyFromContextWithoutRegistry(t *testing.T) {
	rep := ReplyFromContext(context.Background())
	require.NotNil(t, rep, "never nil, so action-reply can call it unconditionally")
	assert.False(t, rep.Reply("anything", 1, false))
}

func TestReplyFromContextFindsRegistry(t *testing.T) {
	reg := newTestRegistry(t)
	w, err := reg.register("run-1", time.Second)
	require.NoError(t, err)
	ctx := withCallRegistry(context.Background(), reg)

	assert.True(t, ReplyFromContext(ctx).Reply("run-1", 42, false))
	assert.JSONEq(t, `42`, string(reg.snapshot(w).Result))
}
