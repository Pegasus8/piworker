package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAssistant struct {
	got service.AssistantFlowRequest
	res service.AssistantFlowResult
	err error
}

func (f *fakeAssistant) ProposeFlow(_ context.Context, req service.AssistantFlowRequest) (service.AssistantFlowResult, error) {
	f.got = req
	return f.res, f.err
}

func assistantRouter(svc FlowAssistant) *mux.Router {
	r := mux.NewRouter()
	NewAssistantHandler(svc, zerolog.Nop()).RegisterRoutes(r)
	return r
}

func TestAssistantHandlerPassesTheRequest(t *testing.T) {
	svc := &fakeAssistant{res: service.AssistantFlowResult{
		Flow:   service.AssistantFlow{Name: "n", Nodes: []flow.Node{}, Connections: []flow.Connection{}},
		Notes:  "define X",
		Issues: []flow.Issue{},
		Usage:  llm.Usage{InputTokens: 1, OutputTokens: 2, Requests: 1},
	}}
	rr, env := doJSON(t, assistantRouter(svc), http.MethodPost, "/api/llm/assistant/flow",
		`{"connection":"local","model":"m","description":"do it","flow":{"name":"cur","description":"",
		"nodes":[{"id":"a","type":"t","position":{"x":1,"y":2},"config":{"k":"v"}}],"connections":[]}}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "local", svc.got.Connection)
	assert.Equal(t, "m", svc.got.Model)
	assert.Equal(t, "do it", svc.got.Description)
	require.NotNil(t, svc.got.Flow)
	require.Len(t, svc.got.Flow.Nodes, 1)
	assert.Equal(t, "a", svc.got.Flow.Nodes[0].ID)
	assert.True(t, svc.got.Flow.Nodes[0].Enabled, "absent enabled is true, as for stored flows")
	assert.Equal(t, map[string]any{
		"flow":   map[string]any{"name": "n", "description": "", "nodes": []any{}, "connections": []any{}},
		"notes":  "define X",
		"issues": []any{},
		"usage":  map[string]any{"inputTokens": float64(1), "outputTokens": float64(2), "requests": float64(1)},
	}, env["data"])
}

// Refusals and failures map to statuses the editor can tell apart; the
// message is the service's (never an internal error's).
func TestAssistantHandlerStatuses(t *testing.T) {
	for _, c := range []struct {
		err     error
		status  int
		message string
	}{
		{&service.ToolError{Kind: service.ErrInvalidArguments, Message: "describe the flow"}, http.StatusBadRequest, "describe the flow"},
		{&service.ToolError{Kind: service.ErrBusy, Message: "the flow assistant is busy"}, http.StatusConflict, "the flow assistant is busy"},
		{fmt.Errorf("%w: LLM connection \"c\": %w", service.ErrAssistantModel, llm.ErrBudgetExhausted), http.StatusTooManyRequests, "daily token limit"},
		{fmt.Errorf("%w: LLM request: %w", service.ErrAssistantModel, context.DeadlineExceeded), http.StatusGatewayTimeout, "3 minutes"},
		{fmt.Errorf("%w: %w", service.ErrAssistantModel, &llm.APIError{Status: 401, Message: "invalid x-api-key"}), http.StatusBadGateway, "invalid x-api-key"},
		{fmt.Errorf("%w: %w: not JSON", service.ErrAssistantModel, llm.ErrInvalidAnswer), http.StatusBadGateway, "not JSON"},
		{errors.New("database is locked"), http.StatusInternalServerError, "The flow assistant failed"},
	} {
		rr, env := doJSON(t, assistantRouter(&fakeAssistant{err: c.err}), http.MethodPost, "/api/llm/assistant/flow",
			`{"connection":"c","description":"x"}`)
		assert.Equal(t, c.status, rr.Code, c.err.Error())
		assert.Equal(t, false, env["success"])
		assert.Contains(t, env["error"], c.message)
		assert.NotContains(t, env["error"], "database is locked")
	}
}

func TestAssistantHandlerRejectsABadBody(t *testing.T) {
	svc := &fakeAssistant{}
	for _, body := range []string{`not json`, `{"connection":"c","description":"x","flow":{"nodes":"no"}}`} {
		rr, _ := doJSON(t, assistantRouter(svc), http.MethodPost, "/api/llm/assistant/flow", body)
		assert.Equal(t, http.StatusBadRequest, rr.Code, body)
	}
	assert.Empty(t, svc.got.Connection, "the service is not called")
}

type assistantTestTrigger struct{ mockTriggerNode }

func (assistantTestTrigger) Ports() ([]types.Port, []types.Port) {
	return nil, []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}}
}

// End to end with the real service and a fake OpenAI-compatible server:
// the proposal comes back in the JSON the editor reads.
func TestAssistantHandlerEndToEnd(t *testing.T) {
	answer := `{"name":"Tick","description":"d","notes":["Check the interval.","Then deploy."],
"nodes":[{"id":"t","type":"mock-trigger"}],"connections":[]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "m",
			"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": answer}}},
			"usage":   map[string]any{"prompt_tokens": 7, "completion_tokens": 3},
		})
	}))
	t.Cleanup(srv.Close)
	reg := node.NewRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{
		Type: "mock-trigger", Name: "Mock", Category: flow.NodeCategoryInput, Risk: node.RiskSafe,
		Outputs: []flow.Port{{ID: "output", Name: "Output", DataType: flow.DataTypeAny}},
	}, func(map[string]any) (node.Node, error) { return &assistantTestTrigger{}, nil }))
	conns := llm.NewStore()
	require.NoError(t, conns.Set(llm.Connection{Name: "local", Provider: llm.ProviderOpenAI, BaseURL: srv.URL, Model: "m"}))
	svc := service.NewAssistant(service.AssistantOptions{Registry: reg, LLM: conns})

	rr, env := doJSON(t, assistantRouter(svc), http.MethodPost, "/api/llm/assistant/flow", `{"connection":"local","description":"tick"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	data := env["data"].(map[string]any)
	assert.Equal(t, "Check the interval.\nThen deploy.", data["notes"])
	assert.Equal(t, []any{}, data["issues"])
	assert.Equal(t, map[string]any{"inputTokens": float64(7), "outputTokens": float64(3), "requests": float64(1)}, data["usage"])
	f := data["flow"].(map[string]any)
	assert.Equal(t, "Tick", f["name"])
	assert.Equal(t, []any{}, f["connections"])
	nodes := f["nodes"].([]any)
	require.Len(t, nodes, 1)
	n := nodes[0].(map[string]any)
	assert.Equal(t, "t", n["id"])
	assert.Equal(t, "mock-trigger", n["type"])
	assert.Equal(t, "input", n["category"])
	assert.Equal(t, true, n["enabled"])
	assert.Equal(t, map[string]any{}, n["config"])
	assert.Equal(t, []any{}, n["inputs"])
	assert.Equal(t, []any{map[string]any{"id": "output", "name": "Output", "dataType": "any", "multiple": false, "required": false}}, n["outputs"])
	assert.NotEqual(t, map[string]any{"x": float64(0), "y": float64(0)}, n["position"])
}
