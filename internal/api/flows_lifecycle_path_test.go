package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The lifecycle path itself (one lock, the restart intent, the deployment
// marker) is service.Lifecycle and tested there; these are the editor's
// requests over it.

// The editor's own deploys record the editor as the deployer.
func TestEditorDeploysRecordTheDeployment(t *testing.T) {
	server, store := createTestServer(t)
	t.Cleanup(server.handler.lifecycle.Runtimes().StopAll)
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

// A deployment whose marker cannot be recorded is undone and answered 500
// (service.Lifecycle tests the path; this is the editor's status).
func TestEditorDeployUndoneWhenTheMarkerFails(t *testing.T) {
	server, store := createTestServer(t)
	f := createTestFlow("unmarked")
	require.NoError(t, store.CreateFlow(f))
	_, err := store.DB().Exec(`
		CREATE TRIGGER fail_marker_insert BEFORE INSERT ON flow_deployments BEGIN SELECT RAISE(ABORT, 'marker refused'); END;`)
	require.NoError(t, err)

	rr := performRequest(server, http.MethodPost, "/api/flows/"+f.ID+"/deploy", nil)
	assert.Equal(t, http.StatusInternalServerError, rr.Code, rr.Body.String())
	assert.False(t, server.handler.lifecycle.Running(f.ID))
	d, err := store.GetDeployment(f.ID)
	require.NoError(t, err)
	assert.Nil(t, d)
}

func TestDeployInvalidFlowReturnsBadRequest(t *testing.T) {
	server, store := createTestServer(t)
	f := createTestFlow("dup")
	dup := f.Nodes[0]
	f.Nodes = append(f.Nodes, dup) // duplicate node ID
	require.NoError(t, store.CreateFlow(f))

	rr := performRequest(server, "POST", "/api/flows/"+f.ID+"/deploy", nil)
	assert.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
}
