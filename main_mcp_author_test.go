package main

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/api"
	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mountMCP registers the author and deploy tools and author_flow when the
// authoring service exists, with the agent deploy level read from the
// store: with mcp.author_enabled and agent_deploy=low an author+deploy
// token lists them all, a read token none of them. (buildApp passes the
// authoring service once it creates one: TODO(wave4-join) in buildApp.)
func TestMCPAuthorAndDeployToolsAreWired(t *testing.T) {
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "app.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	require.NoError(t, store.SetAgentDeployLevel(storage.AgentDeployLow))
	manager := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	t.Cleanup(manager.StopAll)
	users, err := storage.NewSQLiteUserStoreWithDB(store.DB())
	require.NoError(t, err)

	cfg := config.Default()
	cfg.Auth = true
	cfg.MCP.Enabled = true
	cfg.MCP.AuthorEnabled = true
	_, _, services := buildApp(cfg, store, manager, node.DefaultRegistry, users)
	authoring := service.NewAuthoring(service.AuthoringOptions{
		Store: store, Registry: node.DefaultRegistry, Audit: services.audit, AuthorEnabled: true, Logger: zerolog.Nop(),
	})
	router := mux.NewRouter()
	mountMCP(router, cfg, services, agentauth.NewTokenVerifier(store), api.NewHostGuard(api.HostGuardOptions{}),
		mcpBuiltins{authoring: authoring, registry: node.DefaultRegistry, settings: store})
	srv := httptest.NewTLSServer(router)
	t.Cleanup(srv.Close)
	app := mcpApp{testApp: testApp{store: store}, srv: srv}

	list := func(scopes ...string) []string {
		_, raw := app.token(t, "agent", scopes)
		session, err := app.connect(t, raw, "2025-11-25")
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		res, err := session.ListTools(ctx, nil)
		require.NoError(t, err)
		var names []string
		for _, tool := range res.Tools {
			names = append(names, tool.Name)
		}
		sort.Strings(names)
		return names
	}
	assert.Equal(t, []string{"pw_deploy_flow", "pw_dry_run", "pw_get_draft", "pw_list_node_types", "pw_save_draft",
		"pw_stop_flow", "pw_test_node", "pw_validate_flow"}, list(storage.ScopeAuthor, storage.ScopeDeploy))
	assert.NotContains(t, list(storage.ScopeRead), "pw_save_draft")
}
