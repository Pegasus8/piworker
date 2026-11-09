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

// deployedAt records a deployment of version of flowID by kind at at (the
// marker the lifecycle path records).
func (e *readsEnv) deployedAt(flowID string, version int, kind string, at time.Time) {
	e.t.Helper()
	require.NoError(e.t, e.store.RecordDeployment(flowID, storage.Deployment{Version: version, ActorKind: kind, DeployedAt: at}))
}

// approvedDeployment records that the admin deployed version 1 of flowID
// (admin-approved) before the runs of these tests.
func (e *readsEnv) approvedDeployment(flowID string) {
	e.deployedAt(flowID, 1, storage.VersionActorEditor, runsBase.Add(-time.Hour))
}

// What an agent deployed itself may raise errors on purpose to hand back
// data (audit finding F2): an agent sees a run's recorded errors only when
// the run started after the admin deployed a version the admin approved.
// Otherwise (an agent's deployment, or a run whose version cannot be told)
// the errors are withheld; the editor sees them.
func TestGetRunWithholdsErrorsOfRunsNotOfAnApprovedDeployment(t *testing.T) {
	e := newReadsEnv(t)
	f := e.save(manualFlow("runs"))
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

	// No deployment recorded: which version ran is unknown.
	e.addRun(f.ID, "unknown", "", 0, time.Second, "error", leak)
	assert.NotContains(t, errorOf(agent, "unknown"), "8231")
	assert.Equal(t, withheldRunError, errorOf(agent, "unknown"))
	assert.Contains(t, errorOf(EditorActor(), "unknown"), "8231-SECRET", "the editor sees it")

	// The admin deployed the version it approved: its runs show errors.
	e.deployedAt(f.ID, 1, storage.VersionActorEditor, runsBase.Add(time.Minute))
	e.addRun(f.ID, "approved", "", 2*time.Minute, time.Second, "error", leak)
	assert.Contains(t, errorOf(agent, "approved"), "8231-SECRET")
	assert.Equal(t, withheldRunError, errorOf(agent, "unknown"), "a run before that deployment")
	e.addRun(f.ID, "same-second", "", time.Minute+500*time.Millisecond, time.Second, "error", leak)
	assert.Equal(t, withheldRunError, errorOf(agent, "same-second"), "the marker counts in seconds")

	// An agent deployed its own version: its runs do not.
	g := mustStored(t, e, f.ID)
	nodeOfFlow(g, "log").Config["message"] = "changed"
	v, err := e.store.UpdateFlowVersioned(g, 0, storage.VersionMeta{ActorKind: storage.VersionActorAgent, TokenID: "tok-1", TokenName: "reader"})
	require.NoError(t, err)
	e.deployedAt(f.ID, v, storage.VersionActorAgent, runsBase.Add(3*time.Minute))
	e.addRun(f.ID, "agents", "", 4*time.Minute, time.Second, "error", leak)
	assert.Equal(t, withheldRunError, errorOf(agent, "agents"))
	assert.Equal(t, withheldRunError, errorOf(agent, "approved"), "the current deployment decides")
	assert.Contains(t, errorOf(EditorActor(), "agents"), "8231-SECRET")
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
