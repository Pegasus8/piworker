package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/require"
)

// ADR-0006 through the real router and builtin nodes: an exclusive resource
// already taken (a webhook path) is a 409 and the flow is not left running; a
// node config its factory refuses is a 400.
func TestDeployStatusForConflictsAndInvalidNodeConfig(t *testing.T) {
	app := newTestApp(t, false)

	newFlow := func(name, triggerType string, cfg map[string]any) string {
		f := flow.NewFlow(name)
		tr := flow.NewNode(triggerType, flow.NodeCategoryInput)
		tr.Config = cfg
		dbg := flow.NewNode("action-log", flow.NodeCategoryOutput)
		f.AddNode(*tr)
		f.AddNode(*dbg)
		f.AddConnection(*flow.NewConnection(tr.ID, "output", dbg.ID, "input"))
		require.NoError(t, app.store.CreateFlow(f))
		return f.ID
	}
	deploy := func(id string) *http.Response {
		w := app.do("POST", "/api/flows/"+id+"/deploy", "application/json", "", editorHeaders)
		return w.Result()
	}

	first := newFlow("a", "trigger-webhook", map[string]any{"path": "deploy-status-dup"})
	second := newFlow("b", "trigger-webhook", map[string]any{"path": "deploy-status-dup"})
	require.Equal(t, http.StatusOK, deploy(first).StatusCode)
	resp := deploy(second)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Contains(t, body["error"], "already registered")
	_, running := app.manager.GetRuntime(second)
	require.False(t, running, "a flow whose trigger failed to start must not be registered")

	bad := newFlow("c", "trigger-cron", map[string]any{"expression": "not a cron expression"})
	require.Equal(t, http.StatusBadRequest, deploy(bad).StatusCode)
}
