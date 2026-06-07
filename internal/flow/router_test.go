package flow

import (
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// MessageRouter Creation Tests
// ============================================================================

func TestNewMessageRouter(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("creates router with empty connections", func(t *testing.T) {
		router := NewMessageRouter([]Connection{}, logger)

		assert.NotNil(t, router)
		assert.NotNil(t, router.Output())
	})

	t.Run("creates router with single connection", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "trigger", SourcePort: "output", TargetNode: "action", TargetPort: "input"},
		}

		router := NewMessageRouter(connections, logger)

		assert.NotNil(t, router)
	})

	t.Run("creates router with multiple connections", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "trigger", SourcePort: "output", TargetNode: "action1", TargetPort: "input"},
			{ID: "c2", SourceNode: "trigger", SourcePort: "output", TargetNode: "action2", TargetPort: "input"},
			{ID: "c3", SourceNode: "action1", SourcePort: "output", TargetNode: "action3", TargetPort: "input"},
		}

		router := NewMessageRouter(connections, logger)

		assert.NotNil(t, router)
	})

	t.Run("builds adjacency map correctly", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "A", SourcePort: "out1", TargetNode: "B", TargetPort: "in1"},
			{ID: "c2", SourceNode: "A", SourcePort: "out1", TargetNode: "C", TargetPort: "in1"},
			{ID: "c3", SourceNode: "A", SourcePort: "out2", TargetNode: "D", TargetPort: "in1"},
			{ID: "c4", SourceNode: "B", SourcePort: "out1", TargetNode: "E", TargetPort: "in1"},
		}

		router := NewMessageRouter(connections, logger)

		// Verify adjacency map structure
		targets := router.GetTargets("A", "out1")
		assert.Len(t, targets, 2)

		targets = router.GetTargets("A", "out2")
		assert.Len(t, targets, 1)

		targets = router.GetTargets("B", "out1")
		assert.Len(t, targets, 1)
	})
}

// ============================================================================
// Message Routing Tests
// ============================================================================

func TestMessageRouterRoute(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("routes message to single target", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "trigger", SourcePort: "output", TargetNode: "action", TargetPort: "input"},
		}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		msg := NewMessage("test", DataTypeString)
		msg.SourcePort = "output"

		router.Route("trigger", msg)

		select {
		case routed := <-router.Output():
			assert.Equal(t, "action", routed.TargetNode)
			assert.Equal(t, "input", routed.TargetPort)
			assert.Equal(t, "test", routed.Message.Payload)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for routed message")
		}
	})

	t.Run("routes message to multiple targets", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "trigger", SourcePort: "output", TargetNode: "action1", TargetPort: "input"},
			{ID: "c2", SourceNode: "trigger", SourcePort: "output", TargetNode: "action2", TargetPort: "input"},
			{ID: "c3", SourceNode: "trigger", SourcePort: "output", TargetNode: "action3", TargetPort: "input"},
		}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		msg := NewMessage("test", DataTypeString)
		msg.SourcePort = "output"

		router.Route("trigger", msg)

		receivedTargets := make(map[string]bool)
		timeout := time.After(500 * time.Millisecond)

		for i := 0; i < 3; i++ {
			select {
			case routed := <-router.Output():
				receivedTargets[routed.TargetNode] = true
			case <-timeout:
				t.Fatalf("timeout waiting for message %d, received: %v", i+1, receivedTargets)
			}
		}

		assert.True(t, receivedTargets["action1"])
		assert.True(t, receivedTargets["action2"])
		assert.True(t, receivedTargets["action3"])
	})

	t.Run("does not route message when no connections exist", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "trigger", SourcePort: "output", TargetNode: "action", TargetPort: "input"},
		}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		msg := NewMessage("test", DataTypeString)
		msg.SourcePort = "output"

		// Route from non-existent node
		router.Route("nonexistent", msg)

		select {
		case <-router.Output():
			t.Fatal("should not receive message from non-existent source")
		case <-time.After(50 * time.Millisecond):
			// Expected: no message
		}
	})

	t.Run("routes message only to matching port", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "node", SourcePort: "port1", TargetNode: "action1", TargetPort: "input"},
			{ID: "c2", SourceNode: "node", SourcePort: "port2", TargetNode: "action2", TargetPort: "input"},
		}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		msg := NewMessage("test", DataTypeString)
		msg.SourcePort = "port1"

		router.Route("node", msg)

		select {
		case routed := <-router.Output():
			assert.Equal(t, "action1", routed.TargetNode)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for routed message")
		}

		// Should not receive second message
		select {
		case <-router.Output():
			t.Fatal("should not receive message for different port")
		case <-time.After(50 * time.Millisecond):
			// Expected
		}
	})

	t.Run("clones message for each target", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "trigger", SourcePort: "output", TargetNode: "action1", TargetPort: "input"},
			{ID: "c2", SourceNode: "trigger", SourcePort: "output", TargetNode: "action2", TargetPort: "input"},
		}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		msg := NewMessage("test", DataTypeString)
		msg.SourcePort = "output"

		router.Route("trigger", msg)

		messageIDs := make(map[string]bool)
		timeout := time.After(200 * time.Millisecond)

		for i := 0; i < 2; i++ {
			select {
			case routed := <-router.Output():
				messageIDs[routed.Message.ID] = true
			case <-timeout:
				t.Fatal("timeout waiting for message")
			}
		}

		// All messages should have different IDs (cloned)
		assert.Len(t, messageIDs, 2)
	})
}

// ============================================================================
// Input Channel Tests
// ============================================================================

func TestMessageRouterGetInputChannel(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("returns consistent channel for same node", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "trigger", SourcePort: "output", TargetNode: "action", TargetPort: "input"},
		}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		ch1 := router.GetInputChannel("trigger")
		ch2 := router.GetInputChannel("trigger")

		assert.Equal(t, ch1, ch2)
	})

	t.Run("returns different channels for different nodes", func(t *testing.T) {
		connections := []Connection{}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		ch1 := router.GetInputChannel("node1")
		ch2 := router.GetInputChannel("node2")

		assert.NotEqual(t, ch1, ch2)
	})

	t.Run("forwards messages from input channel to routing", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "trigger", SourcePort: "output", TargetNode: "action", TargetPort: "input"},
		}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		inputCh := router.GetInputChannel("trigger")

		msg := NewMessage("test", DataTypeString)
		msg.SourcePort = "output"

		inputCh <- msg

		select {
		case routed := <-router.Output():
			assert.Equal(t, "action", routed.TargetNode)
			assert.Equal(t, "test", routed.Message.Payload)
		case <-time.After(200 * time.Millisecond):
			t.Fatal("timeout waiting for routed message")
		}
	})
}

// ============================================================================
// Router Closing Tests
// ============================================================================

func TestMessageRouterClose(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("closes router gracefully", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "trigger", SourcePort: "output", TargetNode: "action", TargetPort: "input"},
		}

		router := NewMessageRouter(connections, logger)

		// Get input channel first
		_ = router.GetInputChannel("trigger")

		router.Close()

		// The output channel is intentionally NOT closed on shutdown: it has
		// concurrent senders (the worker pool), and closing a channel with live
		// senders would panic and crash the process. Instead, routing after
		// Close is a no-op, so no message is ever delivered.
		msg := NewMessage("test", DataTypeString)
		msg.SourcePort = "output"
		router.Route("trigger", msg)

		select {
		case _, ok := <-router.Output():
			assert.False(t, ok, "no message should be delivered after Close")
		case <-time.After(100 * time.Millisecond):
			// Expected: routing after Close delivered nothing.
		}
	})

	t.Run("does not route messages after close", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "trigger", SourcePort: "output", TargetNode: "action", TargetPort: "input"},
		}

		router := NewMessageRouter(connections, logger)
		router.Close()

		msg := NewMessage("test", DataTypeString)
		msg.SourcePort = "output"

		// Should not panic
		router.Route("trigger", msg)
	})

	t.Run("double close does not panic", func(t *testing.T) {
		connections := []Connection{}

		router := NewMessageRouter(connections, logger)

		router.Close()
		router.Close() // Should not panic
	})
}

// ============================================================================
// GetTargets Tests
// ============================================================================

func TestMessageRouterGetTargets(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("returns targets for existing node and port", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "A", SourcePort: "out", TargetNode: "B", TargetPort: "in"},
			{ID: "c2", SourceNode: "A", SourcePort: "out", TargetNode: "C", TargetPort: "in"},
		}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		targets := router.GetTargets("A", "out")

		assert.Len(t, targets, 2)
	})

	t.Run("returns nil for non-existent node", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "A", SourcePort: "out", TargetNode: "B", TargetPort: "in"},
		}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		targets := router.GetTargets("X", "out")

		assert.Nil(t, targets)
	})

	t.Run("returns nil for non-existent port", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "A", SourcePort: "out", TargetNode: "B", TargetPort: "in"},
		}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		targets := router.GetTargets("A", "nonexistent")

		assert.Nil(t, targets)
	})
}

// ============================================================================
// Concurrent Access Tests
// ============================================================================

func TestMessageRouterConcurrency(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("handles concurrent routing", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "trigger", SourcePort: "output", TargetNode: "action", TargetPort: "input"},
		}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		var wg sync.WaitGroup
		messageCount := 100

		// Start consumer
		received := 0
		done := make(chan bool)
		go func() {
			for range router.Output() {
				received++
				if received >= messageCount {
					done <- true
					return
				}
			}
		}()

		// Concurrent producers
		for i := 0; i < messageCount; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				msg := NewMessage("test", DataTypeString)
				msg.SourcePort = "output"
				router.Route("trigger", msg)
			}()
		}

		wg.Wait()

		select {
		case <-done:
			assert.Equal(t, messageCount, received)
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout: received %d of %d messages", received, messageCount)
		}
	})

	t.Run("handles concurrent input channel access", func(t *testing.T) {
		connections := []Connection{}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		var wg sync.WaitGroup

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				nodeID := "node" + string(rune('0'+id))
				ch := router.GetInputChannel(nodeID)
				assert.NotNil(t, ch)
			}(i)
		}

		wg.Wait()
	})
}

// ============================================================================
// Worker Pool Tests
// ============================================================================

func TestWorkerPool(t *testing.T) {
	t.Run("creates pool with valid size", func(t *testing.T) {
		pool := newWorkerPool(5)

		assert.NotNil(t, pool)
		assert.Equal(t, 5, pool.size)
	})

	t.Run("creates pool with minimum size for invalid input", func(t *testing.T) {
		pool := newWorkerPool(0)

		assert.Equal(t, 1, pool.size)

		pool = newWorkerPool(-5)
		assert.Equal(t, 1, pool.size)
	})

	t.Run("executes submitted jobs", func(t *testing.T) {
		pool := newWorkerPool(3)
		pool.Start()
		defer pool.Stop()

		executed := make(chan bool, 10)

		for i := 0; i < 10; i++ {
			pool.Submit(func() {
				executed <- true
			})
		}

		for i := 0; i < 10; i++ {
			select {
			case <-executed:
				// Job executed
			case <-time.After(1 * time.Second):
				t.Fatalf("timeout waiting for job %d", i)
			}
		}
	})

	t.Run("stops gracefully", func(t *testing.T) {
		pool := newWorkerPool(3)
		pool.Start()

		// Submit some jobs
		counter := 0
		var mu sync.Mutex
		for i := 0; i < 5; i++ {
			pool.Submit(func() {
				mu.Lock()
				counter++
				mu.Unlock()
			})
		}

		time.Sleep(50 * time.Millisecond)
		pool.Stop()

		mu.Lock()
		assert.Equal(t, 5, counter)
		mu.Unlock()
	})

	t.Run("double start does not panic", func(t *testing.T) {
		pool := newWorkerPool(2)

		pool.Start()
		pool.Start() // Should not panic
		pool.Stop()
	})

	t.Run("double stop does not panic", func(t *testing.T) {
		pool := newWorkerPool(2)
		pool.Start()

		pool.Stop()
		pool.Stop() // Should not panic
	})
}

// ============================================================================
// RoutedMessage Tests
// ============================================================================

func TestRoutedMessage(t *testing.T) {
	t.Run("contains all required fields", func(t *testing.T) {
		msg := NewMessage("test", DataTypeString)

		routed := RoutedMessage{
			Message:    msg,
			TargetNode: "action",
			TargetPort: "input",
		}

		assert.Equal(t, msg, routed.Message)
		assert.Equal(t, "action", routed.TargetNode)
		assert.Equal(t, "input", routed.TargetPort)
	})
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestMessageRouterEdgeCases(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("handles connections with same source and different ports", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "node", SourcePort: "success", TargetNode: "action1", TargetPort: "input"},
			{ID: "c2", SourceNode: "node", SourcePort: "error", TargetNode: "action2", TargetPort: "input"},
		}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		// Send to success port
		msg := NewMessage("success", DataTypeString)
		msg.SourcePort = "success"
		router.Route("node", msg)

		select {
		case routed := <-router.Output():
			assert.Equal(t, "action1", routed.TargetNode)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout")
		}

		// Send to error port
		msg = NewMessage("error", DataTypeString)
		msg.SourcePort = "error"
		router.Route("node", msg)

		select {
		case routed := <-router.Output():
			assert.Equal(t, "action2", routed.TargetNode)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout")
		}
	})

	t.Run("handles message with nil payload", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "trigger", SourcePort: "output", TargetNode: "action", TargetPort: "input"},
		}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		msg := NewMessage(nil, DataTypeAny)
		msg.SourcePort = "output"

		router.Route("trigger", msg)

		select {
		case routed := <-router.Output():
			assert.Nil(t, routed.Message.Payload)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout")
		}
	})

	t.Run("handles chain of nodes", func(t *testing.T) {
		connections := []Connection{
			{ID: "c1", SourceNode: "A", SourcePort: "out", TargetNode: "B", TargetPort: "in"},
			{ID: "c2", SourceNode: "B", SourcePort: "out", TargetNode: "C", TargetPort: "in"},
			{ID: "c3", SourceNode: "C", SourcePort: "out", TargetNode: "D", TargetPort: "in"},
		}

		router := NewMessageRouter(connections, logger)
		defer router.Close()

		// Simulate chain processing
		msg := NewMessage("test", DataTypeString)
		msg.SourcePort = "out"

		// A -> B
		router.Route("A", msg)
		routedB := <-router.Output()
		require.Equal(t, "B", routedB.TargetNode)

		// B -> C
		router.Route("B", routedB.Message.WithPayload("from_B", DataTypeString))
		routedC := <-router.Output()
		require.Equal(t, "C", routedC.TargetNode)

		// C -> D
		router.Route("C", routedC.Message.WithPayload("from_C", DataTypeString))
		routedD := <-router.Output()
		require.Equal(t, "D", routedD.TargetNode)
	})
}
