package storage

import (
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/require"
)

func TestDeleteFlowRemovesItsHistory(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()
	gone := createTestFlow("gone")
	kept := createTestFlow("kept")
	require.NoError(t, store.CreateFlow(gone))
	require.NoError(t, store.CreateFlow(kept))
	now := time.Now()
	for _, f := range []*flow.Flow{gone, kept} {
		run := "run-" + f.ID
		require.NoError(t, store.UpsertFlowRun(run, f.ID, now))
		require.NoError(t, store.InsertNodeEvents([]NodeEventRecord{
			{RunID: run, FlowID: f.ID, NodeID: "n1", NodeType: "trigger-manual", Phase: "success", CreatedAt: now},
		}))
	}

	require.NoError(t, store.DeleteFlow(gone.ID))

	count := func(table, flowID string) int {
		var n int
		require.NoError(t, store.DB().QueryRow("SELECT COUNT(*) FROM "+table+" WHERE flow_id = ?", flowID).Scan(&n))
		return n
	}
	require.Zero(t, count("flow_runs", gone.ID), "runs of a deleted flow must not linger")
	require.Zero(t, count("node_events", gone.ID), "events of a deleted flow must not linger")
	require.Equal(t, 1, count("flow_runs", kept.ID))
	require.Equal(t, 1, count("node_events", kept.ID))
}

func TestListFlowsSkipsCorruptRows(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()
	good := createTestFlow("good")
	good.State = flow.FlowStateRunning
	require.NoError(t, store.CreateFlow(good))
	// Simulate an SD-card / partial-write corruption of one row.
	_, err := store.DB().Exec(`INSERT INTO flows (id, name, description, data, state, created_at, updated_at)
		VALUES ('bad', 'bad', '', '{"id":"bad", truncated', 'running', ?, ?)`, time.Now(), time.Now())
	require.NoError(t, err)

	flows, err := store.ListFlows(nil)
	require.NoError(t, err, "one corrupt row must not hide every other flow")
	require.Len(t, flows, 1)
	require.Equal(t, good.ID, flows[0].ID)

	running, err := store.GetRunningFlows()
	require.NoError(t, err, "boot-time restore must still see the healthy flows")
	require.Len(t, running, 1)
}
