package storage

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertFlowRunWithActor(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	now := time.Now()
	require.NoError(t, store.UpsertFlowRunWithActor("called", "flow-1", "token:bot", now))
	require.NoError(t, store.UpsertFlowRun("plain", "flow-1", now.Add(time.Second)))

	runs, err := store.ListFlowRuns("flow-1", 10, 0)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Equal(t, "plain", runs[0].ID)
	assert.Equal(t, "", runs[0].Actor, "runs not started by a caller have no actor")
	assert.Equal(t, "called", runs[1].ID)
	assert.Equal(t, "token:bot", runs[1].Actor)
}

// TestUpsertFlowRunActorIsFilledButNeverReplaced: a later upsert (another
// event of the same run) fills a missing actor but never rewrites one.
func TestUpsertFlowRunActorIsFilledButNeverReplaced(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	start := time.Now()
	require.NoError(t, store.UpsertFlowRunWithActor("run-1", "flow-1", "", start))
	require.NoError(t, store.UpsertFlowRunWithActor("run-1", "flow-1", "token:first", start.Add(time.Minute)))
	require.NoError(t, store.UpsertFlowRunWithActor("run-1", "flow-1", "token:second", start.Add(time.Hour)))
	require.NoError(t, store.UpsertFlowRunWithActor("run-1", "flow-1", "", start.Add(time.Hour)))

	runs, err := store.ListFlowRuns("flow-1", 10, 0)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "token:first", runs[0].Actor)
	assert.Equal(t, "running", runs[0].Status)
	assert.WithinDuration(t, start, runs[0].StartedAt, time.Second, "the start time never moves")
}

func TestUpsertFlowRunWithActorErrorsWhenClosed(t *testing.T) {
	store := createTestStore(t)
	require.NoError(t, store.Close())
	assert.ErrorIs(t, store.UpsertFlowRunWithActor("r", "f", "token:x", time.Now()), ErrDatabaseClosed)
}

func TestFlowRunJSONIncludesActor(t *testing.T) {
	data, err := json.Marshal(FlowRun{ID: "r1", FlowID: "f1", Status: "success", Actor: "token:bot"})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"actor":"token:bot"`)

	data, err = json.Marshal(FlowRun{ID: "r1", FlowID: "f1", Status: "success"})
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"actor"`, "runs without actor keep their previous shape")
}

// TestFlowRunsActorMigration: a database created before flow_runs had an actor
// column gains it on open, keeps its rows, and reopening is harmless.
func TestFlowRunsActorMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = old.Exec(`
		CREATE TABLE flow_runs (
			id          TEXT PRIMARY KEY,
			flow_id     TEXT NOT NULL,
			started_at  DATETIME NOT NULL,
			finished_at DATETIME,
			status      TEXT NOT NULL DEFAULT 'running',
			node_count  INTEGER NOT NULL DEFAULT 0,
			error_count INTEGER NOT NULL DEFAULT 0
		);
		INSERT INTO flow_runs (id, flow_id, started_at, status) VALUES ('legacy', 'flow-1', '2026-01-01 00:00:00', 'success');
	`)
	require.NoError(t, err)
	require.NoError(t, old.Close())

	for i := 0; i < 2; i++ { // the second open runs the migration again
		store, err := NewSQLiteStore(path)
		require.NoError(t, err, "open %d", i)

		require.NoError(t, store.UpsertFlowRunWithActor("new-"+string(rune('a'+i)), "flow-1", "token:bot", time.Now()))
		runs, err := store.ListFlowRuns("flow-1", 10, 0)
		require.NoError(t, err)
		require.Len(t, runs, 2+i)
		byID := map[string]FlowRun{}
		for _, r := range runs {
			byID[r.ID] = r
		}
		assert.Equal(t, "", byID["legacy"].Actor, "existing rows get an empty actor")
		assert.Equal(t, "success", byID["legacy"].Status)
		assert.Equal(t, "token:bot", byID["new-a"].Actor)
		require.NoError(t, store.Close())
	}
}
