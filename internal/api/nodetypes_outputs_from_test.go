package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A type whose outputs come from a setting (ADR-0040) says which one, so
// the editor renders a port per item of the node's config; other types
// leave the field out.
func TestNodeTypesCarryOutputsFrom(t *testing.T) {
	registry := createTestRegistry()
	require.NoError(t, registry.Register(node.NodeTypeInfo{
		Type: "mock-sort", Name: "Sort", Category: flow.NodeCategoryProcessing, Risk: node.RiskSafe,
		Inputs:      []flow.Port{{ID: "input", Name: "Input", DataType: flow.DataTypeAny}},
		Outputs:     []flow.Port{{ID: "other", Name: "Other", DataType: flow.DataTypeAny}},
		OutputsFrom: "labels",
		Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
			"labels": {Type: "array", Title: "Labels", AgentVisible: true},
		}},
	}, func(map[string]any) (node.Node, error) { return &mockNode{}, nil }))

	r := mux.NewRouter()
	NewNodeTypesHandler(registry, zerolog.Nop()).RegisterRoutes(r)
	get := func(path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		return rr
	}

	rr := get("/api/node-types")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var list struct {
		Data struct {
			NodeTypes []map[string]json.RawMessage `json:"nodeTypes"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &list))
	seen := 0
	for _, nt := range list.Data.NodeTypes {
		var typ string
		require.NoError(t, json.Unmarshal(nt["type"], &typ))
		if typ == "mock-sort" {
			seen++
			assert.JSONEq(t, `"labels"`, string(nt["outputsFrom"]))
			continue
		}
		_, has := nt["outputsFrom"]
		assert.False(t, has, "%s has no outputs from a setting", typ)
	}
	assert.Equal(t, 1, seen)

	rr = get("/api/node-types/mock-sort")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), `"outputsFrom":"labels"`)
}
