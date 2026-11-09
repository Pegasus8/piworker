package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The flow version (docs/design/mcp.md §10): listed and read with the flow,
// checked on save when the editor sends the version it loaded, and bumped by
// definition changes only.

func TestFlowReadsIncludeTheVersion(t *testing.T) {
	server, store := createTestServer(t)
	f := createTestFlow("versioned")
	require.NoError(t, store.CreateFlow(f))
	require.NoError(t, store.UpdateFlow(f))

	rr := performRequest(server, http.MethodGet, "/api/flows/"+f.ID, nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var one struct {
		Data struct {
			Flow struct {
				Version int `json:"version"`
			} `json:"flow"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &one))
	assert.Equal(t, 2, one.Data.Flow.Version)

	rr = performRequest(server, http.MethodGet, "/api/flows", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var list struct {
		Data struct {
			Flows []struct {
				Version int `json:"version"`
			} `json:"flows"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &list))
	require.Len(t, list.Data.Flows, 1)
	assert.Equal(t, 2, list.Data.Flows[0].Version)
}

func TestUpdateFlowWithTheLoadedVersion(t *testing.T) {
	server, store := createTestServer(t)
	f := createTestFlow("v1")
	require.NoError(t, store.CreateFlow(f))

	rr := performRequest(server, http.MethodPut, "/api/flows/"+f.ID, map[string]any{"name": "v2", "version": 1})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	saved := decodeFlow(t, rr.Body.Bytes())
	assert.Equal(t, 2, saved.Version, "the response carries the new version")

	stored, err := store.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, "v2", stored.Name)
	assert.Equal(t, 2, stored.Version)
}

func TestUpdateFlowWithAStaleVersionConflicts(t *testing.T) {
	server, store := createTestServer(t)
	f := createTestFlow("original")
	require.NoError(t, store.CreateFlow(f))
	f.Name = "changed elsewhere"
	require.NoError(t, store.UpdateFlow(f)) // version 2

	rr := performRequest(server, http.MethodPut, "/api/flows/"+f.ID, map[string]any{"name": "overwrite", "version": 1})
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Data    struct {
			CurrentVersion int `json:"currentVersion"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.False(t, resp.Success)
	assert.NotEmpty(t, resp.Error)
	assert.Equal(t, 2, resp.Data.CurrentVersion)

	stored, err := store.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, "changed elsewhere", stored.Name, "never a silent overwrite")
	assert.Equal(t, 2, stored.Version)
}

// Scripts that never sent a version keep working: no check.
func TestUpdateFlowWithoutAVersionIsNotChecked(t *testing.T) {
	server, store := createTestServer(t)
	f := createTestFlow("script")
	require.NoError(t, store.CreateFlow(f))
	require.NoError(t, store.UpdateFlow(f))

	rr := performRequest(server, http.MethodPut, "/api/flows/"+f.ID, map[string]any{"name": "from a script"})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, 3, decodeFlow(t, rr.Body.Bytes()).Version)
}

// Creating ignores a client-supplied version.
func TestCreateFlowStartsAtVersionOne(t *testing.T) {
	server, _ := createTestServer(t)
	rr := performRequest(server, http.MethodPost, "/api/flows", map[string]any{"name": "new", "version": 9})
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	assert.Equal(t, 1, decodeFlow(t, rr.Body.Bytes()).Version)
}

// Deploying, stopping and toggling change the flow's state, not its
// definition: no new version.
func TestLifecycleChangesDoNotBumpTheVersion(t *testing.T) {
	server, store := createTestServer(t)
	f := createTestFlow("lifecycle")
	require.NoError(t, store.CreateFlow(f))

	for _, step := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/deploy", nil},
		{http.MethodPost, "/stop", nil},
		{http.MethodPatch, "/toggle", map[string]any{"enabled": true}},
		{http.MethodPatch, "/toggle", map[string]any{"enabled": false}},
	} {
		rr := performRequest(server, step.method, "/api/flows/"+f.ID+step.path, step.body)
		require.Equal(t, http.StatusOK, rr.Code, "%s %s: %s", step.method, step.path, rr.Body.String())
	}

	stored, err := store.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, stored.Version)
	versions, err := store.ListFlowVersions(f.ID, 10)
	require.NoError(t, err)
	assert.Len(t, versions, 1)
}
