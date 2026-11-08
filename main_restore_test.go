package main

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
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

	restoreRunningFlows(app.store, app.manager)

	rt, ok := app.manager.GetRuntime(f.ID)
	require.True(t, ok, "the flow must be restored")
	assert.True(t, rt.IsRunning())
	saved, err := app.store.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, flow.FlowStateRunning, saved.State)

	// An explicit deploy still asks for the values to be fixed.
	require.NoError(t, app.manager.Undeploy(f.ID))
	w := app.do("POST", "/api/flows/"+f.ID+"/deploy", "", "", editorHeaders)
	assert.Equal(t, 400, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "nodes[0].config.interval")
}
