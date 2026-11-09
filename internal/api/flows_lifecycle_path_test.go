package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The editor's lifecycle path is the one agent deploys and approvals go
// through (docs/design/mcp.md §10): FlowsHandler implements
// service.FlowLifecycle under its own lifecycleMu.

var _ service.FlowLifecycle = (*FlowsHandler)(nil)

var agentDeployer = service.DeployActor{Kind: service.ActorAgent, TokenID: "tok-1", TokenName: "claude"}

// lifecycleCall runs fn under the handler's lifecycle lock.
func lifecycleCall(t *testing.T, h *FlowsHandler, fn func(service.LockedLifecycle) error) error {
	t.Helper()
	return h.WithLock(context.Background(), fn)
}

func requireLifecycleStatus(t *testing.T, err error, status int) *service.LifecycleError {
	t.Helper()
	var le *service.LifecycleError
	require.True(t, errors.As(err, &le), "%v is not a *LifecycleError", err)
	assert.Equal(t, status, le.Status, le.Message)
	return le
}

func TestWithLockHoldsTheLifecycleLock(t *testing.T) {
	server, _ := createTestServer(t)
	h := server.handler
	sentinel := errors.New("from fn")
	err := lifecycleCall(t, h, func(service.LockedLifecycle) error {
		assert.False(t, h.lifecycleMu.TryLock(), "fn runs holding lifecycleMu")
		return sentinel
	})
	assert.ErrorIs(t, err, sentinel, "WithLock returns fn's error")
	require.True(t, h.lifecycleMu.TryLock(), "released afterwards")
	h.lifecycleMu.Unlock()
}

func TestLockedDeployRecordsTheDeployment(t *testing.T) {
	server, store := createTestServer(t)
	h := server.handler
	f := createTestFlow("agent deploy")
	require.NoError(t, store.CreateFlow(f))
	require.NoError(t, store.UpdateFlow(f)) // version 2

	var version int
	require.NoError(t, lifecycleCall(t, h, func(l service.LockedLifecycle) error {
		assert.False(t, l.Running(f.ID))
		var err error
		version, err = l.Deploy(context.Background(), f.ID, agentDeployer, false)
		assert.True(t, l.Running(f.ID))
		return err
	}))
	t.Cleanup(h.manager.StopAll)
	assert.Equal(t, 2, version)
	stored, err := store.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, flow.FlowStateRunning, stored.State, "the restart intent is persisted")
	d, err := store.GetDeployment(f.ID)
	require.NoError(t, err)
	require.NotNil(t, d)
	assert.Equal(t, 2, d.Version)
	assert.Equal(t, storage.VersionActorAgent, d.ActorKind)
	assert.Equal(t, "tok-1", d.TokenID)
	assert.Equal(t, "claude", d.TokenName)

	// Deploying a running flow conflicts unless it restarts it.
	err = lifecycleCall(t, h, func(l service.LockedLifecycle) error {
		_, err := l.Deploy(context.Background(), f.ID, agentDeployer, false)
		return err
	})
	requireLifecycleStatus(t, err, http.StatusConflict)

	// A restart deploys the stored current version.
	f.Name = "agent deploy v3"
	require.NoError(t, store.UpdateFlow(f))
	require.NoError(t, lifecycleCall(t, h, func(l service.LockedLifecycle) error {
		var err error
		version, err = l.Deploy(context.Background(), f.ID, service.DeployActor{Kind: service.ActorEditor}, true)
		return err
	}))
	assert.Equal(t, 3, version)
	d, err = store.GetDeployment(f.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, d.Version)
	assert.Equal(t, storage.VersionActorEditor, d.ActorKind)
	assert.Empty(t, d.TokenID)
	rt, ok := h.manager.GetRuntime(f.ID)
	require.True(t, ok)
	assert.True(t, rt.IsRunning())
}

func TestLockedDeployRefusals(t *testing.T) {
	server, store := createTestServer(t)
	h := server.handler
	t.Cleanup(h.manager.StopAll)
	deploy := func(id string) error {
		return lifecycleCall(t, h, func(l service.LockedLifecycle) error {
			_, err := l.Deploy(context.Background(), id, agentDeployer, false)
			return err
		})
	}
	requireLifecycleStatus(t, deploy("nope"), http.StatusNotFound)

	bad := createTestFlow("invalid")
	bad.AddNode(*flow.NewNode("no-such-type", flow.NodeCategoryProcessing))
	require.NoError(t, store.CreateFlow(bad))
	le := requireLifecycleStatus(t, deploy(bad.ID), http.StatusBadRequest)
	assert.ErrorIs(t, le, flow.ErrInvalidNodeConfig, "the cause is deploy's error")
	d, err := store.GetDeployment(bad.ID)
	require.NoError(t, err)
	assert.Nil(t, d, "a refused deploy records nothing")

	ok := createTestFlow("fine")
	require.NoError(t, store.CreateFlow(ok))
	h.BeginShutdown()
	requireLifecycleStatus(t, deploy(ok.ID), http.StatusServiceUnavailable)
}

// A redeploy that fails after the old instance stopped leaves the flow
// stopped with its running intent, which the read views show as failed.
func TestLockedRedeployFailureKeepsTheRunningIntent(t *testing.T) {
	server, store := createTestServer(t)
	h := server.handler
	t.Cleanup(h.manager.StopAll)
	f := createTestFlow("redeploy")
	require.NoError(t, store.CreateFlow(f))
	require.NoError(t, lifecycleCall(t, h, func(l service.LockedLifecycle) error {
		_, err := l.Deploy(context.Background(), f.ID, agentDeployer, false)
		return err
	}))
	f.AddNode(*flow.NewNode("no-such-type", flow.NodeCategoryProcessing))
	require.NoError(t, store.UpdateFlow(f))
	err := lifecycleCall(t, h, func(l service.LockedLifecycle) error {
		_, err := l.Deploy(context.Background(), f.ID, agentDeployer, true)
		return err
	})
	requireLifecycleStatus(t, err, http.StatusBadRequest)
	_, running := h.manager.GetRuntime(f.ID)
	assert.False(t, running)
	stored, err := store.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, flow.FlowStateRunning, stored.State)
	d, err := store.GetDeployment(f.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, d.Version, "the marker names the last successful deployment")
}

func TestLockedStop(t *testing.T) {
	server, store := createTestServer(t)
	h := server.handler
	t.Cleanup(h.manager.StopAll)
	f := createTestFlow("stop me")
	require.NoError(t, store.CreateFlow(f))
	stop := func(id string) error {
		return lifecycleCall(t, h, func(l service.LockedLifecycle) error {
			return l.Stop(context.Background(), id, agentDeployer)
		})
	}
	requireLifecycleStatus(t, stop(f.ID), http.StatusConflict)
	requireLifecycleStatus(t, stop("nope"), http.StatusNotFound)

	require.NoError(t, lifecycleCall(t, h, func(l service.LockedLifecycle) error {
		_, err := l.Deploy(context.Background(), f.ID, agentDeployer, false)
		return err
	}))
	h.BeginShutdown() // stopping stays allowed
	require.NoError(t, stop(f.ID))
	_, running := h.manager.GetRuntime(f.ID)
	assert.False(t, running)
	stored, err := store.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, flow.FlowStateInactive, stored.State)
	d, err := store.GetDeployment(f.ID)
	require.NoError(t, err)
	require.NotNil(t, d, "the marker names the last deployment")
	assert.Equal(t, storage.VersionActorAgent, d.ActorKind)
}

// The editor's own deploys record the editor as the deployer.
func TestEditorDeploysRecordTheDeployment(t *testing.T) {
	server, store := createTestServer(t)
	t.Cleanup(server.handler.manager.StopAll)
	f := createTestFlow("editor deploy")
	require.NoError(t, store.CreateFlow(f))
	require.NoError(t, store.RecordDeployment(f.ID, storage.Deployment{Version: 1, ActorKind: storage.VersionActorAgent, TokenID: "t", TokenName: "old"}))

	rr := performRequest(server, http.MethodPost, "/api/flows/"+f.ID+"/deploy", nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	d, err := store.GetDeployment(f.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.VersionActorEditor, d.ActorKind)
	assert.Empty(t, d.TokenName)

	rr = performRequest(server, http.MethodPost, "/api/flows/"+f.ID+"/stop", nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	require.NoError(t, store.RecordDeployment(f.ID, storage.Deployment{Version: 1, ActorKind: storage.VersionActorAgent, TokenID: "t", TokenName: "old"}))
	rr = performRequest(server, http.MethodPatch, "/api/flows/"+f.ID+"/toggle", map[string]any{"enabled": true})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	d, err = store.GetDeployment(f.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.VersionActorEditor, d.ActorKind, "toggling on deploys too")
}

// GET /api/flows and GET /api/flows/{id} carry the deployment marker and
// whether the current version was not approved by the admin (H6).
func TestFlowReadsCarryTheMarks(t *testing.T) {
	server, store := createTestServer(t)
	plain := createTestFlow("plain")
	require.NoError(t, store.CreateFlow(plain))
	agent := createTestFlow("agent changed")
	require.NoError(t, store.CreateFlow(agent))
	agent.Name = "agent version"
	_, err := store.UpdateFlowVersioned(agent, 0, storage.VersionMeta{ActorKind: storage.VersionActorAgent, TokenID: "t", TokenName: "claude"})
	require.NoError(t, err)
	require.NoError(t, store.RecordDeployment(agent.ID, storage.Deployment{Version: 2, ActorKind: storage.VersionActorAgent, TokenID: "t", TokenName: "claude"}))

	type marked struct {
		ID         string `json:"id"`
		Version    int    `json:"version"`
		Deployment *struct {
			Version   int    `json:"version"`
			ActorKind string `json:"actorKind"`
			TokenName string `json:"tokenName"`
			TokenID   string `json:"tokenId"`
		} `json:"deployment"`
		AgentChanged bool `json:"agentChanged"`
	}
	rr := performRequest(server, http.MethodGet, "/api/flows", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var list struct {
		Data struct {
			Flows []marked `json:"flows"`
			Total int      `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &list))
	require.Len(t, list.Data.Flows, 2)
	byID := map[string]marked{}
	for _, f := range list.Data.Flows {
		byID[f.ID] = f
	}
	assert.Nil(t, byID[plain.ID].Deployment)
	assert.False(t, byID[plain.ID].AgentChanged)
	got := byID[agent.ID]
	assert.Equal(t, 2, got.Version)
	assert.True(t, got.AgentChanged)
	require.NotNil(t, got.Deployment)
	assert.Equal(t, 2, got.Deployment.Version)
	assert.Equal(t, "agent", got.Deployment.ActorKind)
	assert.Equal(t, "claude", got.Deployment.TokenName)
	assert.Empty(t, got.Deployment.TokenID, "the editor needs the name only")

	rr = performRequest(server, http.MethodGet, "/api/flows/"+agent.ID, nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var one struct {
		Data struct {
			Flow    marked `json:"flow"`
			Running bool   `json:"running"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &one))
	assert.True(t, one.Data.Flow.AgentChanged)
	require.NotNil(t, one.Data.Flow.Deployment)
	assert.Equal(t, "claude", one.Data.Flow.Deployment.TokenName)
}
