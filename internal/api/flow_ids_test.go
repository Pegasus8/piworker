package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/require"
)

func TestCreateFlowRejectsUnsafeIDs(t *testing.T) {
	server, store := createTestServer(t)
	for _, id := range []string{"../etc", "a b", "a/b", "flow?x=1", strings.Repeat("a", 65), "é"} {
		rr := performRequest(server, "POST", "/api/flows", map[string]any{"id": id, "name": "x"})
		require.Equal(t, http.StatusBadRequest, rr.Code, "id %q", id)
	}
	flows, err := store.ListFlows(nil)
	require.NoError(t, err)
	require.Empty(t, flows)
}

func TestCreateFlowWithClientIDGetsServerTimestamps(t *testing.T) {
	server, store := createTestServer(t)
	before := time.Now().Add(-time.Second)
	rr := performRequest(server, "POST", "/api/flows", map[string]any{
		"id": "My_flow-1", "name": "x", "createdAt": "2001-01-01T00:00:00Z", "updatedAt": "2001-01-01T00:00:00Z",
	})
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	saved, err := store.GetFlow("My_flow-1")
	require.NoError(t, err)
	require.True(t, saved.CreatedAt.After(before), "createdAt must be set by the server, got %v", saved.CreatedAt)
	require.True(t, saved.UpdatedAt.After(before), "updatedAt must be set by the server, got %v", saved.UpdatedAt)
}

func TestCreateFlowWithoutIDGeneratesOne(t *testing.T) {
	server, _ := createTestServer(t)
	rr := performRequest(server, "POST", "/api/flows", map[string]any{"name": "x"})
	require.Equal(t, http.StatusCreated, rr.Code)
	var res struct {
		Data flow.Flow `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &res))
	require.True(t, validFlowID(res.Data.ID), res.Data.ID)
	require.False(t, res.Data.CreatedAt.IsZero())
}

func TestUpdateFlowRefreshesUpdatedAt(t *testing.T) {
	server, store := createTestServer(t)
	f := createTestFlow("u")
	f.CreatedAt = time.Now().Add(-time.Hour)
	f.UpdatedAt = f.CreatedAt
	require.NoError(t, store.CreateFlow(f))
	before := time.Now().Add(-time.Second)
	f.UpdatedAt = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	rr := performRequest(server, "PUT", "/api/flows/"+f.ID, f)
	require.Equal(t, http.StatusOK, rr.Code)
	saved, err := store.GetFlow(f.ID)
	require.NoError(t, err)
	require.True(t, saved.UpdatedAt.After(before), "updatedAt must reflect the edit, got %v", saved.UpdatedAt)
}

func TestFailedDeployReportsErrorAndDoesNotMarkRunning(t *testing.T) {
	server, store := createTestServer(t)
	f := createTestFlow("broken")
	f.Nodes[0].Type = "no-such-node-type"
	require.NoError(t, store.CreateFlow(f))
	for _, req := range []struct{ method, path string }{
		{"POST", "/api/flows/" + f.ID + "/deploy"},
		{"PATCH", "/api/flows/" + f.ID + "/toggle"},
	} {
		var body any
		if req.method == "PATCH" {
			body = ToggleRequest{Enabled: true}
		}
		rr := performRequest(server, req.method, req.path, body)
		require.GreaterOrEqual(t, rr.Code, 400, rr.Body.String())
		saved, err := store.GetFlow(f.ID)
		require.NoError(t, err)
		require.NotEqual(t, flow.FlowStateRunning, saved.State)
	}
}
