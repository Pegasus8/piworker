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
	UpsertFlowRun(runID, flowID string, startedAt time.Time) error
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
	if !ok {
		if err := r.store.UpsertFlowRun(e.CorrID, e.FlowID, now); err != nil {
			r.log.Warn().Err(err).Msg("upsert flow run failed")
		}
		agg = &runAgg{}
		r.runs[e.CorrID] = agg
	}
	agg.lastSeen = now

	if e.Phase == flow.NodePhaseSuccess || e.Phase == flow.NodePhaseError {
		r.pending = append(r.pending, toRecord(e, now))
		agg.nodeCount++
		if e.Phase == flow.NodePhaseError {
			agg.errCount++
		}
	}
}

// flush writes the pending batch in one transaction.
func (r *Recorder) flush() {
	if len(r.pending) == 0 {
		return
	}
	if err := r.store.InsertNodeEvents(r.pending); err != nil {
		r.log.Warn().Err(err).Int("batch", len(r.pending)).Msg("insert node events failed")
	}
	r.pending = r.pending[:0]
}

// finalizeIdle finalizes runs with no activity within idleTimeout of asOf. The
// runtime has no per-trigger "done" signal, so a run is considered complete once
// it goes quiet — a pragmatic approximation.
func (r *Recorder) finalizeIdle(asOf time.Time) {
	for corr, agg := range r.runs {
		if asOf.Sub(agg.lastSeen) >= r.idleTimeout {
			r.finalize(corr, agg)
		}
	}
}

// finalizeAll finalizes every tracked run (used on shutdown).
func (r *Recorder) finalizeAll() {
	for corr, agg := range r.runs {
		r.finalize(corr, agg)
	}
}

func (r *Recorder) finalize(corr string, agg *runAgg) {
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
