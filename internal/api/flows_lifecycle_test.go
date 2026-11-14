package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeFlow pulls the Flow out of a {success,data} envelope.
func decodeFlow(t *testing.T, body []byte) *flow.Flow {
	t.Helper()
	var resp struct {
		Success bool      `json:"success"`
		Data    flow.Flow `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	require.True(t, resp.Success)
	return &resp.Data
}

func TestDuplicateFlow(t *testing.T) {
	server, store := createTestServer(t)
	src := createTestFlow("Original")
	require.NoError(t, store.CreateFlow(src))

	rr := performRequest(server, http.MethodPost, "/api/flows/"+src.ID+"/duplicate", nil)
	require.Equal(t, http.StatusCreated, rr.Code)

	dup := decodeFlow(t, rr.Body.Bytes())
	assert.NotEqual(t, src.ID, dup.ID, "duplicate must get a fresh ID")
	assert.Len(t, dup.Nodes, len(src.Nodes))
	assert.Contains(t, dup.Name, "Original")
	assert.Equal(t, flow.FlowStateInactive, dup.State)

	// Both flows now exist.
	flows, err := store.ListFlows(nil)
	require.NoError(t, err)
	assert.Len(t, flows, 2)
}

func TestDuplicateMissingFlowIs404(t *testing.T) {
	server, _ := createTestServer(t)
	rr := performRequest(server, http.MethodPost, "/api/flows/nope/duplicate", nil)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestExportFlow(t *testing.T) {
	server, store := createTestServer(t)
	src := createTestFlow("Exportable")
	require.NoError(t, store.CreateFlow(src))

	rr := performRequest(server, http.MethodGet, "/api/flows/"+src.ID+"/export", nil)
	require.Equal(t, http.StatusOK, rr.Code)

	exported := decodeFlow(t, rr.Body.Bytes())
	assert.Equal(t, src.ID, exported.ID)
	assert.Len(t, exported.Nodes, len(src.Nodes))
}

func TestImportFlowAssignsFreshID(t *testing.T) {
	server, store := createTestServer(t)
	src := createTestFlow("Imported")
	src.ID = "some-foreign-id"

	rr := performRequest(server, http.MethodPost, "/api/flows/import", src)
	require.Equal(t, http.StatusCreated, rr.Code)

	imported := decodeFlow(t, rr.Body.Bytes())
	assert.NotEqual(t, "some-foreign-id", imported.ID, "import must not reuse the incoming ID")
	assert.Equal(t, flow.FlowStateInactive, imported.State)

	// It was actually persisted under the new ID.
	got, err := store.GetFlow(imported.ID)
	require.NoError(t, err)
	assert.Len(t, got.Nodes, len(src.Nodes))
}

func TestImportFlowInvalidBodyIs400(t *testing.T) {
	server, _ := createTestServer(t)
	rr := performRequest(server, http.MethodPost, "/api/flows/import", "not a flow")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}
