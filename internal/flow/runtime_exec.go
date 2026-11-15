package flow

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// routeAllowed asks the route gate, if any, whether msg may be routed as
// copies messages.
func (r *FlowRuntime) routeAllowed(msg *types.Message, copies int) bool {
	return r.routeGate == nil || copies <= 0 || r.routeGate(msg, copies)
}

// dispatch reads routed messages from the router and hands each one to its
// target node's queue. The hand-off never blocks: a slow or blocking node (a
// delay, a long HTTP call) only grows its own queue, so unrelated branches
// keep flowing. A node whose queue is full has the message dropped.
func (r *FlowRuntime) dispatch() {
	defer r.wg.Done()

	for {
		select {
		case <-r.ctx.Done():
			r.logger.Debug().Msg("Dispatcher stopped")
			return

		case routedMsg, ok := <-r.router.Output():
			if !ok {
				r.logger.Debug().Msg("Router output channel closed")
				return
			}

			q, exists := r.queues[routedMsg.TargetNode]
			if !exists {
				r.logger.Error().
					Str("nodeID", routedMsg.TargetNode).
					Msg("Target node not found or disabled; dropping message")
				r.countDrop()
				r.releaseDropped(routedMsg, "the node is not running (not found or disabled)")
				continue
			}

			if !q.push(routedMsg) {
				r.logger.Warn().
					Str("flowID", r.flow.ID).
					Str("nodeID", routedMsg.TargetNode).
					Str("msgID", routedMsg.Message.ID).
					Int("queueLimit", r.queueLimit).
					Msg("Node queue full; dropping message")
				r.countDrop()
				r.releaseDropped(routedMsg, fmt.Sprintf("its queue is full (limit %d)", r.queueLimit))
			}
		}
	}
}

// countDrop counts a dropped message in the process-wide metrics.
func (r *FlowRuntime) countDrop() {
	if !r.noMetrics {
		metrics.IncrementMessagesDropped()
	}
}

// actorOf returns msg's "actor" meta (set by Call on a run's first message
// and inherited by its clones), or "" when it is missing or not a string.
func actorOf(msg *types.Message) string {
	if msg == nil {
		return ""
	}
	v, _ := msg.GetMeta("actor")
	actor, _ := v.(string)
	return actor
}

// releaseDropped accounts for a routed message that will never execute: it
// counts the drop, records it as the error of a called run (reason says why),
// and releases the message's slot in its run, so the run still finishes.
// Every drop path (queue overflow, unknown target, router send timeout) must
// call it exactly once per dropped message.
func (r *FlowRuntime) releaseDropped(routedMsg RoutedMessage, reason string) {
	r.droppedMessages.Add(1)
	corrID := r.ensureCorrelationID(routedMsg.Message)
	nodeType := ""
	if def := r.nodeDefs[routedMsg.TargetNode]; def != nil {
		nodeType = def.Type
	}
	r.calls.dropped(corrID, routedMsg.TargetNode, nodeType, reason)
	r.runDone(corrID)
}

// DroppedMessages reports how many messages this runtime discarded.
func (r *FlowRuntime) DroppedMessages() int64 {
	return r.droppedMessages.Load()
}

// nodeExecutor drains a single node's queue serially. Serial per-node
// execution preserves message ordering for stateful nodes (batch, dedupe,
// throttle), while the shared sem caps how many nodes run at once across the
// whole flow so an unbounded fan-out can't spawn unbounded work.
func (r *FlowRuntime) nodeExecutor(nodeID string, q *nodeQueue) {
	defer r.wg.Done()

	for {
		routedMsg, ok := q.pop(r.ctx)
		if !ok {
			return
		}

		// Acquire a global concurrency slot, respecting cancellation.
		slot := &semSlot{sem: r.sem}
		if !slot.acquire(r.ctx) {
			return
		}

		func() {
			defer slot.release()
			r.executeNodeInSlot(routedMsg, slot)
		}()
	}
}

// semSlot tracks whether an executor currently holds a concurrency slot, so the
// slot can be handed back during retry backoff and re-acquired afterwards
// without ever being released twice.
type semSlot struct {
	sem  chan struct{}
	held bool
}

// acquire takes a slot, or returns false if ctx ends first.
func (s *semSlot) acquire(ctx context.Context) bool {
	if s == nil || s.held {
		return true
	}
	select {
	case s.sem <- struct{}{}:
		s.held = true
		return true
	case <-ctx.Done():
		return false
	}
}

// release gives the slot back if it is held.
func (s *semSlot) release() {
	if s == nil || !s.held {
		return
	}
	<-s.sem
	s.held = false
}

// executeNode processes a message through the target node and routes the output.
// It uses exponential backoff retry for transient failures.
func (r *FlowRuntime) executeNode(routedMsg RoutedMessage) {
	r.executeNodeInSlot(routedMsg, nil)
}

// executeNodeInSlot is executeNode for a caller holding a concurrency slot; the
// slot is released while waiting out retry backoff (nil = no slot to manage).
func (r *FlowRuntime) executeNodeInSlot(routedMsg RoutedMessage, slot *semSlot) {
	// Assign the run's correlation ID up front and decrement the run counter on
	// every return path — this message has been consumed. The child increments
	// happen below, before routing, so the counter never reaches zero while work
	// for the run is still pending. When it does reach zero, runDone emits the
	// run-finished signal.
	correlationID := r.ensureCorrelationID(routedMsg.Message)
	defer r.runDone(correlationID)

	nodeInstance, ok := r.nodes[routedMsg.TargetNode]
	if !ok {
		r.logger.Error().
			Str("nodeID", routedMsg.TargetNode).
			Msg("Target node not found")
		return
	}

	// Get the flow node definition for metadata (precomputed, lock-free).
	flowNode, ok := r.nodeDefs[routedMsg.TargetNode]
	if !ok || flowNode == nil {
		r.logger.Error().
			Str("nodeID", routedMsg.TargetNode).
			Msg("Flow node definition not found")
		return
	}

	// Shared fields for every NodeEvent this execution emits; each fire point
	// copies it and sets only Phase/Duration/Outputs/Err.
	baseEvent := NodeEvent{
		FlowID:      r.flow.ID,
		NodeID:      routedMsg.TargetNode,
		NodeType:    flowNode.Type,
		CorrID:      correlationID,
		Actor:       actorOf(routedMsg.Message),
		FlowVersion: r.version,
		Input:       routedMsg.Message,
	}

	// A panic inside a node's Process must not take down the executor goroutine
	// (and through it the whole runtime). Recover, log with a stack trace, and
	// record it as a node error so it is observable instead of silent. We also
	// emit a terminal error NodeEvent so a panicking node doesn't leave the
	// observer (and the editor) stuck showing it as perpetually "running".
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error().
				Interface("panic", rec).
				Str("nodeID", routedMsg.TargetNode).
				Str("nodeType", flowNode.Type).
				Bytes("stack", debug.Stack()).
				Msg("Recovered from panic during node execution")
			if !r.noMetrics {
				metrics.RecordNodeError(flowNode.Type)
			}
			panicErr := fmt.Errorf("panic: %v", rec)
			if r.observer != nil {
				ev := baseEvent
				ev.Phase = NodePhaseError
				ev.Err = panicErr
				r.observer(ev)
			}
			r.calls.nodeError(correlationID, routedMsg.TargetNode, flowNode.Type, panicErr)
		}
	}()

	// Create a logger with correlation context
	logger := r.logger.With().
		Str("correlationID", correlationID).
		Str("flowID", r.flow.ID).
		Str("nodeID", routedMsg.TargetNode).
		Str("nodeType", flowNode.Type).
		Logger()

	// Emit the "running" event. The observer is non-blocking by contract, so it
	// never stalls the executor goroutine that holds a semaphore slot.
	if r.observer != nil {
		ev := baseEvent
		ev.Phase = NodePhaseRunning
		r.observer(ev)
	}

	logger.Debug().
		Str("msgID", routedMsg.Message.ID).
		Msg("Executing node")

	// Execute the node with retry logic
	startTime := time.Now()
	outputs, err := r.executeNodeWithRetry(nodeInstance, routedMsg, flowNode, logger, slot)
	duration := time.Since(startTime)

	// Record metrics
	if !r.noMetrics {
		metrics.RecordNodeExecution(flowNode.Type, duration.Seconds())
		metrics.RecordMessageProcessed(flowNode.Type, err == nil)
	}

	// Emit the terminal event (success/error) with timing and, on success, the
	// outputs (used to attribute process-debug output to its canvas node).
	if r.observer != nil {
		ev := baseEvent
		ev.Phase = NodePhaseSuccess
		ev.Duration = duration
		if err != nil {
			ev.Phase = NodePhaseError
			ev.Err = err
		} else {
			ev.Outputs = outputs
		}
		r.observer(ev)
	}

	if err != nil {
		if !r.noMetrics {
			metrics.RecordNodeError(flowNode.Type)
		}
		r.calls.nodeError(correlationID, routedMsg.TargetNode, flowNode.Type, err)
		logger.Error().
			Err(err).
			Dur("duration", duration).
			Msg("Node execution failed after all retries")

		select {
		case r.errors <- NewNodeExecutionError(routedMsg.TargetNode, flowNode.Type, err):
		default:
			logger.Warn().Msg("Runtime error channel full; dropping error notification (failure already logged)")
		}
		return
	}

	logger.Debug().
		Int("outputCount", len(outputs)).
		Dur("duration", duration).
		Msg("Node execution completed")

	// Route output messages to connected nodes
	for _, output := range outputs {
		output.SourceNode = routedMsg.TargetNode

		// Auto-assign the source port only when the node has exactly one output.
		// For multi-output nodes (switch/filter with true/false ports) a missing
		// SourcePort is a bug: silently defaulting to Outputs[0] would route the
		// message down the wrong branch, so warn and skip instead of guessing.
		if output.SourcePort == "" {
			if len(flowNode.Outputs) == 1 {
				output.SourcePort = flowNode.Outputs[0].ID
			} else if len(flowNode.Outputs) > 1 {
				logger.Warn().
					Str("nodeType", flowNode.Type).
					Msg("Node produced an output with no SourcePort but declares multiple output ports; message not routed")
				continue
			}
		}

		// Outputs inherit the run's correlation ID when the node built a fresh
		// message, so the cascade stays one traceable run. An explicitly set ID is
		// respected, and the children are counted under whichever ID they carry.
		childCorr := correlationID
		if id, ok := output.GetMeta("correlationID"); ok {
			if s, ok := id.(string); ok && s != "" {
				childCorr = s
			}
		}
		output.SetMeta("correlationID", childCorr)

		// Count this output's downstream children into the run BEFORE routing them,
		// so the counter reflects the pending work before any child can finish.
		copies := r.targetCount(routedMsg.TargetNode, output.SourcePort)
		if !r.routeAllowed(output, copies) {
			continue
		}
		r.runAdd(childCorr, copies)
		r.router.Route(routedMsg.TargetNode, output)
	}
}

// executeNodeWithRetry executes a node with exponential backoff retry logic.
// It returns the outputs on success or the last error after all retries are exhausted.
// When slot is non-nil its concurrency slot is released for the duration of
// each backoff sleep (so a retrying node doesn't starve the rest of the flow)
// and re-acquired before the next attempt.
func (r *FlowRuntime) executeNodeWithRetry(nodeInstance node.Node, routedMsg RoutedMessage, flowNode *Node, logger zerolog.Logger, slot *semSlot) ([]*types.Message, error) {
	var lastErr error
	wait := r.retryConfig.InitialWait
	// A negative MaxRetries would skip the loop entirely and report success
	// without ever running the node; clamp so the node always runs once.
	maxRetries := max(r.retryConfig.MaxRetries, 0)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		outputs, err := nodeInstance.Process(r.ctx, routedMsg.Message)

		if err == nil {
			if attempt > 0 {
				logger.Info().
					Int("attempt", attempt).
					Msg("Node execution succeeded after retry")
			}
			return outputs, nil
		}

		lastErr = err

		// Don't retry if context is cancelled
		if r.ctx.Err() != nil {
			return nil, r.ctx.Err()
		}

		// Only transient errors are retried. Errors are permanent by default
		// (node.IsRetryable == false), so side-effecting actions (HTTP POST,
		// shell commands) are never blindly re-run — preventing duplicate side
		// effects — and permanent failures (4xx, bad config) fail fast.
		if !node.IsRetryable(err) {
			return nil, err
		}

		// Don't retry if we've exhausted all attempts
		if attempt >= maxRetries {
			break
		}

		// Record retry metric
		if !r.noMetrics {
			metrics.RecordNodeRetry(flowNode.Type)
		}

		logger.Warn().
			Err(err).
			Int("attempt", attempt).
			Int("maxRetries", maxRetries).
			Dur("nextWait", wait).
			Msg("Node execution failed, retrying")

		// Wait before retrying without holding a concurrency slot, then take
		// one again before the next attempt (both cancellation-aware).
		slot.release()
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-r.ctx.Done():
			timer.Stop()
			return nil, r.ctx.Err()
		}
		if !slot.acquire(r.ctx) {
			return nil, r.ctx.Err()
		}

		// Calculate next wait time with exponential backoff
		wait = time.Duration(float64(wait) * r.retryConfig.Multiplier)
		if wait > r.retryConfig.MaxWait {
			wait = r.retryConfig.MaxWait
		}
	}

	return nil, lastErr
}

// ensureCorrelationID ensures the message has a correlation ID for tracing.
// If one doesn't exist, it generates a new one.
func (r *FlowRuntime) ensureCorrelationID(msg *types.Message) string {
	if val, ok := msg.GetMeta("correlationID"); ok {
		if id, ok := val.(string); ok && id != "" {
			return id
		}
	}

	// Generate new correlation ID
	correlationID := uuid.New().String()
	msg.SetMeta("correlationID", correlationID)
	return correlationID
}

// targetCount returns how many downstream messages the router will deliver for a
// message leaving sourceNode on sourcePort — i.e. how many in-flight children to
// add. It mirrors MessageRouter routing: a specific port routes to that port's
// targets; an empty port routes from every output port of the node.
func (r *FlowRuntime) targetCount(sourceNode, sourcePort string) int {
	if sourcePort != "" {
		return r.router.CountTargets(sourceNode, sourcePort)
	}
	n := 0
	if def := r.nodeDefs[sourceNode]; def != nil {
		for _, p := range def.Outputs {
			n += r.router.CountTargets(sourceNode, p.ID)
		}
	}
	return n
}

// runAdd adds delta in-flight messages to a run's counter. Always called before
// the corresponding children are routed, so the counter can't dip to zero while
// work is still pending.
func (r *FlowRuntime) runAdd(correlationID string, delta int) {
	if delta <= 0 {
		return
	}
	r.runMu.Lock()
	r.runInflight[correlationID] += delta
	r.runMu.Unlock()
}

// runDone decrements a run's counter by one (a message finished). When it reaches
// zero the run has drained and a run-finished event is emitted exactly once.
func (r *FlowRuntime) runDone(correlationID string) {
	r.runMu.Lock()
	n := r.runInflight[correlationID] - 1
	finished := n <= 0
	if finished {
		delete(r.runInflight, correlationID)
	} else {
		r.runInflight[correlationID] = n
	}
	r.runMu.Unlock()

	if !finished {
		return
	}
	if r.observer != nil {
		r.observer(NodeEvent{
			FlowID:      r.flow.ID,
			CorrID:      correlationID,
			FlowVersion: r.version,
			Phase:       NodePhaseRunFinished,
		})
	}
	r.calls.runFinished(correlationID)
}
