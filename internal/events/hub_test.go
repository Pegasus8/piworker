package events

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func recv(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(time.Second):
		t.Fatal("expected an event but none arrived")
		return Event{}
	}
}

func TestHubPublishDeliversToSubscriber(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe("flow-1")
	defer unsub()

	hub.Publish(Event{FlowID: "flow-1", NodeID: "n1", Phase: PhaseRunning})

	e := recv(t, ch)
	assert.Equal(t, "flow-1", e.FlowID)
	assert.Equal(t, "n1", e.NodeID)
	assert.Equal(t, PhaseRunning, e.Phase)
}

func TestHubPublishOnlyToMatchingFlow(t *testing.T) {
	hub := NewHub()
	chA, unsubA := hub.Subscribe("flow-A")
	defer unsubA()
	chB, unsubB := hub.Subscribe("flow-B")
	defer unsubB()

	hub.Publish(Event{FlowID: "flow-A", NodeID: "n1", Phase: PhaseSuccess})

	got := recv(t, chA)
	assert.Equal(t, "n1", got.NodeID)

	select {
	case e := <-chB:
		t.Fatalf("flow-B subscriber must not receive flow-A events, got %+v", e)
	case <-time.After(50 * time.Millisecond):
		// expected: nothing for flow-B
	}
}

func TestHubMultipleSubscribersSameFlow(t *testing.T) {
	hub := NewHub()
	ch1, unsub1 := hub.Subscribe("flow-1")
	defer unsub1()
	ch2, unsub2 := hub.Subscribe("flow-1")
	defer unsub2()

	hub.Publish(Event{FlowID: "flow-1", NodeID: "n1", Phase: PhaseError, Error: "boom"})

	for _, ch := range []<-chan Event{ch1, ch2} {
		e := recv(t, ch)
		assert.Equal(t, PhaseError, e.Phase)
		assert.Equal(t, "boom", e.Error)
	}
}

func TestHubUnsubscribeStopsDeliveryAndClosesChannel(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe("flow-1")

	unsub()

	// Channel must be closed so an SSE range-loop terminates.
	select {
	case _, open := <-ch:
		assert.False(t, open, "channel must be closed after unsubscribe")
	case <-time.After(time.Second):
		t.Fatal("expected closed channel after unsubscribe")
	}

	// Publishing after unsubscribe must not panic (no send on closed channel).
	assert.NotPanics(t, func() {
		hub.Publish(Event{FlowID: "flow-1", NodeID: "n1", Phase: PhaseRunning})
	})

	// Unsubscribe must be idempotent.
	assert.NotPanics(t, unsub)
}

// TestHubPublishNeverBlocks is the load-bearing concurrency guarantee: a slow or
// stalled subscriber (full buffer) must never apply backpressure to the producer
// (the flow runtime), so Publish drops rather than blocks.
func TestHubPublishNeverBlocks(t *testing.T) {
	hub := NewHub()
	_, unsub := hub.Subscribe("flow-1") // never drained
	defer unsub()

	done := make(chan struct{})
	go func() {
		// Far exceed any reasonable buffer; if Publish blocked, this never returns.
		for i := 0; i < subscriberBuffer*4; i++ {
			hub.Publish(Event{FlowID: "flow-1", NodeID: "n1", Phase: PhaseRunning})
		}
		close(done)
	}()

	select {
	case <-done:
		// passed: all publishes returned without blocking
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a full subscriber buffer")
	}
}
