package flow

import (
	"context"
	"sync"
)

// nodeQueue is a per-node FIFO mailbox with a hard size limit. Pushing never
// blocks: when the queue is full the push is refused and the caller drops the
// message. That keeps one slow node from back-pressuring the shared dispatcher
// (and through it every other branch of the flow) while still bounding memory.
type nodeQueue struct {
	mu     sync.Mutex
	items  []RoutedMessage
	head   int
	limit  int
	notify chan struct{} // capacity 1: "items may be available"
}

func newNodeQueue(limit int) *nodeQueue {
	if limit < 1 {
		limit = 1
	}
	return &nodeQueue{limit: limit, notify: make(chan struct{}, 1)}
}

// push appends m and reports whether it was accepted (false = queue full).
func (q *nodeQueue) push(m RoutedMessage) bool {
	q.mu.Lock()
	if len(q.items)-q.head >= q.limit {
		q.mu.Unlock()
		return false
	}
	q.items = append(q.items, m)
	q.mu.Unlock()

	select {
	case q.notify <- struct{}{}:
	default:
	}
	return true
}

// pop removes and returns the oldest message, blocking until one is available
// or ctx is done (ok=false).
func (q *nodeQueue) pop(ctx context.Context) (RoutedMessage, bool) {
	for {
		q.mu.Lock()
		if q.head < len(q.items) {
			m := q.items[q.head]
			q.items[q.head] = RoutedMessage{} // release the reference for the GC
			q.head++
			if q.head == len(q.items) {
				// Drained: reuse the backing array from the start.
				q.items = q.items[:0]
				q.head = 0
			} else if q.head > q.limit {
				// Compact so a long-lived backlog can't grow the array forever.
				n := copy(q.items, q.items[q.head:])
				clear(q.items[n:])
				q.items = q.items[:n]
				q.head = 0
			}
			q.mu.Unlock()
			return m, true
		}
		q.mu.Unlock()

		select {
		case <-q.notify:
		case <-ctx.Done():
			return RoutedMessage{}, false
		}
	}
}

// len reports how many messages are waiting.
func (q *nodeQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items) - q.head
}
