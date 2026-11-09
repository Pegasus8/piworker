package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/api"
	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newServicesApp is newTestApp with authentication and the services buildApp
// returns.
func newServicesApp(t *testing.T) (testApp, appServices) {
	t.Helper()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "app.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	manager := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	t.Cleanup(manager.StopAll)
	cfg := config.Default()
	cfg.Auth = true
	cfg.MCP.AuthorEnabled = true
	cfg.AllowedHosts = []string{"piworker.local"}
	users, err := storage.NewSQLiteUserStoreWithDB(store.DB())
	require.NoError(t, err)
	hash, err := api.HashPassword("test-password")
	require.NoError(t, err)
	require.NoError(t, users.CreateUser(&types.User{Username: "admin", PasswordHash: hash}))
	router, flows, services := buildApp(cfg, store, manager, node.DefaultRegistry, users)
	t.Cleanup(func() { _ = services.audit.Close(t.Context()) })
	return testApp{handler: router, flows: flows, store: store, users: users, manager: manager}, services
}

func appData[T any](t *testing.T, body []byte) T {
	t.Helper()
	var env struct {
		Success bool `json:"success"`
		Data    T    `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &env), string(body))
	require.True(t, env.Success, string(body))
	return env.Data
}

// The whole path: an agent drafts a flow through the service, the admin
// approves and deploys it over REST, the agent deploys a change of it (its
// deployment is marked), the admin rolls it back.
func TestDraftApprovalAndAgentDeployEndToEnd(t *testing.T) {
	app, services := newServicesApp(t)
	require.NotNil(t, services.authoring)
	headers := app.login(t)
	require.NoError(t, app.store.SetAgentDeployLevel(storage.AgentDeployAll))
	tok, _, err := app.store.CreateAPIToken(storage.NewAPIToken{Name: "claude", Scopes: []string{storage.ScopeAuthor, storage.ScopeDeploy}}, time.Now())
	require.NoError(t, err)
	agent := service.AgentActor(agentauth.TokenInfo{ID: tok.ID, Name: tok.Name, Scopes: tok.Scopes, ExpiresAt: tok.ExpiresAt})

	proposal := flow.NewFlow("from the agent")
	start := flow.NewNode("trigger-manual", flow.NodeCategoryInput)
	start.ID = "start"
	calc := flow.NewNode("process-transform", flow.NodeCategoryProcessing)
	calc.ID, calc.Config = "calc", map[string]any{"expression": "payload"}
	proposal.AddNode(*start)
	proposal.AddNode(*calc)
	proposal.AddConnection(*flow.NewConnection("start", "output", "calc", "input"))
	saved, err := services.authoring.SaveDraft(t.Context(), agent, service.DraftInput{Flow: proposal, Note: "new"})
	require.NoError(t, err)

	w := app.do("GET", api.PendingApprovalsCountPath, "", "", headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 1, appData[api.PendingApprovalsCount](t, w.Body.Bytes()).Drafts)
	w = app.do("GET", "/api/agent/drafts", "", "", headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, appData[service.DraftList](t, w.Body.Bytes()).Drafts, 1)
	w = app.do("GET", "/api/agent/drafts/"+saved.DraftID, "", "", headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	noCSRF := map[string]string{"Cookie": headers["Cookie"]}
	w = app.do("POST", "/api/agent/drafts/"+saved.DraftID+"/approve", "application/json", `{"deploy":true}`, noCSRF)
	require.Equal(t, http.StatusForbidden, w.Code, "writes need the CSRF header")
	w = app.do("POST", "/api/agent/drafts/"+saved.DraftID+"/approve", "application/json", `{"deploy":true}`, headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	applied := appData[service.ApplyResult](t, w.Body.Bytes())
	require.True(t, applied.Deployed, "%+v", applied.DeployError)
	flowID := applied.FlowID
	rt, ok := app.manager.GetRuntime(flowID)
	require.True(t, ok)
	assert.True(t, rt.IsRunning())

	type markedFlow struct {
		ID         string                  `json:"id"`
		Version    int                     `json:"version"`
		Deployment *service.DeploymentView `json:"deployment"`
		Changed    bool                    `json:"agentChanged"`
	}
	w = app.do("GET", "/api/flows/"+flowID, "", "", headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	one := appData[struct {
		Flow markedFlow `json:"flow"`
	}](t, w.Body.Bytes()).Flow
	require.NotNil(t, one.Deployment)
	assert.Equal(t, "editor", one.Deployment.ActorKind)
	assert.False(t, one.Changed)

	// The agent changes and deploys it: the editor sees who did.
	change, err := services.authoring.SaveDraft(t.Context(), agent, service.DraftInput{FlowID: flowID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "calc", Config: map[string]any{"expression": "payload + 1"}},
	}})
	require.NoError(t, err)
	deployed, err := services.authoring.Deploy(t.Context(), agent, service.DeployRequest{FlowID: flowID, DraftID: change.DraftID})
	require.NoError(t, err)
	assert.Equal(t, 2, deployed.Version)
	w = app.do("GET", "/api/flows", "", "", headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	listed := appData[struct {
		Flows []markedFlow `json:"flows"`
	}](t, w.Body.Bytes()).Flows
	require.Len(t, listed, 1)
	assert.True(t, listed[0].Changed)
	require.NotNil(t, listed[0].Deployment)
	assert.Equal(t, "agent", listed[0].Deployment.ActorKind)
	assert.Equal(t, "claude", listed[0].Deployment.TokenName)

	w = app.do("GET", "/api/flows/"+flowID+"/versions", "", "", headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	versions := appData[service.VersionList](t, w.Body.Bytes())
	assert.Equal(t, 2, versions.Current)
	assert.Equal(t, 1, versions.LastAdminApproved)

	w = app.do("POST", "/api/flows/"+flowID+"/rollback", "application/json", "", headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	rolled := appData[service.ApplyResult](t, w.Body.Bytes())
	assert.Equal(t, 3, rolled.Version)
	assert.True(t, rolled.Deployed, "a running flow is redeployed")
	w = app.do("POST", "/api/flows/"+flowID+"/rollback", "application/json", "", headers)
	assert.Equal(t, http.StatusConflict, w.Code, "nothing left to roll back")

	// Agent deploys go through the editor's lifecycle: after shutdown began
	// they are refused like the editor's.
	_, err = services.authoring.Stop(t.Context(), agent, flowID)
	require.NoError(t, err)
	app.flows.BeginShutdown()
	_, err = services.authoring.Deploy(t.Context(), agent, service.DeployRequest{FlowID: flowID})
	require.ErrorIs(t, err, service.ErrUnavailable, "503 from the editor's lifecycle path")
}

// Without authentication there are no drafts, but the version history and
// rollback are the editor's.
func TestVersionRoutesWithoutAuth(t *testing.T) {
	app := newTestApp(t, false)
	f := flow.NewFlow("plain")
	require.NoError(t, app.store.CreateFlow(f))
	w := app.do("GET", "/api/flows/"+f.ID+"/versions", "", "", editorHeaders)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = app.do("POST", "/api/flows/"+f.ID+"/rollback", "application/json", "", editorHeaders)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
}
