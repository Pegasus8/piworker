package flow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/google/uuid"
)

// CallStatus is the outcome of a synchronous call (FlowRuntime.Call / Wait).
type CallStatus string

// Call statuses. approval_required and rejected are produced by the service
// layer (confirm: admin), never by the runtime itself.
const (
	CallStatusCompleted        CallStatus = "completed"
	CallStatusCompletedNoReply CallStatus = "completed_no_reply"
	CallStatusError            CallStatus = "error"
	CallStatusRunning          CallStatus = "running"
	CallStatusExpired          CallStatus = "expired"
	CallStatusApprovalRequired CallStatus = "approval_required"
	CallStatusRejected         CallStatus = "rejected"
)

// Defaults for synchronous calls.
const (
	// DefaultCallTimeout is used when CallRequest.Timeout is not positive.
	DefaultCallTimeout = 10 * time.Second
	// DefaultCallExpiryGrace is added to a call's timeout to get the hard
	// lifetime of its waiter: once it passes, an unfinished call reports
	// "expired" and its result is forgotten.
	DefaultCallExpiryGrace = 10 * time.Minute
	// DefaultMaxCallWaiters caps how many called runs a runtime tracks at once
	// (runs still executing plus results not collected yet).
	DefaultMaxCallWaiters = 64
	// DefaultMaxCallResultBytes caps the serialized payload of a reply. It is
	// well above what the service layer passes on to agents (max_result_bytes),
	// so the service can still truncate and flag; it bounds what a runtime
	// holds: DefaultMaxCallWaiters * DefaultMaxCallResultBytes = 16 MiB.
	DefaultMaxCallResultBytes = 256 << 10
)

// Errors returned by Call before a run is started.
var (
	// ErrBusy is wrapped by every refusal that means "busy, retry later"
	// (ErrTooManyCalls, ErrTriggerBusy): callers map them with one errors.Is.
	ErrBusy = errors.New("busy")
	// ErrTriggerNotCallable: the trigger does not implement node.Emitter.
	ErrTriggerNotCallable = errors.New("trigger cannot be called synchronously")
	// ErrNoActiveTargets: the trigger has no connection to an enabled node, so
	// a run started from it could never do anything (or finish).
	ErrNoActiveTargets = errors.New("trigger has no active downstream connection")
	// ErrTooManyCalls (wraps ErrBusy): the runtime already tracks its maximum
	// of called runs and none of them is a finished result that could make
	// room.
	ErrTooManyCalls error = &busyError{"too many calls in progress"}
	// ErrTriggerBusy (wraps ErrBusy): the trigger did not accept the call
	// within its timeout (its output stayed full).
	ErrTriggerBusy error = &busyError{"trigger is busy"}
)

// busyError is a "busy, retry later" refusal: its message is its own, and it
// wraps ErrBusy.
type busyError struct{ msg string }

func (e *busyError) Error() string { return e.msg }
func (e *busyError) Unwrap() error { return ErrBusy }

// flowStoppedMessage is the CallError message of calls cancelled by Stop.
const flowStoppedMessage = "flow stopped"

// CallResult is the result of a synchronous call.
type CallResult struct {
	Status     CallStatus      `json:"status"`
	RunID      string          `json:"runId"`
	Result     json.RawMessage `json:"result,omitempty"` // deep-copied, serialized
	Error      *CallError      `json:"error,omitempty"`
	DurationMs int64           `json:"durationMs"`
}

// CallError describes the first node error of a called run (or why the call
// was cancelled). Messages are raw node errors: callers redact them before
// showing them to agents.
type CallError struct {
	NodeID   string `json:"nodeId,omitempty"`
	NodeType string `json:"nodeType,omitempty"`
	Message  string `json:"message"`
}

// CallRequest starts a run on a callable trigger (see node.Emitter).
type CallRequest struct {
	TriggerNodeID string
	Args          map[string]any // already validated
	Actor         string         // "token:<name>", "editor", …
	Timeout       time.Duration
}

// Replier delivers the reply of a called run. Reply reports whether the
// payload became the call's result: only the first reply of a run that is
// still waiting wins; replies for unknown or finished correlation IDs (e.g. a
// run not started by Call) return false. The payload is serialized inside the
// call, so the caller may keep mutating it afterwards. A payload that cannot
// be serialized, or whose JSON exceeds the runtime's maximum result size, is
// not a result: Reply returns false and, unless a later reply wins, the call
// ends as an error explaining why.
type Replier interface {
	Reply(correlationID string, payload any, isError bool) bool
}

// Call starts a run on a callable trigger (one implementing node.Emitter, such
// as trigger-tool) and waits for its reply, the end of the run, the timeout, or
// ctx cancellation (whichever first). On timeout it returns Status "running";
// the run continues and Wait can be used later. On ctx cancellation it returns
// the same "running" result together with ctx.Err().
//
// The timeout counts from the start of Call and covers handing the message to
// the trigger: when the trigger does not accept it in time, no run is started
// and Call fails with ErrTriggerBusy.
//
// A message of the run that is dropped before it executes (full node queue,
// unknown target, routing timeout) is recorded like a node error ("message
// dropped at node X: …"), so the call reports an error unless a reply won.
//
// A final result returned by Call is handed over to the caller: the runtime
// forgets it as soon as the run is over (RunDone closed), so a later Wait for
// that run reports "expired". Only results Call could not return (status
// "running") stay retrievable with Wait, until the waiter's hard lifetime ends.
//
// The run ID is a server-generated UUID set as the run's correlationID, with
// req.Actor as its "actor" meta. The payload is a deep copy of req.Args (an
// empty object when nil). An error without a run ID means no run was started.
func (r *FlowRuntime) Call(ctx context.Context, req CallRequest) (CallResult, error) {
	if !r.IsRunning() {
		return CallResult{}, ErrFlowNotRunning
	}
	trigger, ok := r.triggers[req.TriggerNodeID]
	if !ok {
		return CallResult{}, fmt.Errorf("%w: %s is not an enabled trigger of this flow", ErrNodeNotFound, req.TriggerNodeID)
	}
	emitter, ok := trigger.(node.Emitter)
	if !ok {
		return CallResult{}, fmt.Errorf("%w: node %s", ErrTriggerNotCallable, req.TriggerNodeID)
	}
	if r.ActiveTargets(req.TriggerNodeID) == 0 {
		return CallResult{}, fmt.Errorf("%w: node %s", ErrNoActiveTargets, req.TriggerNodeID)
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = DefaultCallTimeout
	}

	runID := uuid.New().String()
	w, err := r.calls.register(runID, timeout)
	if err != nil {
		return CallResult{}, err
	}

	args := req.Args
	if args == nil {
		args = map[string]any{}
	}
	// Clone so the run owns its payload and the caller keeps req.Args.
	msg := types.NewMessage(args, types.DataTypeObject).Clone()
	msg.SetMeta("correlationID", runID)
	if req.Actor != "" {
		msg.SetMeta("actor", req.Actor)
	}
	// The timeout covers the hand-off too: a trigger whose output stays full
	// must not hold the caller past it.
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	emitCtx, cancelEmit := context.WithTimeout(ctx, timeout)
	err = emitter.Emit(emitCtx, msg)
	handOffExpired := ctx.Err() == nil && errors.Is(emitCtx.Err(), context.DeadlineExceeded)
	cancelEmit()
	if err != nil {
		r.calls.remove(runID)
		if handOffExpired {
			return CallResult{}, fmt.Errorf("%w: %s did not accept the call within %s", ErrTriggerBusy, req.TriggerNodeID, timeout)
		}
		return CallResult{}, fmt.Errorf("start run on %s: %w", req.TriggerNodeID, err)
	}

	var interrupted error
	select {
	case <-w.done:
	case <-timer.C:
	case <-ctx.Done():
		interrupted = ctx.Err()
	}
	return r.calls.deliver(w, interrupted)
}

// Wait returns the final result of a run started by Call, blocking until it is
// final or ctx ends (then: the "running" result and ctx.Err()). It can be
// repeated until the waiter's hard lifetime ends. A run that is unknown, whose
// waiter expired, or whose result Call already returned reports "expired".
func (r *FlowRuntime) Wait(ctx context.Context, runID string) (CallResult, error) {
	w := r.calls.get(runID)
	if w == nil {
		return CallResult{Status: CallStatusExpired, RunID: runID}, nil
	}
	select {
	case <-w.done:
		return r.calls.snapshot(w), nil
	case <-ctx.Done():
		return r.calls.snapshotOrErr(w, ctx.Err())
	}
}

// RunDone returns a channel closed when the run identified by runID finishes
// (all of its in-flight work drained). It is also closed when the flow stops
// or the run's waiter expires, so a stuck run cannot hold a caller's resources
// forever, and it is already closed for runs that are not tracked.
func (r *FlowRuntime) RunDone(runID string) <-chan struct{} {
	return r.calls.runDoneChan(runID)
}

// ActiveTargets returns how many enabled nodes a message leaving nodeID (on
// any of its output ports) is routed to. Connections to disabled nodes do not
// count. Unknown or disabled nodes have none.
func (r *FlowRuntime) ActiveTargets(nodeID string) int {
	if _, ok := r.nodes[nodeID]; !ok {
		return 0
	}
	return r.targetCount(nodeID, "")
}

// ValidateCallTriggers fails when a callable trigger (node.Emitter) has no
// active downstream connection: calls to it could never do anything. Deploy
// runs it; the error is a *ValidationError.
func (r *FlowRuntime) ValidateCallTriggers() error {
	ids := make([]string, 0, len(r.triggers))
	for id, trigger := range r.triggers {
		if _, ok := trigger.(node.Emitter); ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		if r.ActiveTargets(id) > 0 {
			continue
		}
		nodeType := ""
		if def := r.nodeDefs[id]; def != nil {
			nodeType = def.Type
		}
		return NewValidationError("callable trigger %s (%s) has no active connection: connect it to at least one enabled node", id, nodeType)
	}
	return nil
}

type callRegistryKey struct{}

// withCallRegistry stores the runtime's waiter registry on ctx, the way
// flowvars.WithFlowStore stores the flow variables.
func withCallRegistry(ctx context.Context, reg *callRegistry) context.Context {
	return context.WithValue(ctx, callRegistryKey{}, reg)
}

// ReplyFromContext returns the Replier of the flow runtime that owns ctx (the
// context nodes receive in Process). It is never nil: outside a runtime it
// returns a Replier whose Reply always reports false.
func ReplyFromContext(ctx context.Context) Replier {
	if ctx != nil {
		if reg, ok := ctx.Value(callRegistryKey{}).(*callRegistry); ok && reg != nil {
			return reg
		}
	}
	return noReplier{}
}

type noReplier struct{}

func (noReplier) Reply(string, any, bool) bool { return false }

// closedChan is returned by RunDone for runs that are not (or no longer)
// tracked.
var closedChan = func() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}()

// callWaiter tracks one called run. Every field after the channels is guarded
// by the registry mutex.
type callWaiter struct {
	runID string
	start time.Time
	// deadline is the end of the waiter's hard lifetime (timeout + grace).
	deadline time.Time
	// done is closed once result is final (reply, run end, stop or expiry).
	done chan struct{}
	// runDone is closed once the run no longer executes for the caller: it
	// finished, the flow stopped, or the waiter expired.
	runDone chan struct{}

	final       bool
	runFinished bool
	// delivered: Call returned the final result to its caller, so nothing
	// needs it once the run is over.
	delivered bool
	result    CallResult
	nodeErr   *CallError
	expiry    *time.Timer
}

// callRegistry is the per-runtime set of waiters keyed by correlation ID.
type callRegistry struct {
	mu      sync.Mutex
	waiters map[string]*callWaiter
	stopped bool
	grace   time.Duration
	now     func() time.Time
	// maxWaiters caps len(waiters) and maxResultBytes the serialized reply
	// payload; <= 0 means no cap. Both are set before start.
	maxWaiters     int
	maxResultBytes int
}

func newCallRegistry(grace time.Duration) *callRegistry {
	return &callRegistry{
		waiters:        make(map[string]*callWaiter),
		stopped:        true,
		grace:          grace,
		now:            time.Now,
		maxWaiters:     DefaultMaxCallWaiters,
		maxResultBytes: DefaultMaxCallResultBytes,
	}
}

// start accepts new calls (the runtime is running).
func (c *callRegistry) start() {
	c.mu.Lock()
	c.stopped = false
	c.mu.Unlock()
}

// register creates the waiter of a run before it is started. Its hard
// lifetime is timeout + grace. At maxWaiters, the uncollected result closest
// to its expiry is evicted to make room; when every waiter belongs to a run
// that still executes, register fails with ErrTooManyCalls.
func (c *callRegistry) register(runID string, timeout time.Duration) (*callWaiter, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return nil, ErrFlowNotRunning
	}
	if _, dup := c.waiters[runID]; dup {
		return nil, fmt.Errorf("run %s is already registered", runID)
	}
	if c.maxWaiters > 0 && len(c.waiters) >= c.maxWaiters && !c.evictFinished() {
		return nil, fmt.Errorf("%w: limit of %d reached", ErrTooManyCalls, c.maxWaiters)
	}
	now := c.now()
	w := &callWaiter{
		runID:    runID,
		start:    now,
		deadline: now.Add(timeout + c.grace),
		done:     make(chan struct{}),
		runDone:  make(chan struct{}),
	}
	w.expiry = time.AfterFunc(timeout+c.grace, func() { c.expire(w) })
	c.waiters[runID] = w
	return w, nil
}

// remove forgets a waiter whose run could not be started.
func (c *callRegistry) remove(runID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.waiters[runID]
	if w == nil {
		return
	}
	c.closeRun(w)
	c.forget(w)
}

// get returns the waiter of runID, or nil.
func (c *callRegistry) get(runID string) *callWaiter {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.waiters[runID]
}

// runDoneChan returns the channel closed when runID finishes; already closed
// for runs that are not tracked.
func (c *callRegistry) runDoneChan(runID string) <-chan struct{} {
	if w := c.get(runID); w != nil {
		return w.runDone
	}
	return closedChan
}

// waiting reports whether correlationID is a call still waiting for its result.
func (c *callRegistry) waiting(correlationID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.waiters[correlationID]
	return w != nil && !w.final
}

// Reply implements Replier.
func (c *callRegistry) Reply(correlationID string, payload any, isError bool) bool {
	// Most runs are not calls: only serialize for a call still waiting.
	if !c.waiting(correlationID) {
		return false
	}
	// Serialize outside the lock; this is the deep copy of the payload. The
	// waiter is checked again below, another reply may have won meanwhile.
	data, err := json.Marshal(payload)
	failure := ""
	switch {
	case err != nil:
		failure = fmt.Sprintf("reply payload is not serializable: %v", err)
	case c.maxResultBytes > 0 && len(data) > c.maxResultBytes:
		failure = fmt.Sprintf("reply payload is too large: %d bytes (limit %d)", len(data), c.maxResultBytes)
		data = nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.waiters[correlationID]
	if w == nil || w.final {
		return false
	}
	if failure != "" {
		if w.nodeErr == nil {
			w.nodeErr = &CallError{Message: failure}
		}
		return false
	}
	status := CallStatusCompleted
	if isError {
		status = CallStatusError
	}
	c.finalize(w, CallResult{Status: status, Result: data})
	return true
}

// nodeError records the first node error of a called run.
func (c *callRegistry) nodeError(correlationID, nodeID, nodeType string, err error) {
	if err == nil {
		return
	}
	c.recordError(correlationID, nodeID, nodeType, err.Error)
}

// dropped records, as the run's error, that one of its messages was dropped
// before nodeID could execute it (full queue, unknown target, routing
// timeout): the call reports error, not a silent completed_no_reply, unless a
// reply already won or an earlier error was recorded.
func (c *callRegistry) dropped(correlationID, nodeID, nodeType, reason string) {
	c.recordError(correlationID, nodeID, nodeType, func() string {
		return fmt.Sprintf("message dropped at node %s: %s", nodeID, reason)
	})
}

// recordError keeps the first error of a call still waiting; message is only
// built for such a call.
func (c *callRegistry) recordError(correlationID, nodeID, nodeType string, message func() string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.waiters[correlationID]
	if w == nil || w.final || w.nodeErr != nil {
		return
	}
	w.nodeErr = &CallError{NodeID: nodeID, NodeType: nodeType, Message: message()}
}

// runFinished is called once a run's in-flight work drained. A call still
// waiting completes without reply (or with its first node error). A result
// Call already returned is forgotten.
func (c *callRegistry) runFinished(correlationID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.waiters[correlationID]
	if w == nil {
		return
	}
	if !w.final {
		if w.nodeErr != nil {
			c.finalize(w, CallResult{Status: CallStatusError, Error: w.nodeErr})
		} else {
			c.finalize(w, CallResult{Status: CallStatusCompletedNoReply})
		}
	}
	c.closeRun(w)
	if w.delivered {
		c.forget(w)
	}
}

// stop cancels every waiting call ("flow stopped"), refuses new ones and
// forgets every waiter: nothing can reach a stopped runtime's calls any more.
// Callers already blocked in Call or Wait get the final result they hold.
func (c *callRegistry) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopped = true
	for _, w := range c.waiters {
		if !w.final {
			c.finalize(w, CallResult{Status: CallStatusError, Error: &CallError{Message: flowStoppedMessage}})
		}
		c.closeRun(w)
		c.forget(w)
	}
}

// expire ends a waiter's hard lifetime: an unfinished call reports expired,
// and the waiter is forgotten.
func (c *callRegistry) expire(w *callWaiter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.waiters[w.runID] != w {
		return // already forgotten
	}
	if !w.final {
		c.finalize(w, CallResult{Status: CallStatusExpired})
	}
	c.closeRun(w)
	c.forget(w)
}

// snapshot returns the waiter's current result: the final one, or "running".
// The returned Result is a private copy.
func (c *callRegistry) snapshot(w *callWaiter) CallResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !w.final {
		return CallResult{
			Status:     CallStatusRunning,
			RunID:      w.runID,
			DurationMs: c.now().Sub(w.start).Milliseconds(),
		}
	}
	res := w.result
	res.Result = bytes.Clone(res.Result)
	if res.Error != nil {
		e := *res.Error
		res.Error = &e
	}
	return res
}

// snapshotOrErr is snapshot for a wait interrupted by err (nil when it was
// not): a result that is already final wins over the interruption.
func (c *callRegistry) snapshotOrErr(w *callWaiter, err error) (CallResult, error) {
	res := c.snapshot(w)
	if res.Status == CallStatusRunning {
		return res, err
	}
	return res, nil
}

// deliver is snapshotOrErr for Call, which hands a final result over to its
// caller: the waiter is then only kept while its run still executes (for
// RunDone), and forgotten at once when the run is already over. A "running"
// result is not delivered; the waiter stays for Wait.
func (c *callRegistry) deliver(w *callWaiter, interrupted error) (CallResult, error) {
	res, err := c.snapshotOrErr(w, interrupted)
	if res.Status == CallStatusRunning {
		return res, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	w.delivered = true
	if w.runFinished {
		c.forget(w)
	}
	return res, err
}

// count returns how many waiters the registry holds.
func (c *callRegistry) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

// evictFinished forgets the waiter whose run is over and whose deadline is the
// nearest: a result nobody collected yet (results Call returned are already
// gone). Waiters of runs that still execute are never evicted, so RunDone stays
// accurate. It reports whether one was evicted. Callers hold c.mu.
func (c *callRegistry) evictFinished() bool {
	var victim *callWaiter
	for _, w := range c.waiters {
		if w.runFinished && (victim == nil || w.deadline.Before(victim.deadline)) {
			victim = w
		}
	}
	if victim == nil {
		return false
	}
	c.forget(victim)
	return true
}

// forget drops a waiter and stops its expiry timer. Callers hold c.mu.
func (c *callRegistry) forget(w *callWaiter) {
	w.expiry.Stop()
	if c.waiters[w.runID] == w {
		delete(c.waiters, w.runID)
	}
}

// finalize sets the final result. Callers hold c.mu and check !w.final.
func (c *callRegistry) finalize(w *callWaiter, res CallResult) {
	res.RunID = w.runID
	res.DurationMs = c.now().Sub(w.start).Milliseconds()
	w.result = res
	w.final = true
	close(w.done)
}

// closeRun closes runDone once. Callers hold c.mu.
func (c *callRegistry) closeRun(w *callWaiter) {
	if !w.runFinished {
		w.runFinished = true
		close(w.runDone)
	}
}
