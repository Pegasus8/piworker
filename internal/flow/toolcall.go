package flow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
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
)

// Errors returned by Call before a run is started.
var (
	// ErrTriggerNotCallable: the trigger does not implement node.Emitter.
	ErrTriggerNotCallable = errors.New("trigger cannot be called synchronously")
	// ErrNoActiveTargets: the trigger has no connection to an enabled node, so
	// a run started from it could never do anything (or finish).
	ErrNoActiveTargets = errors.New("trigger has no active downstream connection")
)

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
// call, so the caller may keep mutating it afterwards.
type Replier interface {
	Reply(correlationID string, payload any, isError bool) bool
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
	// done is closed once result is final (reply, run end, stop or expiry).
	done chan struct{}
	// runDone is closed once the run no longer executes for the caller: it
	// finished, the flow stopped, or the waiter expired.
	runDone chan struct{}

	final       bool
	runFinished bool
	result      CallResult
	nodeErr     *CallError
	expiry      *time.Timer
}

// callRegistry is the per-runtime set of waiters keyed by correlation ID.
type callRegistry struct {
	mu      sync.Mutex
	waiters map[string]*callWaiter
	stopped bool
	grace   time.Duration
	now     func() time.Time
}

func newCallRegistry(grace time.Duration) *callRegistry {
	return &callRegistry{
		waiters: make(map[string]*callWaiter),
		stopped: true,
		grace:   grace,
		now:     time.Now,
	}
}

// start accepts new calls (the runtime is running).
func (c *callRegistry) start() {
	c.mu.Lock()
	c.stopped = false
	c.mu.Unlock()
}

// register creates the waiter of a run before it is started. Its hard
// lifetime is timeout + grace.
func (c *callRegistry) register(runID string, timeout time.Duration) (*callWaiter, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return nil, ErrFlowNotRunning
	}
	if _, dup := c.waiters[runID]; dup {
		return nil, fmt.Errorf("run %s is already registered", runID)
	}
	w := &callWaiter{
		runID:   runID,
		start:   c.now(),
		done:    make(chan struct{}),
		runDone: make(chan struct{}),
	}
	w.expiry = time.AfterFunc(timeout+c.grace, func() { c.expire(runID) })
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
	w.expiry.Stop()
	c.closeRun(w)
	delete(c.waiters, runID)
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

// Reply implements Replier.
func (c *callRegistry) Reply(correlationID string, payload any, isError bool) bool {
	// Serialize outside the lock; this is the deep copy of the payload.
	data, err := json.Marshal(payload)

	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.waiters[correlationID]
	if w == nil || w.final {
		return false
	}
	if err != nil {
		if w.nodeErr == nil {
			w.nodeErr = &CallError{Message: fmt.Sprintf("reply payload is not serializable: %v", err)}
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
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.waiters[correlationID]
	if w == nil || w.final || w.nodeErr != nil {
		return
	}
	w.nodeErr = &CallError{NodeID: nodeID, NodeType: nodeType, Message: err.Error()}
}

// runFinished is called once a run's in-flight work drained. A call still
// waiting completes without reply (or with its first node error).
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
}

// stop cancels every waiting call ("flow stopped") and refuses new ones.
// Finished results stay retrievable until their waiters expire.
func (c *callRegistry) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopped = true
	for _, w := range c.waiters {
		if !w.final {
			c.finalize(w, CallResult{Status: CallStatusError, Error: &CallError{Message: flowStoppedMessage}})
		}
		c.closeRun(w)
	}
}

// expire ends a waiter's hard lifetime: an unfinished call reports expired,
// and the waiter is forgotten.
func (c *callRegistry) expire(runID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.waiters[runID]
	if w == nil {
		return
	}
	if !w.final {
		c.finalize(w, CallResult{Status: CallStatusExpired})
	}
	c.closeRun(w)
	delete(c.waiters, runID)
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
