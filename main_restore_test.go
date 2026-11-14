package main

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Flows saved before deploys checked configs against schemas may hold values
// the nodes used to clamp or default: an interval under 100 ms, a 0 left by a
// cleared number field, an enum in the wrong case. If such a flow was running,
// it must keep running after the upgrade instead of being marked Failed.
func TestRestoreKeepsPreviouslyAcceptedFlowsRunning(t *testing.T) {
	app := newTestApp(t, false)
	f := flow.NewFlow("legacy")
	trigger := flow.NewNode("trigger-interval", flow.NodeCategoryInput)
	trigger.Config = map[string]interface{}{"interval": float64(50)}
	delay := flow.NewNode("process-delay", flow.NodeCategoryProcessing)
	delay.Config = map[string]interface{}{"delayMs": float64(0)}
	f.AddNode(*trigger)
	f.AddNode(*delay)
	f.AddConnection(*flow.NewConnection(trigger.ID, "output", delay.ID, "input"))
	require.NotEmpty(t, f.ConfigIssues(node.DefaultRegistry), "the fixture must trip the stricter checks")
	require.NoError(t, app.store.CreateFlow(f))
	require.NoError(t, app.store.UpdateFlowState(f.ID, flow.FlowStateRunning))
	marker := storage.Deployment{Version: 1, ActorKind: storage.VersionActorAgent, TokenID: "tok", TokenName: "claude"}
	require.NoError(t, app.store.RecordDeployment(f.ID, marker))

	restoreRunningFlows(app.store, app.manager)

	rt, ok := app.manager.GetRuntime(f.ID)
	require.True(t, ok, "the flow must be restored")
	assert.True(t, rt.IsRunning())
	saved, err := app.store.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, flow.FlowStateRunning, saved.State)
	// A restore is not a new deployment: the marker still names who
	// deployed the flow (H6).
	d, err := app.store.GetDeployment(f.ID)
	require.NoError(t, err)
	require.NotNil(t, d)
	assert.Equal(t, storage.VersionActorAgent, d.ActorKind)
	assert.Equal(t, "claude", d.TokenName)

	// An explicit deploy still asks for the values to be fixed.
	require.NoError(t, app.manager.Undeploy(f.ID))
	w := app.do("POST", "/api/flows/"+f.ID+"/deploy", "", "", editorHeaders)
	assert.Equal(t, 400, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "nodes[0].config.interval")
}

// A redeploy that failed after an agent's draft was saved as version 2
// leaves the flow stopped with its running intent and the marker naming
// version 1: the restore at startup runs version 2, so it records version 2
// as deployed by whoever saved it (the agent's token), not the editor of
// version 1. A marker that names the restored version is kept.
func TestRestoreAttributesAVersionTheMarkerDoesNotName(t *testing.T) {
	app := newTestApp(t, false)
	f := flow.NewFlow("restored")
	f.AddNode(*flow.NewNode("trigger-manual", flow.NodeCategoryInput))
	require.NoError(t, app.store.CreateFlow(f))
	require.NoError(t, app.store.RecordDeployment(f.ID, storage.Deployment{Version: 1, ActorKind: storage.VersionActorEditor}))
	f.Description = "the agent's change"
	v, err := app.store.UpdateFlowVersioned(f, 1, storage.VersionMeta{ActorKind: storage.VersionActorAgent, TokenID: "tok", TokenName: "claude"})
	require.NoError(t, err)
	require.Equal(t, 2, v)
	require.NoError(t, app.store.UpdateFlowState(f.ID, flow.FlowStateRunning))

	restoreRunningFlows(app.store, app.manager)

	d, err := app.store.GetDeployment(f.ID)
	require.NoError(t, err)
	require.NotNil(t, d)
	assert.Equal(t, 2, d.Version)
	assert.Equal(t, storage.VersionActorAgent, d.ActorKind)
	assert.Equal(t, "tok", d.TokenID)
	assert.Equal(t, "claude", d.TokenName)
}
