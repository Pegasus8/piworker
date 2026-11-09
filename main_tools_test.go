package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// saveToolFlow stores trigger-tool "tool" → steps (each fed by the tool).
func saveToolFlow(t *testing.T, app testApp, toolCfg map[string]any, steps ...flow.Node) *flow.Flow {
	t.Helper()
	f := flow.NewFlow("Garage")
	tool := flow.NewNode("trigger-tool", flow.NodeCategoryInput)
	tool.ID = "tool"
	tool.Config = map[string]any{
		"toolName":    "main_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		"description": "Opens the left or right garage door and reports whether it moved.",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"door": map[string]any{"type": "string", "enum": []any{"left", "right"}}},
		},
	}
	for k, v := range toolCfg {
		tool.Config[k] = v
	}
	f.AddNode(*tool)
	if len(steps) == 0 {
		reply := flow.NewNode("action-reply", flow.NodeCategoryOutput)
		reply.ID = "reply"
		steps = []flow.Node{*reply}
	}
	for _, s := range steps {
		f.AddNode(s)
		f.AddConnection(*flow.NewConnection("tool", "output", s.ID, "input"))
	}
	require.NoError(t, app.store.CreateFlow(f))
	return f
}

func withoutHeader(h map[string]string, name string) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		if k != name {
			out[k] = v
		}
	}
	return out
}

func TestToolCallAndToolListThroughTheRouter(t *testing.T) {
	app := newTestApp(t, true)
	headers := app.login(t)
	f := saveToolFlow(t, app, nil)
	w := app.do("POST", "/api/flows/"+f.ID+"/deploy", "", "", headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	path := "/api/flows/" + f.ID + "/call/tool"
	body := `{"args":{"door":"left"}}`
	w = app.do("POST", path, "application/json", body, headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var call struct {
		Data flow.CallResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &call))
	assert.Equal(t, flow.CallStatusCompleted, call.Data.Status)
	assert.JSONEq(t, `{"door":"left"}`, string(call.Data.Result))

	assert.Equal(t, http.StatusUnauthorized, app.do("POST", path, "application/json", body, editorHeaders).Code, "needs the session")
	assert.Equal(t, http.StatusForbidden, app.do("POST", path, "application/json", body, withoutHeader(headers, "X-PiWorker-Request")).Code, "needs the CSRF header")

	w = app.do("GET", "/api/agent/tools", "", "", headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var list struct {
		Data struct {
			Version uint64 `json:"version"`
			Tools   []struct {
				FlowID   string `json:"flowId"`
				FlowName string `json:"flowName"`
				ToolName string `json:"toolName"`
				Risk     string `json:"risk"`
			} `json:"tools"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list.Data.Tools, 1)
	assert.Equal(t, f.ID, list.Data.Tools[0].FlowID)
	assert.Equal(t, "Garage", list.Data.Tools[0].FlowName)
	assert.Equal(t, f.Nodes[0].Config["toolName"], list.Data.Tools[0].ToolName)
	assert.Equal(t, "safe", list.Data.Tools[0].Risk)
	assert.Positive(t, list.Data.Version)
	assert.Equal(t, http.StatusUnauthorized, app.do("GET", "/api/agent/tools", "", "", nil).Code)
}

func TestToolCallWithoutAuth(t *testing.T) {
	app := newTestApp(t, false)
	f := saveToolFlow(t, app, nil)
	require.NoError(t, app.manager.Deploy(context.Background(), f))
	w := app.do("POST", "/api/flows/"+f.ID+"/call/tool", "application/json", `{"args":{"door":"right"}}`, editorHeaders)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = app.do("POST", "/api/flows/"+f.ID+"/call/tool", "application/json", `{"args":{"door":"up"}}`, editorHeaders)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"code":"invalid_arguments"`)
}

// The risk analysis answers 400 through the deploy route.
func TestDeployRefusesToolSecurityIssues(t *testing.T) {
	app := newTestApp(t, false)
	cmd := flow.NewNode("action-command", flow.NodeCategoryOutput)
	cmd.ID, cmd.Config = "cmd", map[string]any{"command": "true"}
	f := saveToolFlow(t, app, map[string]any{"inputSchema": map[string]any{
		"type":       "object",
		"properties": map[string]any{"note": map[string]any{"type": "string"}},
	}}, *cmd)
	w := app.do("POST", "/api/flows/"+f.ID+"/deploy", "", "", editorHeaders)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "inputSchema")
	assert.Contains(t, w.Body.String(), "/note")
	assert.Empty(t, app.manager.ListRunning())
}

// Approving a queued tool call runs it as the requesting token and answers
// its result.
func TestApprovingAToolCallRunsIt(t *testing.T) {
	app := newTestApp(t, true)
	headers := app.login(t)
	f := saveToolFlow(t, app, map[string]any{"confirm": "admin"})
	require.NoError(t, app.manager.Deploy(context.Background(), f))
	_, tools := app.manager.Tools()
	require.Len(t, tools, 1)
	tok, _, err := app.store.CreateAPIToken(storage.NewAPIToken{
		Name: "laptop", Scopes: []string{storage.ScopeCall},
		Grants: []storage.TokenGrant{{FlowID: f.ID, TriggerNodeID: "tool", Fingerprint: tools[0].Fingerprint}},
	}, time.Now())
	require.NoError(t, err)
	a, err := app.store.CreateAgentApproval(storage.AgentApproval{
		Kind: storage.ApprovalKindToolCall, TokenID: tok.ID, FlowID: f.ID, Ref: "tool",
		ToolName: tools[0].ToolName, ToolFingerprint: tools[0].Fingerprint, ToolRisk: string(tools[0].Risk),
		Args: json.RawMessage(`{"door":"right"}`), Summary: "garage",
	}, time.Now())
	require.NoError(t, err)

	w := app.do("POST", "/api/agent/approvals/"+a.ID+"/approve", "", "", headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var decision struct {
		Data struct {
			Executed       bool   `json:"executed"`
			ExecutionError string `json:"executionError"`
			Approval       struct {
				Result flow.CallResult `json:"result"`
			} `json:"approval"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &decision))
	assert.True(t, decision.Data.Executed, decision.Data.ExecutionError)
	assert.Equal(t, flow.CallStatusCompleted, decision.Data.Approval.Result.Status)
	assert.JSONEq(t, `{"door":"right"}`, string(decision.Data.Approval.Result.Result))
}

// Flushers (the MCP audit) run after the flows stopped, while the store is
// still open.
func TestGracefulShutdownRunsFlushersLast(t *testing.T) {
	app := newTestApp(t, false)
	f := manualFlow(t, app)
	require.NoError(t, app.manager.Deploy(context.Background(), f))
	running := -1
	flushed := false
	err := gracefulShutdown(nil, app.flows, app.manager, time.Second, func(context.Context) error {
		running = len(app.manager.ListRunning())
		flushed = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, flushed)
	assert.Equal(t, 0, running)
}
