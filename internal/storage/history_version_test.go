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

// A run records the version of the definition it ran, from its first
// event; later events never change it (ADR-0022: which version a run ran
// decides whether agents see its errors).
func TestStartFlowRunRecordsTheVersion(t *testing.T) {
	store := createTestStore(t)
	start := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	require.NoError(t, store.StartFlowRun(RunStart{ID: "run-1", FlowID: "flow-1", FlowVersion: 3, StartedAt: start}))
	// A later event of the run fills the actor but keeps the version.
	require.NoError(t, store.StartFlowRun(RunStart{ID: "run-1", FlowID: "flow-1", Actor: "token:bot", FlowVersion: 5, StartedAt: start.Add(time.Minute)}))
	require.NoError(t, store.UpsertFlowRun("legacy", "flow-1", start.Add(time.Hour)))

	r, err := store.GetFlowRun("flow-1", "run-1")
	require.NoError(t, err)
	assert.Equal(t, 3, r.FlowVersion)
	assert.Equal(t, "token:bot", r.Actor)
	assert.True(t, r.StartedAt.Equal(start), "the start is kept: %v", r.StartedAt)

	runs, err := store.ListFlowRunsBefore("flow-1", "", 10)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Equal(t, "legacy", runs[0].ID)
	assert.Equal(t, 0, runs[0].FlowVersion, "a run recorded without a version has none")
	assert.Equal(t, 3, runs[1].FlowVersion)

	data, err := json.Marshal(*r)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"flowVersion":3`)
	data, err = json.Marshal(runs[0])
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"flowVersion"`)
}

func TestStartFlowRunErrorsWhenClosed(t *testing.T) {
	store := createTestStore(t)
	require.NoError(t, store.Close())
	assert.ErrorIs(t, store.StartFlowRun(RunStart{ID: "r", FlowID: "f", StartedAt: time.Now()}), ErrDatabaseClosed)
}

// A database created before flow_runs had a version column gains it on
// open, its rows recording none (0), and reopening is harmless.
func TestFlowRunsVersionMigration(t *testing.T) {
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
			error_count INTEGER NOT NULL DEFAULT 0,
			actor       TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO flow_runs (id, flow_id, started_at, status, actor) VALUES ('legacy', 'flow-1', '2026-01-01 00:00:00', 'success', 'editor');
	`)
	require.NoError(t, err)
	require.NoError(t, old.Close())

	for i := 0; i < 2; i++ { // the second open runs the migration again
		store, err := NewSQLiteStore(path)
		require.NoError(t, err, "open %d", i)
		id := "new-" + string(rune('a'+i))
		require.NoError(t, store.StartFlowRun(RunStart{ID: id, FlowID: "flow-1", FlowVersion: 2, StartedAt: time.Now()}))

		legacy, err := store.GetFlowRun("flow-1", "legacy")
		require.NoError(t, err)
		assert.Equal(t, 0, legacy.FlowVersion)
		assert.Equal(t, "editor", legacy.Actor)
		r, err := store.GetFlowRun("flow-1", id)
		require.NoError(t, err)
		assert.Equal(t, 2, r.FlowVersion)
		require.NoError(t, store.Close())
	}
}
