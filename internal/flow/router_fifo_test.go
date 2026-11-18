package flow

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// TestRouterPreservesFIFOPerSourceTarget routes thousands of messages from one
// source to two targets with sequential Route calls and asserts each target
// sees them in exactly the order they were routed.
func TestRouterPreservesFIFOPerSourceTarget(t *testing.T) {
	const n = 5000
	router := NewMessageRouter([]Connection{
		{ID: "c1", SourceNode: "src", SourcePort: "output", TargetNode: "a", TargetPort: "input"},
		{ID: "c2", SourceNode: "src", SourcePort: "output", TargetNode: "b", TargetPort: "input"},
	}, zerolog.Nop())
	defer router.Close()

	next := map[string]int{"a": 0, "b": 0}
	reordered := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		for got := 0; got < 2*n; got++ {
			select {
			case rm := <-router.Output():
				idx := rm.Message.Payload.(int)
				if idx != next[rm.TargetNode] {
					reordered++
				}
				next[rm.TargetNode] = idx + 1
			case <-time.After(5 * time.Second):
				t.Errorf("timeout after %d messages", got)
				return
			}
		}
	}()

	for i := 0; i < n; i++ {
		msg := NewMessage(i, DataTypeNumber)
		msg.SourcePort = "output"
		router.Route("src", msg)
	}
	<-done
	require.Zero(t, reordered, "messages between the same source and target must stay in FIFO order")
	require.Equal(t, n, next["a"])
	require.Equal(t, n, next["b"])
}
