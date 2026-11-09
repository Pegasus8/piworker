package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runIDs(runs []FlowRun) []string {
	ids := make([]string, 0, len(runs))
	for _, r := range runs {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestGetFlowRun(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	start := time.Now()
	require.NoError(t, store.UpsertFlowRunWithActor("run-1", "flow-1", "token:bot", start))
	require.NoError(t, store.FinishFlowRun("run-1", "error", start.Add(time.Second), 3, 1))

	run, err := store.GetFlowRun("flow-1", "run-1")
	require.NoError(t, err)
	assert.Equal(t, "run-1", run.ID)
	assert.Equal(t, "flow-1", run.FlowID)
	assert.Equal(t, "error", run.Status)
	assert.Equal(t, "token:bot", run.Actor)
	assert.Equal(t, 3, run.NodeCount)
	assert.Equal(t, 1, run.ErrorCount)
	require.NotNil(t, run.FinishedAt)
	assert.WithinDuration(t, start, run.StartedAt, time.Millisecond)

	// A run is only found through its own flow.
	_, err = store.GetFlowRun("flow-2", "run-1")
	assert.ErrorIs(t, err, ErrFlowRunNotFound)
	_, err = store.GetFlowRun("flow-1", "missing")
	assert.ErrorIs(t, err, ErrFlowRunNotFound)
}

func TestListFlowRunsBeforePagesNewestFirst(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	base := time.Now()
	// run-c and run-d start at the same instant: the ID breaks the tie so
	// every run appears on exactly one page.
	for i, id := range []string{"run-a", "run-b", "run-c", "run-d", "run-e"} {
		at := base.Add(time.Duration(i) * time.Second)
		if id == "run-d" {
			at = base.Add(2 * time.Second)
		}
		require.NoError(t, store.UpsertFlowRun(id, "flow-1", at))
	}
	require.NoError(t, store.UpsertFlowRun("other", "flow-2", base.Add(time.Hour)))

	page, err := store.ListFlowRunsBefore("flow-1", "", 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"run-e", "run-d"}, runIDs(page))

	page, err = store.ListFlowRunsBefore("flow-1", "run-d", 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"run-c", "run-b"}, runIDs(page))

	page, err = store.ListFlowRunsBefore("flow-1", "run-b", 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"run-a"}, runIDs(page))

	page, err = store.ListFlowRunsBefore("flow-1", "run-a", 2)
	require.NoError(t, err)
	assert.Empty(t, page)
}

func TestListFlowRunsBeforeUnknownCursor(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	require.NoError(t, store.UpsertFlowRun("run-1", "flow-1", time.Now()))
	require.NoError(t, store.UpsertFlowRun("run-2", "flow-2", time.Now()))

	_, err := store.ListFlowRunsBefore("flow-1", "missing", 10)
	assert.ErrorIs(t, err, ErrFlowRunNotFound)
	// A cursor from another flow is unknown too.
	_, err = store.ListFlowRunsBefore("flow-1", "run-2", 10)
	assert.ErrorIs(t, err, ErrFlowRunNotFound)
}

func TestHistoryReadsOnClosedStore(t *testing.T) {
	store := createTestStore(t)
	require.NoError(t, store.Close())

	_, err := store.GetFlowRun("flow-1", "run-1")
	assert.ErrorIs(t, err, ErrDatabaseClosed)
	_, err = store.ListFlowRunsBefore("flow-1", "", 10)
	assert.ErrorIs(t, err, ErrDatabaseClosed)
}

// The read views page through a run's events with their errors cut, so a
// run of any size costs one bounded page at a time; a run is only read
// through its own flow.
func TestListFlowRunEventsAfterPagesWithCutErrors(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()
	require.NoError(t, store.UpsertFlowRun("run-1", "flow-1", now))
	require.NoError(t, store.UpsertFlowRun("run-2", "flow-2", now))
	errs := []string{"", "short", "ééééééé long", "x"}
	var events []NodeEventRecord
	for i, e := range errs {
		events = append(events, NodeEventRecord{RunID: "run-1", FlowID: "flow-1", NodeID: "n", NodeType: "t", Phase: "error",
			Error: e, CreatedAt: now.Add(time.Duration(i) * time.Millisecond)})
	}
	events = append(events, NodeEventRecord{RunID: "run-2", FlowID: "flow-2", NodeID: "n", NodeType: "t", Phase: "success", CreatedAt: now})
	require.NoError(t, store.InsertNodeEvents(events))

	page, err := store.ListFlowRunEventsAfter("flow-1", "run-1", 0, 3, 6)
	require.NoError(t, err)
	require.Len(t, page, 3)
	assert.Equal(t, []string{"", "short", "éééééé"}, []string{page[0].Error, page[1].Error, page[2].Error},
		"errors cut to 6 characters, not bytes")
	assert.Less(t, page[0].ID, page[1].ID)

	page, err = store.ListFlowRunEventsAfter("flow-1", "run-1", page[2].ID, 3, 6)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, "x", page[0].Error)
	assert.Equal(t, "run-1", page[0].RunID)

	page, err = store.ListFlowRunEventsAfter("flow-2", "run-1", 0, 3, 6)
	require.NoError(t, err)
	assert.Empty(t, page, "not through another flow")

	n, err := store.CountFlowRunEvents("flow-1", "run-1")
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	n, err = store.CountFlowRunEvents("flow-2", "run-1")
	require.NoError(t, err)
	assert.Zero(t, n)
}
