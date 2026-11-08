package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListRunsIncludesActor: the run JSON carries who started a called run.
func TestListRunsIncludesActor(t *testing.T) {
	router, store, _ := newEventsRouter(t)
	now := time.Now()
	require.NoError(t, store.UpsertFlowRunWithActor("called", "f1", "token:bot", now.Add(time.Second)))
	require.NoError(t, store.UpsertFlowRun("plain", "f1", now))

	req := httptest.NewRequest(http.MethodGet, "/api/flows/f1/runs", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp struct {
		Data struct {
			Runs []map[string]any `json:"runs"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Data.Runs, 2)
	assert.Equal(t, "called", resp.Data.Runs[0]["id"])
	assert.Equal(t, "token:bot", resp.Data.Runs[0]["actor"])
	assert.Equal(t, "plain", resp.Data.Runs[1]["id"])
	assert.NotContains(t, resp.Data.Runs[1], "actor", "runs without an actor omit the field")
}
