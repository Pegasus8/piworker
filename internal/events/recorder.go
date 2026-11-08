package events

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
)

// HistoryStore is the subset of storage the recorder writes through. Defining it
// here (rather than importing the concrete *storage.SQLiteStore) keeps the
// recorder unit-testable with a fake and documents exactly what it touches.
type HistoryStore interface {
	// UpsertFlowRunWithActor records a run's start (idempotent) and who started
	// it; a later call fills an empty actor but never replaces one.
	UpsertFlowRunWithActor(runID, flowID, actor string, startedAt time.Time) error
	FinishFlowRun(runID, status string, finishedAt time.Time, nodeCount, errorCount int) error
	InsertNodeEvents(batch []storage.NodeEventRecord) error
	PruneHistory(olderThan time.Time, maxRunsPerFlow int) (int, error)
}

// Recorder bridges the flow runtime's observer hook to the observability layer.
// Observe() is the hot path called inline on node executor goroutines: it does
// only a non-blocking hub publish (for live SSE) and a non-blocking enqueue (for
// persistence), so it never stalls node execution. Run() is a single goroutine
// that owns ALL history writes — batching node events into one transaction and
// finalizing runs — so it never contends with itself under storage's
// single-connection pool.
type Recorder struct {
	store HistoryStore
	hub   *Hub
	in    chan flow.NodeEvent
	now   func() time.Time
	log   zerolog.Logger

	batchSize      int
	flushInterval  time.Duration
	idleTimeout    time.Duration
	staleTimeout   time.Duration
	pruneInterval  time.Duration
	retention      time.Duration
	maxRunsPerFlow int

	dropped atomic.Int64

	// Owned exclusively by the Run goroutine (and the deterministic unit tests).
	pending []storage.NodeEventRecord
	runs    map[string]*runAgg
}

type runAgg struct {
	lastSeen  time.Time
	nodeCount int
	errCount  int
	// actor is the run's "actor" meta as recorded so far ("" = not yet seen).
	actor string
	// running is how many node executions of the run have started but not yet
	// reported a terminal event. A quiet run with running > 0 is waiting on a
	// slow node (e.g. a long process-delay), not finished.
	running int
}

// RecorderOption configures a Recorder.
type RecorderOption func(*Recorder)

func WithHub(h *Hub) RecorderOption              { return func(r *Recorder) { r.hub = h } }
func WithLogger(l zerolog.Logger) RecorderOption { return func(r *Recorder) { r.log = l } }
func WithBatchSize(n int) RecorderOption {
	return func(r *Recorder) {
		if n > 0 {
			r.batchSize = n
		}
	}
}
func WithFlushInterval(d time.Duration) RecorderOption {
	return func(r *Recorder) {
		if d > 0 {
			r.flushInterval = d
		}
	}
}
func WithIdleTimeout(d time.Duration) RecorderOption {
	return func(r *Recorder) {
		if d > 0 {
			r.idleTimeout = d
		}
	}
}

// WithStaleTimeout sets how long a run with node executions still marked
// running may stay quiet before it is finalized anyway (a safety net for a
// terminal event lost to queue overflow).
func WithStaleTimeout(d time.Duration) RecorderOption {
	return func(r *Recorder) {
		if d > 0 {
			r.staleTimeout = d
		}
	}
}
func WithQueueSize(n int) RecorderOption {
	return func(r *Recorder) {
		if n > 0 {
			r.in = make(chan flow.NodeEvent, n)
		}
	}
}

// NewRecorder creates a Recorder with sensible defaults.
func NewRecorder(store HistoryStore, opts ...RecorderOption) *Recorder {
	r := &Recorder{
		store:          store,
		hub:            DefaultHub,
		in:             make(chan flow.NodeEvent, 4096),
		now:            time.Now,
		log:            zerolog.Nop(),
		batchSize:      200,
		flushInterval:  500 * time.Millisecond,
		idleTimeout:    30 * time.Second, // safety net; runs normally finalize on the run-finished signal
		staleTimeout:   time.Hour,        // last resort for runs whose node is still marked running
		pruneInterval:  time.Hour,
		retention:      7 * 24 * time.Hour,
		maxRunsPerFlow: 500,
		runs:           make(map[string]*runAgg),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Observe is the runtime observer hook. It publishes live (non-blocking) and
// enqueues for persistence (non-blocking, dropping on overflow). It must never
// block — observability is best-effort and must not throttle the flow runtime.
func (r *Recorder) Observe(e flow.NodeEvent) {
	r.publish(e)
	select {
	case r.in <- e:
	default:
		r.dropped.Add(1)
	}
}

// Dropped reports how many events were dropped due to a full queue.
func (r *Recorder) Dropped() int64 { return r.dropped.Load() }

// Run owns all history writes. One goroutine, batched transactions.
func (r *Recorder) Run(ctx context.Context) {
	flushT := time.NewTicker(r.flushInterval)
	idleT := time.NewTicker(r.idleTimeout)
	pruneT := time.NewTicker(r.pruneInterval)
	defer flushT.Stop()
	defer idleT.Stop()
	defer pruneT.Stop()

	for {
		select {
		case e := <-r.in:
			r.persist(e, r.now())
			if len(r.pending) >= r.batchSize {
				r.flush()
			}
		case <-flushT.C:
			r.flush()
		case <-idleT.C:
			r.finalizeIdle(r.now())
		case <-pruneT.C:
			if _, err := r.store.PruneHistory(r.now().Add(-r.retention), r.maxRunsPerFlow); err != nil {
				r.log.Warn().Err(err).Msg("history prune failed")
			}
		case <-ctx.Done():
			r.drain()
			r.flush()
			r.finalizeAll()
			return
		}
	}
}

// drain non-blockingly pulls everything still queued into the pending batch.
func (r *Recorder) drain() {
	for {
		select {
		case e := <-r.in:
			r.persist(e, r.now())
		default:
			return
		}
	}
}

// persist updates per-run aggregates and appends terminal events to the pending
// batch. Running events only seed the run record; terminal events carry timing
// and are what history stores. Called only by Run (and deterministic tests).
func (r *Recorder) persist(e flow.NodeEvent, now time.Time) {
	if e.CorrID == "" {
		return
	}

	// A run-finished signal finalizes the run deterministically. It arrives after
	// all of the run's node events (the runtime emits it only once in-flight work
	// drains), so the aggregate is complete here.
	if e.Phase == flow.NodePhaseRunFinished {
		if agg, ok := r.runs[e.CorrID]; ok {
			r.finalize(e.CorrID, agg)
		}
		return
	}

	agg, ok := r.runs[e.CorrID]
	actor := runActor(e)
	// Upsert on the first event of a run, and again only to fill in an actor
	// the first event did not carry.
	if !ok || (agg.actor == "" && actor != "") {
		if err := r.store.UpsertFlowRunWithActor(e.CorrID, e.FlowID, actor, now); err != nil {
			r.log.Warn().Err(err).Msg("upsert flow run failed")
		}
		if !ok {
			agg = &runAgg{}
			r.runs[e.CorrID] = agg
		}
		agg.actor = actor
	}
	agg.lastSeen = now

	switch e.Phase {
	case flow.NodePhaseRunning:
		agg.running++
	case flow.NodePhaseSuccess, flow.NodePhaseError:
		r.pending = append(r.pending, toRecord(e, now))
		agg.nodeCount++
		if e.Phase == flow.NodePhaseError {
			agg.errCount++
		}
		if agg.running > 0 {
			agg.running--
		}
	}
}

// runActor returns the "actor" meta of the event's input message (set by
// FlowRuntime.Call on the run's first message and inherited by its clones).
func runActor(e flow.NodeEvent) string {
	if e.Input == nil {
		return ""
	}
	v, _ := e.Input.GetMeta("actor")
	actor, _ := v.(string)
	return actor
}

// flush writes the pending batch in one transaction.
func (r *Recorder) flush() bool {
	if len(r.pending) == 0 {
		return true
	}
	if err := r.store.InsertNodeEvents(r.pending); err != nil {
		r.log.Warn().Err(err).Int("batch", len(r.pending)).Msg("insert node events failed")
		return false
	}
	r.pending = r.pending[:0]
	return true
}

// finalizeIdle finalizes runs with no activity within idleTimeout of asOf. It
// is a safety net behind the runtime's run-finished signal (e.g. a signal lost
// to queue overflow). A run that still has a node executing is not idle, merely
// slow, so it is only finalized once quiet for staleTimeout.
func (r *Recorder) finalizeIdle(asOf time.Time) {
	for corr, agg := range r.runs {
		quiet := asOf.Sub(agg.lastSeen)
		if quiet < r.idleTimeout {
			continue
		}
		if agg.running > 0 && quiet < r.staleTimeout {
			continue
		}
		r.finalize(corr, agg)
	}
}

// finalizeAll finalizes every tracked run (used on shutdown).
func (r *Recorder) finalizeAll() {
	for corr, agg := range r.runs {
		r.finalize(corr, agg)
	}
}

func (r *Recorder) finalize(corr string, agg *runAgg) {
	// Completion is visible only once its node details have been persisted.
	if !r.flush() {
		return
	}
	status := flow.NodePhaseSuccess
	if agg.errCount > 0 {
		status = flow.NodePhaseError
	}
	if err := r.store.FinishFlowRun(corr, status, agg.lastSeen, agg.nodeCount, agg.errCount); err != nil {
		r.log.Warn().Err(err).Msg("finish flow run failed")
	}
	delete(r.runs, corr)
}

// publish sends the live event to the hub and, for process-debug nodes, a derived
// debug event carrying a payload/meta snapshot so the editor's debug inspector
// can show what flowed through — without changing the debug node itself.
func (r *Recorder) publish(e flow.NodeEvent) {
	// run-finished is a run-level event (no node) for the persistence path only;
	// the live node-state stream doesn't need it.
	if e.Phase == flow.NodePhaseRunFinished {
		return
	}
	// Skip building events nobody is watching — the common case in production is
	// no editor open, hence no SSE subscriber for the flow.
	if r.hub == nil || !r.hub.HasSubscribers(e.FlowID) {
		return
	}
	now := r.now()
	r.hub.Publish(toHubEvent(e, now))

	if e.NodeType == "process-debug" && e.Phase == flow.NodePhaseSuccess {
		snap := debugSnapshot(e)
		if snap != nil {
			r.hub.Publish(Event{
				FlowID: e.FlowID, NodeID: e.NodeID, NodeType: e.NodeType,
				Phase: PhaseDebug, CorrID: e.CorrID, Debug: snap, Ts: now.UnixMilli(),
			})
		}
	}
}

func toHubEvent(e flow.NodeEvent, now time.Time) Event {
	ev := Event{
		FlowID: e.FlowID, NodeID: e.NodeID, NodeType: e.NodeType,
		Phase: e.Phase, CorrID: e.CorrID,
		DurMs: durMs(e.Duration), Ts: now.UnixMilli(),
	}
	if e.Input != nil {
		ev.MsgID = e.Input.ID
	}
	if e.Err != nil {
		ev.Error = e.Err.Error()
	}
	return ev
}

func toRecord(e flow.NodeEvent, now time.Time) storage.NodeEventRecord {
	rec := storage.NodeEventRecord{
		RunID: e.CorrID, FlowID: e.FlowID, NodeID: e.NodeID, NodeType: e.NodeType,
		Phase: e.Phase, DurationMs: durMs(e.Duration), CreatedAt: now,
	}
	if e.Input != nil {
		rec.MsgID = e.Input.ID
	}
	if e.Err != nil {
		rec.Error = e.Err.Error()
	}
	return rec
}

// debugSnapshot builds a payload/meta snapshot from a debug node's output (or
// input when it doesn't pass through).
func debugSnapshot(e flow.NodeEvent) map[string]interface{} {
	var msg *types.Message
	if len(e.Outputs) > 0 {
		msg = e.Outputs[0]
	} else {
		msg = e.Input
	}
	if msg == nil {
		return nil
	}
	return map[string]interface{}{
		"payload": msg.Payload,
		"meta":    msg.Meta,
		"topic":   msg.Topic,
	}
}

func durMs(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}
