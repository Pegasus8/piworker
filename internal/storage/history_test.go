package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertFlowRunIsIdempotent(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	start := time.Now().Truncate(time.Second)
	require.NoError(t, store.UpsertFlowRun("run-1", "flow-1", start))
	// A second upsert (e.g. another node event for the same run) must not create
	// a duplicate nor move the start time.
	require.NoError(t, store.UpsertFlowRun("run-1", "flow-1", start.Add(time.Hour)))

	runs, err := store.ListFlowRuns("flow-1", 10, 0)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "run-1", runs[0].ID)
	assert.Equal(t, "running", runs[0].Status)
	assert.WithinDuration(t, start, runs[0].StartedAt, time.Second)
	assert.Nil(t, runs[0].FinishedAt)
}

func TestInsertNodeEventsAndList(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()
	require.NoError(t, store.UpsertFlowRun("run-1", "flow-1", now))
	require.NoError(t, store.InsertNodeEvents([]NodeEventRecord{
		{RunID: "run-1", FlowID: "flow-1", NodeID: "n1", NodeType: "trigger-manual", Phase: "running", MsgID: "m1", CreatedAt: now},
		{RunID: "run-1", FlowID: "flow-1", NodeID: "n1", NodeType: "trigger-manual", Phase: "success", MsgID: "m1", DurationMs: 1.5, CreatedAt: now.Add(time.Millisecond)},
		{RunID: "run-1", FlowID: "flow-1", NodeID: "n2", NodeType: "action-log", Phase: "error", MsgID: "m1", Error: "boom", CreatedAt: now.Add(2 * time.Millisecond)},
	}))

	events, err := store.ListNodeEvents("run-1")
	require.NoError(t, err)
	require.Len(t, events, 3)
	// Ordered by created_at ascending.
	assert.Equal(t, "running", events[0].Phase)
	assert.Equal(t, "success", events[1].Phase)
	assert.Equal(t, 1.5, events[1].DurationMs)
	assert.Equal(t, "error", events[2].Phase)
	assert.Equal(t, "boom", events[2].Error)
}

func TestInsertNodeEventsEmptyBatchIsNoop(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()
	require.NoError(t, store.InsertNodeEvents(nil))
	require.NoError(t, store.InsertNodeEvents([]NodeEventRecord{}))
}

func TestFinishFlowRun(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	start := time.Now()
	require.NoError(t, store.UpsertFlowRun("run-1", "flow-1", start))
	finish := start.Add(50 * time.Millisecond)
	require.NoError(t, store.FinishFlowRun("run-1", "error", finish, 4, 1))

	runs, err := store.ListFlowRuns("flow-1", 10, 0)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "error", runs[0].Status)
	assert.Equal(t, 4, runs[0].NodeCount)
	assert.Equal(t, 1, runs[0].ErrorCount)
	require.NotNil(t, runs[0].FinishedAt)
	assert.WithinDuration(t, finish, *runs[0].FinishedAt, time.Second)
}

func TestListFlowRunsScopedAndPaginated(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	base := time.Now()
	for i := 0; i < 5; i++ {
		require.NoError(t, store.UpsertFlowRun(runID(i), "flow-A", base.Add(time.Duration(i)*time.Second)))
	}
	require.NoError(t, store.UpsertFlowRun("other", "flow-B", base))

	// Newest first, limited.
	runs, err := store.ListFlowRuns("flow-A", 2, 0)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Equal(t, runID(4), runs[0].ID, "newest run first")
	assert.Equal(t, runID(3), runs[1].ID)

	// Offset paginates further.
	page2, err := store.ListFlowRuns("flow-A", 2, 2)
	require.NoError(t, err)
	require.Len(t, page2, 2)
	assert.Equal(t, runID(2), page2[0].ID)

	// flow-B isolation.
	bRuns, err := store.ListFlowRuns("flow-B", 10, 0)
	require.NoError(t, err)
	require.Len(t, bRuns, 1)
}

func TestPruneHistory(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()
	old := now.Add(-48 * time.Hour)

	// One old run with events.
	require.NoError(t, store.UpsertFlowRun("old", "flow-1", old))
	require.NoError(t, store.InsertNodeEvents([]NodeEventRecord{
		{RunID: "old", FlowID: "flow-1", NodeID: "n1", NodeType: "x", Phase: "success", CreatedAt: old},
	}))
	// Several recent runs, exceeding the per-flow cap.
	for i := 0; i < 5; i++ {
		require.NoError(t, store.UpsertFlowRun(runID(i), "flow-1", now.Add(time.Duration(i)*time.Second)))
	}

	deleted, err := store.PruneHistory(now.Add(-24*time.Hour), 3)
	require.NoError(t, err)
	assert.Equal(t, 3, deleted, "1 old + 2 over the per-flow cap of 3")

	runs, err := store.ListFlowRuns("flow-1", 100, 0)
	require.NoError(t, err)
	assert.Len(t, runs, 3, "only the 3 newest within retention remain")

	// Orphaned node_events for the pruned old run must be gone.
	oldEvents, err := store.ListNodeEvents("old")
	require.NoError(t, err)
	assert.Empty(t, oldEvents)
}

func TestHistoryMethodsErrorWhenClosed(t *testing.T) {
	store := createTestStore(t)
	require.NoError(t, store.Close())

	assert.ErrorIs(t, store.UpsertFlowRun("r", "f", time.Now()), ErrDatabaseClosed)
	assert.ErrorIs(t, store.FinishFlowRun("r", "success", time.Now(), 0, 0), ErrDatabaseClosed)
	assert.ErrorIs(t, store.InsertNodeEvents([]NodeEventRecord{{RunID: "r"}}), ErrDatabaseClosed)
	_, err := store.ListFlowRuns("f", 10, 0)
	assert.ErrorIs(t, err, ErrDatabaseClosed)
	_, err = store.ListNodeEvents("r")
	assert.ErrorIs(t, err, ErrDatabaseClosed)
	_, err = store.PruneHistory(time.Now(), 1)
	assert.ErrorIs(t, err, ErrDatabaseClosed)
}

func runID(i int) string {
	return "run-" + string(rune('a'+i))
}
