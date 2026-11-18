package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rawSettings(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), w.Body.String())
	return env.Data
}

// The agent pages say whether the MCP endpoint is on (mcp.enabled) and, with
// TLS off, whether it accepts LAN clients (mcp.lan_access); both live in the
// TOML, so they are read-only here.
func TestAgentSettingsReportTheMCPEndpoint(t *testing.T) {
	off := newAgentTestEnv(t, AgentHandlerOptions{})
	w := off.do("GET", "/api/agent/settings", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := rawSettings(t, w)
	assert.Equal(t, false, got["mcpEnabled"], "reported even when off")
	assert.Equal(t, false, got["lanAccess"])

	on := newAgentTestEnv(t, AgentHandlerOptions{MCPEnabled: true, LANAccess: true})
	w = on.do("GET", "/api/agent/settings", nil)
	require.Equal(t, http.StatusOK, w.Code)
	got = rawSettings(t, w)
	assert.Equal(t, true, got["mcpEnabled"])
	assert.Equal(t, true, got["lanAccess"])

	w = on.do("PUT", "/api/agent/settings", map[string]any{"agentDeploy": "low", "password": "test-password"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, true, rawSettings(t, w)["mcpEnabled"])

	// They cannot be changed from the editor.
	w = on.do("PUT", "/api/agent/settings", map[string]any{"agentDeploy": "off", "password": "test-password", "lanAccess": false})
	require.Equal(t, http.StatusBadRequest, w.Code)
}
