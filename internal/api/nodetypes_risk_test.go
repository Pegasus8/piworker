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

// The node-type list and a single node type carry the type's risk (safe,
// external, privileged), which the editor shows in the palette and the
// settings panel, as the agents and the node reference already see it.
func TestNodeTypesCarryTheirRisk(t *testing.T) {
	registry := createTestRegistry()
	require.NoError(t, registry.Register(node.NodeTypeInfo{
		Type: "mock-shell", Name: "Shell", Category: flow.NodeCategoryOutput, Risk: node.RiskPrivileged,
		Inputs: []flow.Port{{ID: "input", Name: "Input", DataType: flow.DataTypeAny}},
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
			NodeTypes []struct {
				Type string `json:"type"`
				Risk string `json:"risk"`
			} `json:"nodeTypes"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &list))
	risks := map[string]string{}
	for _, nt := range list.Data.NodeTypes {
		risks[nt.Type] = nt.Risk
	}
	assert.Equal(t, "privileged", risks["mock-shell"])

	rr = get("/api/node-types/mock-shell")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), `"risk":"privileged"`)
}
