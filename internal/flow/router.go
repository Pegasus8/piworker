package flow

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
)

// Default router configuration values.
const (
	DefaultSendTimeout  = 5 * time.Second
	DefaultOutputBuffer = 1000
)

// RoutedMessage represents a message being routed to a specific node.
type RoutedMessage struct {
	Message    *types.Message
	TargetNode string
	TargetPort string
}

// MessageRouter handles message routing between nodes based on flow connections.
// It builds an adjacency map from connections and routes messages via channels.
//
// Route delivers synchronously, in call order, onto a single output channel, so
// messages between the same (source, target) pair are always FIFO: every source
// node routes from one goroutine (its serial executor, or its trigger's
// forwarder), and that goroutine's sends land on the channel in order.
type MessageRouter struct {
	// Adjacency map: sourceNode -> sourcePort -> []targetConnection
	adjacency map[string]map[string][]targetConnection

	// Input channels for each node (messages from triggers go here)
	inputChannels map[string]chan *types.Message

	// Output channel for routed messages (consumed by the runtime)
	output chan RoutedMessage

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// Send timeout for backpressure handling
	sendTimeout time.Duration

	// Metrics
	messagesDropped int64

	// onDrop, when set, is called for every routed message dropped on send
	// timeout, so the owner can account for work that will never execute.
	onDrop func(RoutedMessage)

	// noMetrics keeps drops out of the process-wide metrics.
	noMetrics bool

	logger zerolog.Logger

	mu     sync.RWMutex
	closed bool
}

// targetConnection represents a single target for a connection.
type targetConnection struct {
	nodeID string
	portID string
}

// RouterOption is a functional option for configuring the MessageRouter.
type RouterOption func(*MessageRouter)

// WithSendTimeout sets the timeout for sending messages to the output channel.
// If a message cannot be sent within this timeout, it will be dropped.
func WithSendTimeout(timeout time.Duration) RouterOption {
	return func(r *MessageRouter) {
		r.sendTimeout = timeout
	}
}

// WithDropHandler registers a callback invoked (synchronously, on the routing
// goroutine) for each message dropped because the output stayed full past the
// send timeout. It must not block.
func WithDropHandler(fn func(RoutedMessage)) RouterOption {
	return func(r *MessageRouter) {
		r.onDrop = fn
	}
}

// withoutRouterMetrics keeps the router's drops out of the process-wide
// metrics (a dry-run's private runtime).
func withoutRouterMetrics() RouterOption {
	return func(r *MessageRouter) {
		r.noMetrics = true
	}
}

// WithContext sets the context for the router.
func WithContext(ctx context.Context) RouterOption {
	return func(r *MessageRouter) {
		r.ctx, r.cancel = context.WithCancel(ctx)
	}
}

// NewMessageRouter creates a new MessageRouter from the given connections.
func NewMessageRouter(connections []Connection, logger zerolog.Logger, opts ...RouterOption) *MessageRouter {
	ctx, cancel := context.WithCancel(context.Background())

	r := &MessageRouter{
		adjacency:     make(map[string]map[string][]targetConnection),
		inputChannels: make(map[string]chan *types.Message),
		output:        make(chan RoutedMessage, DefaultOutputBuffer),
		ctx:           ctx,
		cancel:        cancel,
		sendTimeout:   DefaultSendTimeout,
		logger:        logger,
		closed:        false,
	}

	// Apply options
	for _, opt := range opts {
		opt(r)
	}

	// Build the adjacency map from connections
	for _, conn := range connections {
		if _, exists := r.adjacency[conn.SourceNode]; !exists {
			r.adjacency[conn.SourceNode] = make(map[string][]targetConnection)
		}

		r.adjacency[conn.SourceNode][conn.SourcePort] = append(
			r.adjacency[conn.SourceNode][conn.SourcePort],
			targetConnection{
				nodeID: conn.TargetNode,
				portID: conn.TargetPort,
			},
		)
	}

	return r
}

// GetInputChannel returns or creates an input channel for a node.
// This is used by trigger nodes to send their initial messages.
func (r *MessageRouter) GetInputChannel(nodeID string) chan *types.Message {
	r.mu.Lock()
	defer r.mu.Unlock()

	if ch, exists := r.inputChannels[nodeID]; exists {
		return ch
	}

	ch := make(chan *types.Message, DefaultOutputBuffer)
	r.inputChannels[nodeID] = ch

	// Start a goroutine to forward messages from this input channel to routing
	// with proper context cancellation support to prevent goroutine leaks
	go func() {
		for {
			select {
			case <-r.ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				r.Route(nodeID, msg)
			}
		}
	}()

	return ch
}

// Route sends a message from a source node to all connected target nodes.
func (r *MessageRouter) Route(sourceNode string, msg *types.Message) {
	r.mu.RLock()
	if r.closed {
		r.mu.RUnlock()
		return
	}
	r.mu.RUnlock()

	// Use the message's source port if available, otherwise route from all ports
	sourcePort := msg.SourcePort

	if sourcePort != "" {
		// Route from specific port
		r.routeFromPort(sourceNode, sourcePort, msg)
	} else {
		// Route from all ports of this node
		r.mu.RLock()
		nodePorts, exists := r.adjacency[sourceNode]
		r.mu.RUnlock()

		if exists {
			for port := range nodePorts {
				r.routeFromPort(sourceNode, port, msg)
			}
		}
	}
}

// routeFromPort routes a message from a specific source port.
func (r *MessageRouter) routeFromPort(sourceNode, sourcePort string, msg *types.Message) {
	r.mu.RLock()
	targets, exists := r.adjacency[sourceNode][sourcePort]
	r.mu.RUnlock()

	if !exists || len(targets) == 0 {
		r.logger.Debug().
			Str("sourceNode", sourceNode).
			Str("sourcePort", sourcePort).
			Msg("No targets for message")
		return
	}

	for _, target := range targets {
		// Clone the message for each target to avoid data races
		clonedMsg := msg.Clone()
		clonedMsg.SourceNode = sourceNode
		clonedMsg.SourcePort = sourcePort

		routed := RoutedMessage{
			Message:    clonedMsg,
			TargetNode: target.nodeID,
			TargetPort: target.portID,
		}

		r.send(sourceNode, routed)
	}
}

// send delivers one routed message to the output channel, in call order. It
// waits up to sendTimeout for space (backpressure) before dropping.
func (r *MessageRouter) send(sourceNode string, routed RoutedMessage) {
	r.mu.RLock()
	closed := r.closed
	r.mu.RUnlock()
	if closed {
		return
	}

	// Fast path: no timer allocation when the channel has room.
	select {
	case r.output <- routed:
		return
	default:
	}

	timer := time.NewTimer(r.sendTimeout)
	defer timer.Stop()
	select {
	case r.output <- routed:
	case <-timer.C:
		atomic.AddInt64(&r.messagesDropped, 1)
		if !r.noMetrics {
			metrics.IncrementMessagesDropped()
		}
		r.logger.Warn().
			Str("sourceNode", sourceNode).
			Str("targetNode", routed.TargetNode).
			Str("msgID", routed.Message.ID).
			Dur("timeout", r.sendTimeout).
			Msg("Message dropped after timeout (output channel full)")
		if r.onDrop != nil {
			r.onDrop(routed)
		}
	case <-r.ctx.Done():
		r.logger.Debug().
			Str("sourceNode", sourceNode).
			Str("targetNode", routed.TargetNode).
			Msg("Message routing cancelled")
	}
}

// Output returns the channel for receiving routed messages.
func (r *MessageRouter) Output() <-chan RoutedMessage {
	return r.output
}

// Close stops the router and closes all channels.
func (r *MessageRouter) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()

	// Cancel the context to signal goroutines to stop. We deliberately do NOT
	// close the input/output channels: triggers, the input-forwarding
	// goroutines and node executors are all concurrent senders, and a
	// send on a closed channel panics (which would be unrecoverable and crash
	// the whole process). Every sender and reader already selects on r.ctx.Done,
	// so cancellation alone shuts everything down cleanly; the channels are
	// reclaimed by the GC once the router is unreferenced.
	r.cancel()

	r.logger.Debug().Msg("Message router closed")
}

// MessagesDropped returns the number of messages dropped due to backpressure.
func (r *MessageRouter) MessagesDropped() int64 {
	return atomic.LoadInt64(&r.messagesDropped)
}

// GetTargets returns all target nodes for a given source node and port.
func (r *MessageRouter) GetTargets(sourceNode, sourcePort string) []targetConnection {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if ports, exists := r.adjacency[sourceNode]; exists {
		if targets, ok := ports[sourcePort]; ok {
			// Return a copy to avoid race conditions
			result := make([]targetConnection, len(targets))
			copy(result, targets)
			return result
		}
	}
	return nil
}

// CountTargets returns how many targets a (sourceNode, sourcePort) routes to,
// without allocating. Unlike GetTargets it copies nothing, so it's cheap to call
// on the per-message hot path (e.g. sizing the in-flight run counter). The
// adjacency map is immutable after construction; the nil-map indexing is safe.
func (r *MessageRouter) CountTargets(sourceNode, sourcePort string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.adjacency[sourceNode][sourcePort])
}
