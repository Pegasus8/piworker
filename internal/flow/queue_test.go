package flow

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeQueueFIFOAndLimit(t *testing.T) {
	q := newNodeQueue(3)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		require.True(t, q.push(RoutedMessage{TargetPort: string(rune('a' + i))}))
	}
	assert.False(t, q.push(RoutedMessage{TargetPort: "x"}), "a full queue refuses instead of blocking")
	assert.Equal(t, 3, q.len())

	for i := 0; i < 3; i++ {
		m, ok := q.pop(ctx)
		require.True(t, ok)
		assert.Equal(t, string(rune('a'+i)), m.TargetPort)
	}
	assert.Equal(t, 0, q.len())
}

func TestNodeQueueLongRunStaysFIFO(t *testing.T) {
	q := newNodeQueue(4)
	ctx := context.Background()
	next, want := 0, 0
	for round := 0; round < 1000; round++ {
		// keep a standing backlog of 2-3 so head advances past the limit and compacts
		for q.len() < 3 {
			require.True(t, q.push(RoutedMessage{Message: NewMessage(next, DataTypeNumber)}))
			next++
		}
		m, ok := q.pop(ctx)
		require.True(t, ok)
		require.Equal(t, want, m.Message.Payload)
		want++
	}
}

func TestNodeQueuePopHonoursContext(t *testing.T) {
	q := newNodeQueue(1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool)
	go func() { _, ok := q.pop(ctx); done <- ok }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case ok := <-done:
		assert.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("pop did not return on cancellation")
	}
}

func TestNodeQueuePopWakesOnPush(t *testing.T) {
	q := newNodeQueue(1)
	got := make(chan RoutedMessage)
	go func() { m, _ := q.pop(context.Background()); got <- m }()
	time.Sleep(10 * time.Millisecond)
	require.True(t, q.push(RoutedMessage{TargetNode: "n"}))
	select {
	case m := <-got:
		assert.Equal(t, "n", m.TargetNode)
	case <-time.After(time.Second):
		t.Fatal("pop did not wake on push")
	}
}
