package api

import (
	"net/http"
	"testing"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type agentSettings struct {
	AgentDeploy   string `json:"agentDeploy"`
	AuthorEnabled bool   `json:"authorEnabled"`
}

func currentLevel(t *testing.T, s *storage.SQLiteStore) storage.AgentDeployLevel {
	t.Helper()
	level, err := s.AgentDeployLevel()
	require.NoError(t, err)
	return level
}

func TestGetAgentSettings(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	w := env.do("GET", "/api/agent/settings", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, agentSettings{AgentDeploy: "off"}, data[agentSettings](t, w))

	authoring := newAgentTestEnv(t, AgentHandlerOptions{AuthorEnabled: true})
	w = authoring.do("GET", "/api/agent/settings", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, data[agentSettings](t, w).AuthorEnabled)
}

func TestPutAgentSettingsRequiresStepUp(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})

	w := env.do("PUT", "/api/agent/settings", map[string]any{"agentDeploy": "all"})
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	w = env.do("PUT", "/api/agent/settings", map[string]any{"agentDeploy": "all", "password": "wrong-password"})
	require.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, storage.AgentDeployOff, currentLevel(t, env.store), "accepting the risk needs the password")

	for _, body := range []map[string]any{
		{"agentDeploy": "everything", "password": "test-password"},
		{"agentDeploy": "", "password": "test-password"},
		{"password": "test-password"},
		{"agentDeploy": "low", "password": "test-password", "mcpEnabled": true},
	} {
		require.Equal(t, http.StatusBadRequest, env.do("PUT", "/api/agent/settings", body).Code, body)
	}

	w = env.do("PUT", "/api/agent/settings", map[string]any{"agentDeploy": "low", "password": "test-password"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, agentSettings{AgentDeploy: "low"}, data[agentSettings](t, w))
	assert.Equal(t, storage.AgentDeployLow, currentLevel(t, env.store))

	w = env.do("PUT", "/api/agent/settings", map[string]any{"agentDeploy": "off", "password": "test-password"})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, storage.AgentDeployOff, currentLevel(t, env.store))
}
