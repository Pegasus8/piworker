package service

import (
	"context"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addRunOf records a run of version of flowID (as the recorder does with
// the version the runtime reports) failing with event.
func (e *readsEnv) addRunOf(flowID, runID string, version int, offset time.Duration, event storage.NodeEventRecord) {
	e.t.Helper()
	start := runsBase.Add(offset)
	require.NoError(e.t, e.store.StartFlowRun(storage.RunStart{ID: runID, FlowID: flowID, FlowVersion: version, StartedAt: start}))
	event.RunID, event.FlowID, event.CreatedAt = runID, flowID, start.Add(time.Millisecond)
	require.NoError(e.t, e.store.InsertNodeEvents([]storage.NodeEventRecord{event}))
	require.NoError(e.t, e.store.FinishFlowRun(runID, "error", start.Add(time.Second), 1, 1))
}

// What an agent deployed itself may raise errors on purpose to hand back
// data (audit finding F2): an agent sees a run's recorded errors only when
// the version the run ran, recorded with the run, is one an admin approved.
// Otherwise (an agent's version, or a run whose version is unknown or
// pruned) the errors are withheld; the editor sees them. The deployment
// marker plays no part: a run of an agent's version recorded before the
// marker names it, or still queued in the recorder when the admin
// redeploys an approved version, is still the agent's.
func TestGetRunWithholdsErrorsOfRunsNotOfAnApprovedVersion(t *testing.T) {
	e := newReadsEnv(t)
	f := e.save(manualFlow("runs")) // version 1, the admin's
	leak := storage.NodeEventRecord{NodeID: "peek", NodeType: "process-transform", Phase: "error",
		Error: `time: invalid duration "{\"door_code\": \"8231-SECRET\"}"`}
	agent := readsAgent(storage.ScopeRead)
	errorOf := func(actor Actor, runID string) string {
		t.Helper()
		run, err := e.reads.GetRun(context.Background(), actor, f.ID, runID)
		require.NoError(t, err)
		require.Len(t, run.Events, 1)
		assert.Equal(t, "error", run.Events[0].Phase, "the phase is shown")
		assert.Equal(t, "peek", run.Events[0].NodeID)
		return run.Events[0].Error
	}

	// A run of the admin's version shows its errors, with no deployment
	// marker recorded at all.
	e.addRunOf(f.ID, "approved", 1, 0, leak)
	assert.Contains(t, errorOf(agent, "approved"), "8231-SECRET")

	// A run recorded without a version (before versions were recorded).
	e.addRunOf(f.ID, "unknown", 0, time.Minute, leak)
	assert.Equal(t, withheldRunError, errorOf(agent, "unknown"))
	assert.Contains(t, errorOf(EditorActor(), "unknown"), "8231-SECRET", "the editor sees it")

	// An agent saves version 2. The marker still names version 1 (the
	// window between Deploy and RecordDeployment, or a marker that failed
	// to record): the runs of version 2 are still the agent's.
	g := mustStored(t, e, f.ID)
	nodeOfFlow(g, "log").Config["message"] = "changed"
	v, err := e.store.UpdateFlowVersioned(g, 0, storage.VersionMeta{ActorKind: storage.VersionActorAgent, TokenID: "tok-1", TokenName: "reader"})
	require.NoError(t, err)
	require.Equal(t, 2, v)
	require.NoError(t, e.store.RecordDeployment(f.ID, storage.Deployment{Version: 1, ActorKind: storage.VersionActorEditor, DeployedAt: runsBase}))
	e.addRunOf(f.ID, "window", v, 2*time.Minute, leak)
	assert.Equal(t, withheldRunError, errorOf(agent, "window"))

	// The admin redeploys version 1 while runs of version 2 are still
	// queued in the recorder: they are stamped after the new marker but
	// are still the agent's.
	require.NoError(t, e.store.RecordDeployment(f.ID, storage.Deployment{Version: 1, ActorKind: storage.VersionActorEditor, DeployedAt: runsBase.Add(3 * time.Minute)}))
	e.addRunOf(f.ID, "queued", v, 4*time.Minute, leak)
	assert.Equal(t, withheldRunError, errorOf(agent, "queued"))
	assert.Contains(t, errorOf(EditorActor(), "queued"), "8231-SECRET")

	// Which deployment is current does not change what an earlier run ran.
	require.NoError(t, e.store.RecordDeployment(f.ID, storage.Deployment{Version: v, ActorKind: storage.VersionActorAgent, DeployedAt: runsBase.Add(5 * time.Minute)}))
	assert.Contains(t, errorOf(agent, "approved"), "8231-SECRET")

	// A version that is not stored (pruned) is unknown.
	e.addRunOf(f.ID, "pruned", 99, 6*time.Minute, leak)
	assert.Equal(t, withheldRunError, errorOf(agent, "pruned"))
}

func mustStored(t *testing.T, e *readsEnv, id string) *flow.Flow {
	t.Helper()
	f, err := e.store.GetFlow(id)
	require.NoError(t, err)
	return f
}

func nodeOfFlow(f *flow.Flow, id string) *flow.Node {
	for i := range f.Nodes {
		if f.Nodes[i].ID == id {
			return &f.Nodes[i]
		}
	}
	return nil
}
