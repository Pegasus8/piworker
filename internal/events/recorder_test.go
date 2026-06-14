package events

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStore is an in-memory HistoryStore for deterministic recorder tests.
type fakeStore struct {
	mu         sync.Mutex
	runs       map[string]storage.FlowRun
	events     []storage.NodeEventRecord
	pruneCalls int
}

func newFakeStore() *fakeStore { return &fakeStore{runs: map[string]storage.FlowRun{}} }

func (f *fakeStore) UpsertFlowRun(runID, flowID string, startedAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.runs[runID]; !ok {
		f.runs[runID] = storage.FlowRun{ID: runID, FlowID: flowID, StartedAt: startedAt, Status: "running"}
	}
	return nil
}

func (f *fakeStore) FinishFlowRun(runID, status string, finishedAt time.Time, nodeCount, errorCount int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.runs[runID]
	r.Status = status
	r.FinishedAt = &finishedAt
	r.NodeCount = nodeCount
	r.ErrorCount = errorCount
	f.runs[runID] = r
	return nil
}

func (f *fakeStore) InsertNodeEvents(batch []storage.NodeEventRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, batch...)
	return nil
}

func (f *fakeStore) PruneHistory(olderThan time.Time, maxRunsPerFlow int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pruneCalls++
	return 0, nil
}

func (f *fakeStore) snapshotEvents() []storage.NodeEventRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]storage.NodeEventRecord, len(f.events))
	copy(out, f.events)
	return out
}

func (f *fakeStore) getRun(id string) storage.FlowRun {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs[id]
}

func runningEvent(corr, flowID, nodeID, nodeType string) flow.NodeEvent {
	return flow.NodeEvent{FlowID: flowID, NodeID: nodeID, NodeType: nodeType, CorrID: corr, Phase: flow.NodePhaseRunning, Input: types.NewMessage("in", types.DataTypeString)}
}

func successEvent(corr, flowID, nodeID, nodeType string) flow.NodeEvent {
	e := runningEvent(corr, flowID, nodeID, nodeType)
	e.Phase = flow.NodePhaseSuccess
	e.Duration = 2 * time.Millisecond
	e.Outputs = []*types.Message{types.NewMessage("out", types.DataTypeString)}
	return e
}

func errorEvent(corr, flowID, nodeID, nodeType string) flow.NodeEvent {
	e := runningEvent(corr, flowID, nodeID, nodeType)
	e.Phase = flow.NodePhaseError
	e.Err = errors.New("boom")
	return e
}

func TestRecorderPersistsTerminalEventsOnly(t *testing.T) {
	store := newFakeStore()
	rec := NewRecorder(store, WithHub(NewHub()))

	rec.persist(runningEvent("r1", "f1", "n1", "x"), time.Now())
	rec.persist(successEvent("r1", "f1", "n1", "x"), time.Now())
	rec.persist(errorEvent("r1", "f1", "n2", "y"), time.Now())
	rec.flush()

	events := store.snapshotEvents()
	require.Len(t, events, 2, "only terminal (success/error) events are persisted")
	assert.Equal(t, flow.NodePhaseSuccess, events[0].Phase)
	assert.Equal(t, 2.0, events[0].DurationMs)
	assert.Equal(t, flow.NodePhaseError, events[1].Phase)
	assert.Equal(t, "boom", events[1].Error)
}

func TestRecorderUpsertsRunOnFirstEvent(t *testing.T) {
	store := newFakeStore()
	rec := NewRecorder(store, WithHub(NewHub()))

	rec.persist(runningEvent("r1", "f1", "n1", "x"), time.Now())

	run := store.getRun("r1")
	assert.Equal(t, "r1", run.ID)
	assert.Equal(t, "f1", run.FlowID)
	assert.Equal(t, "running", run.Status)
}

func TestRecorderFinalizesIdleRuns(t *testing.T) {
	store := newFakeStore()
	rec := NewRecorder(store, WithHub(NewHub()), WithIdleTimeout(5*time.Second))

	base := time.Now()
	rec.persist(successEvent("ok", "f1", "n1", "x"), base)
	rec.persist(errorEvent("bad", "f1", "n1", "x"), base)

	// Not yet idle.
	rec.finalizeIdle(base.Add(time.Second))
	assert.Equal(t, "running", store.getRun("ok").Status)

	// Past the idle timeout: both runs finalize with status by error count.
	rec.finalizeIdle(base.Add(10 * time.Second))
	assert.Equal(t, "success", store.getRun("ok").Status)
	assert.Equal(t, "error", store.getRun("bad").Status)
	assert.Equal(t, 1, store.getRun("bad").ErrorCount)
	require.NotNil(t, store.getRun("ok").FinishedAt)
}

func TestRecorderFinalizesOnRunFinished(t *testing.T) {
	store := newFakeStore()
	// A huge idle timeout proves finalization comes from the signal, not the sweep.
	rec := NewRecorder(store, WithHub(NewHub()), WithIdleTimeout(time.Hour))

	now := time.Now()
	rec.persist(successEvent("r1", "f1", "n1", "x"), now)
	rec.persist(successEvent("r1", "f1", "n2", "y"), now)
	assert.Equal(t, "running", store.getRun("r1").Status, "not finalized until the signal")

	rec.persist(flow.NodeEvent{FlowID: "f1", CorrID: "r1", Phase: flow.NodePhaseRunFinished}, now)

	run := store.getRun("r1")
	assert.Equal(t, "success", run.Status)
	assert.Equal(t, 2, run.NodeCount)
	require.NotNil(t, run.FinishedAt)
}

func TestRecorderRunFinishedStatusReflectsErrors(t *testing.T) {
	store := newFakeStore()
	rec := NewRecorder(store, WithHub(NewHub()), WithIdleTimeout(time.Hour))

	now := time.Now()
	rec.persist(successEvent("r1", "f1", "n1", "x"), now)
	rec.persist(errorEvent("r1", "f1", "n2", "y"), now)
	rec.persist(flow.NodeEvent{FlowID: "f1", CorrID: "r1", Phase: flow.NodePhaseRunFinished}, now)

	run := store.getRun("r1")
	assert.Equal(t, "error", run.Status)
	assert.Equal(t, 1, run.ErrorCount)
}

func TestRecorderObservePublishesLiveToHub(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe("f1")
	defer unsub()
	rec := NewRecorder(newFakeStore(), WithHub(hub))

	rec.Observe(runningEvent("r1", "f1", "n1", "x"))

	e := recv(t, ch)
	assert.Equal(t, "n1", e.NodeID)
	assert.Equal(t, PhaseRunning, e.Phase)
}

func TestRecorderDerivesDebugEvent(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe("f1")
	defer unsub()
	rec := NewRecorder(newFakeStore(), WithHub(hub))

	rec.Observe(successEvent("r1", "f1", "dbg", "process-debug"))

	// First the success event, then a derived debug event carrying the payload.
	var sawDebug bool
	for i := 0; i < 2; i++ {
		e := recv(t, ch)
		if e.Phase == PhaseDebug {
			sawDebug = true
			require.NotNil(t, e.Debug)
		}
	}
	assert.True(t, sawDebug, "a process-debug success must yield a derived debug event")
}

func TestRecorderObserveNeverBlocksAndCountsDrops(t *testing.T) {
	rec := NewRecorder(newFakeStore(), WithHub(NewHub()), WithQueueSize(2))
	// Run loop is NOT started, so the queue can only hold QueueSize before drops.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			rec.Observe(runningEvent("r1", "f1", "n1", "x"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Observe blocked when the queue was full")
	}
	assert.Greater(t, rec.Dropped(), int64(0), "overflow must be dropped and counted")
}

func TestRecorderRunDrainsOnShutdown(t *testing.T) {
	store := newFakeStore()
	rec := NewRecorder(store, WithHub(NewHub()), WithFlushInterval(time.Hour), WithBatchSize(1000))

	ctx, cancel := context.WithCancel(context.Background())
	go rec.Run(ctx)

	rec.Observe(successEvent("r1", "f1", "n1", "x"))
	rec.Observe(successEvent("r1", "f1", "n2", "y"))
	time.Sleep(50 * time.Millisecond) // let the loop dequeue
	cancel()                          // shutdown must flush the pending batch

	require.Eventually(t, func() bool {
		return len(store.snapshotEvents()) == 2
	}, time.Second, 10*time.Millisecond, "pending events must be flushed on shutdown")
}
