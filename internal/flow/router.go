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
	DefaultSendTimeout    = 5 * time.Second
	DefaultOutputBuffer   = 1000
	DefaultWorkerPoolSize = 10
)

// RoutedMessage represents a message being routed to a specific node.
type RoutedMessage struct {
	Message    *types.Message
	TargetNode string
	TargetPort string
}

// MessageRouter handles message routing between nodes based on flow connections.
// It builds an adjacency map from connections and routes messages via channels.
type MessageRouter struct {
	// Adjacency map: sourceNode -> sourcePort -> []targetConnection
	adjacency map[string]map[string][]targetConnection

	// Input channels for each node (messages from triggers go here)
	inputChannels map[string]chan *types.Message

	// Output channel for routed messages (consumed by the runtime)
	output chan RoutedMessage

	// Worker pool for parallel message processing
	workerPool *workerPool

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// Send timeout for backpressure handling
	sendTimeout time.Duration

	// Metrics
	messagesDropped int64

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

// WithWorkerPoolSize sets the number of workers in the router's pool.
func WithWorkerPoolSize(size int) RouterOption {
	return func(r *MessageRouter) {
		r.workerPool = newWorkerPool(size)
	}
}

// WithSendTimeout sets the timeout for sending messages to the output channel.
// If a message cannot be sent within this timeout, it will be dropped.
func WithSendTimeout(timeout time.Duration) RouterOption {
	return func(r *MessageRouter) {
		r.sendTimeout = timeout
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
		workerPool:    newWorkerPool(DefaultWorkerPoolSize),
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

	// Start the worker pool
	r.workerPool.Start()

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

		// Submit to worker pool for async processing
		r.workerPool.Submit(func() {
			r.mu.RLock()
			closed := r.closed
			r.mu.RUnlock()

			if closed {
				return
			}

			// Use timeout-based send instead of dropping immediately
			select {
			case r.output <- routed:
				r.logger.Debug().
					Str("sourceNode", sourceNode).
					Str("targetNode", target.nodeID).
					Str("msgID", clonedMsg.ID).
					Msg("Message routed")
			case <-time.After(r.sendTimeout):
				atomic.AddInt64(&r.messagesDropped, 1)
				metrics.IncrementMessagesDropped()
				r.logger.Warn().
					Str("sourceNode", sourceNode).
					Str("targetNode", target.nodeID).
					Str("msgID", clonedMsg.ID).
					Dur("timeout", r.sendTimeout).
					Msg("Message dropped after timeout (output channel full)")
			case <-r.ctx.Done():
				r.logger.Debug().
					Str("sourceNode", sourceNode).
					Str("targetNode", target.nodeID).
					Msg("Message routing cancelled")
			}
		})
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
	// goroutines and the worker-pool jobs are all concurrent senders, and a
	// send on a closed channel panics (which would be unrecoverable and crash
	// the whole process). Every sender and reader already selects on r.ctx.Done,
	// so cancellation alone shuts everything down cleanly; the channels are
	// reclaimed by the GC once the router is unreferenced.
	r.cancel()

	// Stop the worker pool (workers exit via their done channel).
	r.workerPool.Stop()

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

// workerPool manages a pool of worker goroutines for parallel message processing.
type workerPool struct {
	size    int
	jobs    chan func()
	done    chan struct{}
	started bool
	stopped bool
	wg      sync.WaitGroup
	mu      sync.Mutex
}

// newWorkerPool creates a new worker pool with the given size.
func newWorkerPool(size int) *workerPool {
	if size < 1 {
		size = 1
	}
	return &workerPool{
		size: size,
		jobs: make(chan func(), 1000),
		done: make(chan struct{}),
	}
}

// Start begins the worker goroutines.
func (p *workerPool) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return
	}
	p.started = true

	for i := 0; i < p.size; i++ {
		p.wg.Go(p.worker)
	}
}

// worker is the main loop for a worker goroutine.
func (p *workerPool) worker() {
	for {
		select {
		case job, ok := <-p.jobs:
			if !ok {
				return
			}
			job()
		case <-p.done:
			return
		}
	}
}

// Submit adds a job to the worker pool.
func (p *workerPool) Submit(job func()) {
	select {
	case p.jobs <- job:
	default:
		// Pool is full, execute synchronously
		job()
	}
}

// Stop signals all workers to stop and waits for them to finish.
func (p *workerPool) Stop() {
	p.mu.Lock()
	if !p.started || p.stopped {
		p.mu.Unlock()
		return
	}
	p.stopped = true
	close(p.done)
	p.mu.Unlock()

	// Wait for all workers to finish. We do NOT close p.jobs: Submit is called
	// concurrently by routeFromPort, and a send on a closed channel panics. The
	// workers have already exited via the done channel, so any jobs still queued
	// are simply abandoned and reclaimed by the GC.
	p.wg.Wait()

	p.mu.Lock()
	p.started = false
	p.mu.Unlock()
}
