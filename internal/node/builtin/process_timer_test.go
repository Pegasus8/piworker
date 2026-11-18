package builtin

import (
	"context"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTimer(t *testing.T, cfg map[string]interface{}) (*TimerNode, chan *types.Message) {
	t.Helper()
	n, err := NewTimerNode(cfg)
	require.NoError(t, err)
	tm := n.(*TimerNode)
	tm.unit = time.Millisecond // tests count in milliseconds
	out := make(chan *types.Message, 4)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, tm.Start(ctx, out))
	t.Cleanup(func() { _ = tm.Stop() })
	return tm, out
}

func timerSend(t *testing.T, tm *TimerNode, payload interface{}) {
	t.Helper()
	out, err := tm.Process(context.Background(), types.NewMessage(payload, types.DataTypeAny))
	require.NoError(t, err)
	assert.Empty(t, out, "the timer emits later, as a new run")
}

// Each message restarts the timer; it fires once, with the last message,
// when no message came for the whole duration.
func TestTimerRestartsAndFiresWithTheLastMessage(t *testing.T) {
	tm, out := newTimer(t, map[string]interface{}{"duration": float64(150)})
	timerSend(t, tm, "first")
	time.Sleep(80 * time.Millisecond)
	timerSend(t, tm, "second")
	time.Sleep(80 * time.Millisecond) // 160 ms after the first, 80 after the second
	select {
	case m := <-out:
		t.Fatalf("fired early with %v", m.Payload)
	default:
	}
	m := nextMsg(t, out)
	assert.Equal(t, "second", m.Payload)
	assert.Equal(t, "output", m.SourcePort)
	noMsg(t, out)
}

// Without restart, a running timer is not extended.
func TestTimerWithoutRestart(t *testing.T) {
	tm, out := newTimer(t, map[string]interface{}{"duration": float64(150), "restart": false})
	start := time.Now()
	timerSend(t, tm, "first")
	time.Sleep(80 * time.Millisecond)
	timerSend(t, tm, "second")
	m := nextMsg(t, out)
	assert.Equal(t, "first", m.Payload)
	assert.Less(t, time.Since(start), 230*time.Millisecond)
}

// "stop" (or {"stop": true}) cancels a running timer.
func TestTimerStop(t *testing.T) {
	tm, out := newTimer(t, map[string]interface{}{"duration": float64(100)})
	timerSend(t, tm, "go")
	timerSend(t, tm, "stop")
	noMsg(t, out)
	timerSend(t, tm, "go")
	timerSend(t, tm, map[string]interface{}{"stop": true})
	noMsg(t, out)
}

// Stopping the flow cancels the timer; a message before Start waits for it.
func TestTimerLifecycle(t *testing.T) {
	n, err := NewTimerNode(map[string]interface{}{"duration": float64(50)})
	require.NoError(t, err)
	tm := n.(*TimerNode)
	tm.unit = time.Millisecond
	timerSend(t, tm, "early")
	out := make(chan *types.Message, 2)
	require.NoError(t, tm.Start(context.Background(), out))
	assert.Equal(t, "early", nextMsg(t, out).Payload)

	timerSend(t, tm, "late")
	require.NoError(t, tm.Stop())
	noMsg(t, out)
}

func TestTimerConfig(t *testing.T) {
	for _, cfg := range []map[string]interface{}{
		{"duration": float64(0)},
		{"duration": float64(25), "durationUnit": "hours"},
		{"duration": float64(5), "durationUnit": "days"},
	} {
		_, err := NewTimerNode(cfg)
		assert.Error(t, err, "%v", cfg)
	}
	n, err := NewTimerNode(map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, n.(*TimerNode).duration())
	_, isTrigger := n.(node.TriggerNode)
	assert.True(t, isTrigger, "emits on its own, as a new run")
}
