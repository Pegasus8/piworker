// Package events provides a process-wide publish/subscribe hub that streams
// per-node execution events (running/success/error) and debug-node output from
// the flow runtime to live subscribers such as the editor's SSE endpoint.
//
// The hub is deliberately a leaf with no back-references into the runtime: the
// runtime only ever calls Publish, which delivers to each subscriber with a
// non-blocking send. A slow or stalled subscriber (e.g. a disconnected
// EventSource) therefore drops events for itself alone and can never apply
// backpressure to node execution. This mirrors the webhook.Hub pattern.
package events

import "sync"

// Phase values for an Event.
const (
	PhaseRunning = "running"
	PhaseSuccess = "success"
	PhaseError   = "error"
	PhaseDebug   = "debug"
)

// subscriberBuffer is the per-subscriber channel capacity. Events beyond this
// (a subscriber that isn't draining fast enough) are dropped by Publish.
const subscriberBuffer = 256

// Event is a single node-execution observation delivered to subscribers and
// serialized to SSE clients.
type Event struct {
	FlowID   string      `json:"flowId"`
	NodeID   string      `json:"nodeId"`
	NodeType string      `json:"nodeType,omitempty"`
	Phase    string      `json:"phase"` // running|success|error|debug
	MsgID    string      `json:"msgId,omitempty"`
	CorrID   string      `json:"corrId,omitempty"`
	DurMs    float64     `json:"durMs,omitempty"`
	Error    string      `json:"error,omitempty"`
	Debug    interface{} `json:"debug,omitempty"` // payload/meta snapshot for process-debug
	Ts       int64       `json:"ts,omitempty"`    // unix millis
}

// Hub fans events out to subscribers keyed by flow ID. A subscriber is just its
// buffered channel, used directly as the map key.
type Hub struct {
	mu   sync.RWMutex
	subs map[string]map[chan Event]struct{}
}

// NewHub creates an empty Hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[chan Event]struct{})}
}

// Subscribe registers a new subscriber for a flow's events. It returns a
// receive-only channel and an idempotent unsubscribe function that removes the
// subscriber and closes the channel (so an SSE range-loop terminates).
func (h *Hub) Subscribe(flowID string) (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)

	h.mu.Lock()
	set := h.subs[flowID]
	if set == nil {
		set = make(map[chan Event]struct{})
		h.subs[flowID] = set
	}
	set[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			h.mu.Lock()
			if set := h.subs[flowID]; set != nil {
				delete(set, ch)
				if len(set) == 0 {
					delete(h.subs, flowID)
				}
			}
			close(ch)
			h.mu.Unlock()
		})
	}
	return ch, unsub
}

// HasSubscribers reports whether a flow currently has any live subscriber, so
// the producer can skip building events nobody will receive.
func (h *Hub) HasSubscribers(flowID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[flowID]) > 0
}

// Publish delivers an event to every subscriber of e.FlowID with a non-blocking
// send. A full subscriber buffer causes the event to be dropped for that
// subscriber rather than blocking the caller. Publish and Subscribe/unsubscribe
// are mutually exclusive on h.mu, so a subscriber's channel is never closed
// concurrently with a send to it.
func (h *Hub) Publish(e Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs[e.FlowID] {
		select {
		case ch <- e:
		default:
			// Subscriber is not keeping up; drop rather than block the producer.
		}
	}
}

// DefaultHub is the process-wide hub shared by the flow runtime recorder and the
// HTTP SSE endpoint.
var DefaultHub = NewHub()
