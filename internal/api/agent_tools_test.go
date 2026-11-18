package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GET /api/agent/tools answers exactly the JSON of docs/design/mcp.md §7
// (inside the API's success envelope).
func TestListAgentTools(t *testing.T) {
	manager := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	t.Cleanup(manager.StopAll)
	r := mux.NewRouter()
	NewAgentToolsHandler(manager).RegisterRoutes(r)

	var empty struct {
		Data struct {
			Version uint64            `json:"version"`
			Tools   []json.RawMessage `json:"tools"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(getJSON(t, r, "/api/agent/tools"), &empty))
	require.NotNil(t, empty.Data.Tools, "an empty list, not null")
	assert.Empty(t, empty.Data.Tools)

	f := toolTestFlow("Garage")
	require.NoError(t, manager.Deploy(context.Background(), f))
	version, tools := manager.Tools()
	require.Len(t, tools, 1)
	ti := tools[0]

	var body struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(getJSON(t, r, "/api/agent/tools"), &body))
	assert.True(t, body.Success)
	want, err := json.Marshal(map[string]any{
		"version": version,
		"tools": []any{map[string]any{
			"flowId":        f.ID,
			"flowName":      "Garage",
			"triggerNodeId": "tool",
			"toolName":      ti.ToolName,
			"title":         "Open Garage",
			"description":   "Opens the left or right door and reports whether it moved.",
			"risk":          "safe",
			"annotations": map[string]any{
				"readOnlyHint": true, "destructiveHint": false, "idempotentHint": false, "openWorldHint": false,
			},
			"fingerprint": ti.Fingerprint,
			"confirm":     "none",
			"inputSchema": json.RawMessage(ti.Schema.JSON()),
		}},
	})
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(body.Data))
}
