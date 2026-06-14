package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock nodes for the test-node endpoint ---

type echoNode struct{}

func (echoNode) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	out := msg.Clone()
	out.SourcePort = "output"
	return []*types.Message{out}, nil
}
func (echoNode) Ports() (inputs, outputs []types.Port) {
	return []types.Port{{ID: "input", DataType: types.DataTypeAny}}, []types.Port{{ID: "output", DataType: types.DataTypeAny}}
}
func (echoNode) Validate() error { return nil }

type badValidateNode struct{ echoNode }

func (badValidateNode) Validate() error { return errors.New("bad config") }

type panicNode struct{ echoNode }

func (panicNode) Process(context.Context, *types.Message) ([]*types.Message, error) {
	panic("kaboom")
}

func testNodeRouter(t *testing.T) *mux.Router {
	t.Helper()
	registry := node.NewRegistry()
	require.NoError(t, registry.Register(node.NodeTypeInfo{
		Type: "process-echo", Name: "Echo", Category: types.NodeCategoryProcessing,
	}, func(map[string]interface{}) (node.Node, error) { return echoNode{}, nil }))
	require.NoError(t, registry.Register(node.NodeTypeInfo{
		Type: "process-bad", Name: "Bad", Category: types.NodeCategoryProcessing,
	}, func(map[string]interface{}) (node.Node, error) { return badValidateNode{}, nil }))
	require.NoError(t, registry.Register(node.NodeTypeInfo{
		Type: "process-panic", Name: "Panic", Category: types.NodeCategoryProcessing,
	}, func(map[string]interface{}) (node.Node, error) { return panicNode{}, nil }))
	require.NoError(t, registry.Register(node.NodeTypeInfo{
		Type: "action-side", Name: "Side", Category: types.NodeCategoryOutput,
	}, func(map[string]interface{}) (node.Node, error) { return echoNode{}, nil }))

	h := NewNodeTypesHandler(registry, zerolog.Nop())
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	return router
}

func postTestNode(t *testing.T, router *mux.Router, typ string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/"+typ+"/test", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func TestTestNodeRunsProcessingNode(t *testing.T) {
	router := testNodeRouter(t)
	rr := postTestNode(t, router, "process-echo", map[string]interface{}{
		"config":  map[string]interface{}{},
		"payload": "hello",
	})

	require.Equal(t, http.StatusOK, rr.Code)
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Outputs []*types.Message `json:"outputs"`
			Error   string           `json:"error"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.True(t, resp.Success)
	require.Len(t, resp.Data.Outputs, 1)
	assert.Equal(t, "hello", resp.Data.Outputs[0].Payload)
	assert.Empty(t, resp.Data.Error)
}

func TestTestNodeUnknownTypeIs404(t *testing.T) {
	router := testNodeRouter(t)
	rr := postTestNode(t, router, "process-nope", map[string]interface{}{"payload": 1})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestTestNodeRejectsNonProcessing(t *testing.T) {
	router := testNodeRouter(t)
	rr := postTestNode(t, router, "action-side", map[string]interface{}{"payload": 1})
	assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
}

func TestTestNodeInvalidConfigIs400(t *testing.T) {
	router := testNodeRouter(t)
	rr := postTestNode(t, router, "process-bad", map[string]interface{}{"payload": 1})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestTestNodeRecoversPanic(t *testing.T) {
	router := testNodeRouter(t)
	assert.NotPanics(t, func() {
		rr := postTestNode(t, router, "process-panic", map[string]interface{}{"payload": 1})
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
	})
}
