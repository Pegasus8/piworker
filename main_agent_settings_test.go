package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/Pegasus8/piworker/internal/api"
	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The agent pages report the [mcp] enabled and lan_access settings the
// server runs with.
func TestAgentSettingsReportTheMCPConfig(t *testing.T) {
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "app.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	manager := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	t.Cleanup(manager.StopAll)
	users, err := storage.NewSQLiteUserStoreWithDB(store.DB())
	require.NoError(t, err)
	hash, err := api.HashPassword("test-password")
	require.NoError(t, err)
	require.NoError(t, users.CreateUser(&types.User{Username: "admin", PasswordHash: hash}))
	cfg := config.Default()
	cfg.Auth = true
	cfg.AllowedHosts = []string{"piworker.local"}
	cfg.MCP.Enabled = true
	cfg.MCP.LANAccess = true
	router, flows := buildRouter(cfg, store, manager, node.DefaultRegistry, users)
	app := testApp{handler: router, flows: flows, store: store, users: users, manager: manager}

	w := app.do("GET", "/api/agent/settings", "", "", app.login(t))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got struct {
		Data struct {
			MCPEnabled bool `json:"mcpEnabled"`
			LANAccess  bool `json:"lanAccess"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.True(t, got.Data.MCPEnabled)
	assert.True(t, got.Data.LANAccess)
}
