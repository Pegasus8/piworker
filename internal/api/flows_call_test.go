package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/toolschema"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCaller records the call and answers with res/err.
type fakeCaller struct {
	actor service.Actor
	ref   service.ToolRef
	args  map[string]any
	calls int
	res   flow.CallResult
	err   error
}

func (f *fakeCaller) Call(_ context.Context, actor service.Actor, ref service.ToolRef, args map[string]any) (flow.CallResult, error) {
	f.calls++
	f.actor, f.ref, f.args = actor, ref, args
	return f.res, f.err
}

func toolCallRouter(caller ToolCaller) *mux.Router {
	r := mux.NewRouter()
	NewToolCallHandler(caller, zerolog.Nop()).RegisterRoutes(r)
	return r
}

func postCall(r http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestToolCallUsesTheEditorActor(t *testing.T) {
	caller := &fakeCaller{res: flow.CallResult{Status: flow.CallStatusCompleted, RunID: "run-1", Result: json.RawMessage(`{"ok":true}`), DurationMs: 3}}
	w := postCall(toolCallRouter(caller), "/api/flows/f1/call/tool", `{"args":{"door":"left","n":2}}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, service.ActorEditor, caller.actor.Kind)
	assert.Equal(t, service.ToolByNode("f1", "tool"), caller.ref)
	assert.Equal(t, map[string]any{"door": "left", "n": float64(2)}, caller.args)
	assert.JSONEq(t, `{"success":true,"data":{"status":"completed","runId":"run-1","result":{"ok":true},"durationMs":3}}`, w.Body.String())
}

func TestToolCallBodies(t *testing.T) {
	caller := &fakeCaller{res: flow.CallResult{Status: flow.CallStatusCompleted, RunID: "r"}}
	r := toolCallRouter(caller)

	w := postCall(r, "/api/flows/f1/call/tool", "")
	require.Equal(t, http.StatusOK, w.Code, "no body means no arguments")
	assert.Equal(t, map[string]any{}, caller.args)

	w = postCall(r, "/api/flows/f1/call/tool", `{}`)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, map[string]any{}, caller.args)

	for _, bad := range []string{`{"args":[1]}`, `{"args":"x"}`, `{"arguments":{}}`, `{`, `[]`} {
		before := caller.calls
		w = postCall(r, "/api/flows/f1/call/tool", bad)
		assert.Equal(t, http.StatusBadRequest, w.Code, bad)
		assert.Equal(t, before, caller.calls, "nothing called for %s", bad)
	}
}

func TestToolCallErrorStatuses(t *testing.T) {
	issues := []toolschema.Issue{{Pointer: "/door", Message: "must be one of left, right"}}
	flowIssues := []flow.Issue{{Path: "/nodes/http", Code: flow.IssueInvalidConfig, Severity: flow.IssueError, Message: "url is required"}}
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{&service.ToolError{Kind: service.ErrToolNotFound, Message: "unknown tool"}, http.StatusNotFound, "not_found"},
		{&service.ToolError{Kind: service.ErrPermissionDenied, Message: "no"}, http.StatusForbidden, "forbidden"},
		{&service.ToolError{Kind: service.ErrReconfirmRequired, Message: "re-confirm"}, http.StatusConflict, "reconfirm_required"},
		{&service.ToolError{Kind: service.ErrInvalidArguments, Message: "bad", Issues: issues}, http.StatusBadRequest, "invalid_arguments"},
		{&service.ToolError{Kind: service.ErrBusy, Message: "busy"}, http.StatusTooManyRequests, "busy"},
		{&service.ToolError{Kind: service.ErrCooldown, Message: "wait", RetryAfter: 1500 * time.Millisecond}, http.StatusTooManyRequests, "cooldown"},
		{&service.ToolError{Kind: service.ErrUnavailable, Message: "stopped"}, http.StatusServiceUnavailable, "unavailable"},
		{&service.ToolError{Kind: service.ErrConflict, Message: "changed", CurrentVersion: 4}, http.StatusConflict, "conflict"},
		{&service.ToolError{Kind: service.ErrInvalidArguments, Message: "invalid flow", FlowIssues: flowIssues}, http.StatusBadRequest, "invalid_arguments"},
		{&service.ToolError{Kind: service.ErrFlowNotFound, Message: "unknown flow"}, http.StatusNotFound, "flow_not_found"},
		{errors.New("database exploded"), http.StatusInternalServerError, ""},
	}
	for _, tc := range cases {
		caller := &fakeCaller{err: tc.err}
		w := postCall(toolCallRouter(caller), "/api/flows/f1/call/tool", `{"args":{}}`)
		assert.Equal(t, tc.status, w.Code, "%v", tc.err)
		var body struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
			Data    struct {
				Code           string             `json:"code"`
				Issues         []toolschema.Issue `json:"issues"`
				FlowIssues     []flow.Issue       `json:"flowIssues"`
				CurrentVersion int                `json:"currentVersion"`
				RetryAfterMs   int64              `json:"retryAfterMs"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
		assert.False(t, body.Success)
		assert.Equal(t, tc.code, body.Data.Code)
		if tc.code == "" {
			assert.NotContains(t, body.Error, "exploded", "internal errors are not echoed")
			continue
		}
		assert.Equal(t, tc.err.Error(), body.Error)
		var te *service.ToolError
		require.ErrorAs(t, tc.err, &te)
		assert.Equal(t, te.Issues, body.Data.Issues)
		assert.Equal(t, te.FlowIssues, body.Data.FlowIssues)
		assert.Equal(t, te.CurrentVersion, body.Data.CurrentVersion)
		if tc.code == "cooldown" {
			assert.Equal(t, int64(1500), body.Data.RetryAfterMs)
			assert.Equal(t, "2", w.Header().Get("Retry-After"), "rounded up to whole seconds")
		}
	}
}

// End to end through the real service and runtime.
func TestToolCallRunsTheTool(t *testing.T) {
	manager := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	t.Cleanup(manager.StopAll)
	tools := service.NewTools(service.Options{Runtimes: manager})
	f := toolTestFlow("Garage")
	require.NoError(t, manager.Deploy(context.Background(), f))

	r := toolCallRouter(tools)
	w := postCall(r, "/api/flows/"+f.ID+"/call/tool", `{"args":{"door":"left"}}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		Data flow.CallResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, flow.CallStatusCompleted, body.Data.Status)
	assert.JSONEq(t, `{"door":"left"}`, string(body.Data.Result))

	w = postCall(r, "/api/flows/"+f.ID+"/call/tool", `{"args":{"door":"garden"}}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"pointer":"/door"`)

	w = postCall(r, "/api/flows/"+f.ID+"/call/missing", `{"args":{}}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// toolTestFlow is trigger-tool "tool" → action-reply.
func toolTestFlow(name string) *flow.Flow {
	f := flow.NewFlow(name)
	tool := flow.NewNode("trigger-tool", flow.NodeCategoryInput)
	tool.ID = "tool"
	tool.Config = map[string]any{
		"toolName":    "api_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		"title":       "Open " + name,
		"description": "Opens the left or right door and reports whether it moved.",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []any{"door"},
			"properties": map[string]any{
				"door": map[string]any{"type": "string", "enum": []any{"left", "right"}},
			},
		},
	}
	reply := flow.NewNode("action-reply", flow.NodeCategoryOutput)
	reply.ID = "reply"
	f.AddNode(*tool)
	f.AddNode(*reply)
	f.AddConnection(*flow.NewConnection("tool", "output", "reply", "input"))
	return f
}

func getJSON(t *testing.T, r http.Handler, path string) []byte {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return bytes.TrimSpace(w.Body.Bytes())
}
