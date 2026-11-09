package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/toolschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shape is what an MCP client sees of a result.
type shape struct {
	isError    bool
	structured string // JSON
	text       string // JSON of the single text block
}

func shapeOf(t *testing.T, res *mcp.CallToolResult) shape {
	t.Helper()
	require.NotNil(t, res)
	require.Len(t, res.Content, 1)
	text, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok, "a text block")
	if res.StructuredContent == nil {
		return shape{isError: res.IsError, text: text.Text}
	}
	structured, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	return shape{isError: res.IsError, structured: string(structured), text: text.Text}
}

// checkShape checks a result against want: an error carries its envelope
// (want.structured) as its text block only, without structured content
// (clients validate structured content against the tool's output schema
// even on errors); any other result carries it as structured content and,
// unless want.text says otherwise, as its text block too.
func checkShape(t *testing.T, want, got shape) {
	t.Helper()
	assert.Equal(t, want.isError, got.isError)
	if want.text == "" {
		want.text = want.structured
	}
	if want.isError {
		assert.Empty(t, got.structured, "an isError result carries no structured content")
		assert.JSONEq(t, want.structured, got.text)
		return
	}
	assert.JSONEq(t, want.structured, got.structured)
	assert.JSONEq(t, want.text, got.text)
}

func toolErr(kind error, msg string) *service.ToolError {
	return &service.ToolError{Kind: kind, Message: msg}
}

func TestCallResultMapping(t *testing.T) {
	nodeErr := &flow.CallError{NodeID: "cmd", NodeType: "action-command", Message: "exit status 1"}
	invalid := &service.ToolError{Kind: service.ErrInvalidArguments, Message: "the arguments do not match the tool's input schema",
		Issues: []toolschema.Issue{{Pointer: "/door", Message: "must be one of left, right"}}}
	cooldown := &service.ToolError{Kind: service.ErrCooldown, Message: "the tool is cooling down; retry in 1500 ms", RetryAfter: 1500 * time.Millisecond}

	cases := []struct {
		name string
		res  flow.CallResult
		err  error
		want shape
	}{
		{"completed with an object", flow.CallResult{Status: flow.CallStatusCompleted, RunID: "r1", Result: json.RawMessage(`{"moved":true}`)}, nil,
			shape{false, `{"moved":true}`, `{"moved":true}`}},
		{"completed with an array", flow.CallResult{Status: flow.CallStatusCompleted, RunID: "r1", Result: json.RawMessage(` [1,2]`)}, nil,
			shape{false, `{"result":[1,2]}`, `[1,2]`}},
		{"completed with a string", flow.CallResult{Status: flow.CallStatusCompleted, RunID: "r1", Result: json.RawMessage(`"ok"`)}, nil,
			shape{false, `{"result":"ok"}`, `"ok"`}},
		{"completed without a payload", flow.CallResult{Status: flow.CallStatusCompleted, RunID: "r1"}, nil,
			shape{false, `{"result":null}`, `null`}},
		{"completed without reply", flow.CallResult{Status: flow.CallStatusCompletedNoReply, RunID: "r1", DurationMs: 12}, nil,
			shape{false, `{"status":"completed_no_reply","runId":"r1","durationMs":12,"message":"The flow ran to the end without replying."}`, ""}},
		{"running", flow.CallResult{Status: flow.CallStatusRunning, RunID: "r1"}, nil,
			shape{false, `{"status":"running","runId":"r1","message":"The flow is still running. Call pw_get_result with this runId to get its result."}`, ""}},
		{"approval required", flow.CallResult{Status: flow.CallStatusApprovalRequired, RunID: "a1"}, nil,
			shape{true, `{"status":"approval_required","runId":"a1","message":"The call waits for the admin's approval in PiWorker. Call pw_get_result with this runId later to get its result."}`, ""}},
		{"flow error", flow.CallResult{Status: flow.CallStatusError, RunID: "r1", Error: nodeErr, DurationMs: 3}, nil,
			shape{true, `{"status":"error","runId":"r1","error":{"code":"flow_error","message":"exit status 1","nodeId":"cmd","nodeType":"action-command"},"durationMs":3}`, ""}},
		{"invalid arguments", flow.CallResult{}, invalid,
			shape{true, `{"status":"error","error":{"code":"invalid_arguments","message":"the arguments do not match the tool's input schema","issues":[{"pointer":"/door","message":"must be one of left, right"}]}}`, ""}},
		{"cooldown", flow.CallResult{}, cooldown,
			shape{true, `{"status":"error","error":{"code":"cooldown","message":"the tool is cooling down; retry in 1500 ms","retryAfterMs":1500}}`, ""}},
		{"busy", flow.CallResult{}, toolErr(service.ErrBusy, "too many tool calls are running; retry when one finishes"),
			shape{true, `{"status":"error","error":{"code":"busy","message":"too many tool calls are running; retry when one finishes"}}`, ""}},
		{"forbidden", flow.CallResult{}, toolErr(service.ErrPermissionDenied, "this token may not call privileged tools"),
			shape{true, `{"status":"error","error":{"code":"forbidden","message":"this token may not call privileged tools"}}`, ""}},
		{"not found", flow.CallResult{}, toolErr(service.ErrToolNotFound, `unknown tool "x"`),
			shape{true, `{"status":"error","error":{"code":"not_found","message":"unknown tool \"x\""}}`, ""}},
		{"unavailable", flow.CallResult{}, toolErr(service.ErrUnavailable, "the tool's flow is not running"),
			shape{true, `{"status":"error","error":{"code":"unavailable","message":"the tool's flow is not running"}}`, ""}},
		{"reconfirm", flow.CallResult{}, toolErr(service.ErrReconfirmRequired, "tool changed"),
			shape{true, `{"status":"error","error":{"code":"reconfirm_required","message":"tool changed"}}`, ""}},
		{"internal failure", flow.CallResult{}, errors.New("database is locked at /var/lib/piworker.db"),
			shape{true, `{"status":"error","error":{"code":"failed","message":"the call failed; see the PiWorker log"}}`, ""}},
		{"cancelled after the run started", flow.CallResult{Status: flow.CallStatusRunning, RunID: "r9"}, context.Canceled,
			shape{false, `{"status":"running","runId":"r9","message":"The flow is still running. Call pw_get_result with this runId to get its result."}`, ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkShape(t, tc.want, shapeOf(t, callResult(tc.res, tc.err, nil)))
		})
	}
}

// Results carry the run in _meta, so a reply that looks like an envelope is
// never ambiguous.
func TestCallResultMeta(t *testing.T) {
	res := callResult(flow.CallResult{Status: flow.CallStatusCompleted, RunID: "r1", Result: json.RawMessage(`{"status":"running"}`)}, nil, nil)
	assert.Equal(t, "completed", res.Meta["piworker/status"])
	assert.Equal(t, "r1", res.Meta["piworker/runId"])
	refused := callResult(flow.CallResult{}, toolErr(service.ErrBusy, "busy"), nil)
	assert.Equal(t, "error", refused.Meta["piworker/status"])
	assert.NotContains(t, refused.Meta, "piworker/runId")
}

// With an output schema, clients validate structuredContent of every result
// that is not an error: outcomes that cannot conform are errors, and a reply
// that does not match the schema is reported instead of passed on.
func TestCallResultWithAnOutputSchema(t *testing.T) {
	out, err := newOutputSchema(json.RawMessage(`{"type":"object","properties":{"moved":{"type":"boolean"}},"required":["moved"]}`))
	require.NoError(t, err)

	ok := shapeOf(t, callResult(flow.CallResult{Status: flow.CallStatusCompleted, RunID: "r1", Result: json.RawMessage(`{"moved":true}`)}, nil, out))
	assert.False(t, ok.isError)
	assert.JSONEq(t, `{"moved":true}`, ok.structured)

	bad := shapeOf(t, callResult(flow.CallResult{Status: flow.CallStatusCompleted, RunID: "r1", Result: json.RawMessage(`{"moved":"yes"}`)}, nil, out))
	assert.True(t, bad.isError)
	var env struct {
		Status string          `json:"status"`
		RunID  string          `json:"runId"`
		Result json.RawMessage `json:"result"`
		Error  struct{ Code string }
	}
	assert.Empty(t, bad.structured, "an isError result carries no structured content")
	require.NoError(t, json.Unmarshal([]byte(bad.text), &env))
	assert.Equal(t, "completed", env.Status)
	assert.Equal(t, "r1", env.RunID)
	assert.Equal(t, "output_schema_mismatch", env.Error.Code)
	assert.JSONEq(t, `{"moved":"yes"}`, string(env.Result), "the reply is still shown")

	for _, status := range []flow.CallStatus{flow.CallStatusRunning, flow.CallStatusCompletedNoReply} {
		got := shapeOf(t, callResult(flow.CallResult{Status: status, RunID: "r1"}, nil, out))
		assert.True(t, got.isError, status)
		assert.Empty(t, got.structured, status)
	}
}

func TestFetchedResultMapping(t *testing.T) {
	cases := []struct {
		name    string
		res     flow.CallResult
		err     error
		isError bool
		want    string
	}{
		{"completed", flow.CallResult{Status: flow.CallStatusCompleted, RunID: "r1", Result: json.RawMessage(`[1]`), DurationMs: 5}, nil, false,
			`{"status":"completed","runId":"r1","result":[1],"durationMs":5}`},
		{"running", flow.CallResult{Status: flow.CallStatusRunning, RunID: "r1"}, nil, false,
			`{"status":"running","runId":"r1","message":"The flow is still running. Call pw_get_result with this runId to get its result."}`},
		{"waiting for approval", flow.CallResult{Status: flow.CallStatusApprovalRequired, RunID: "a1"}, nil, false,
			`{"status":"approval_required","runId":"a1","message":"The call waits for the admin's approval in PiWorker. Call pw_get_result with this runId later to get its result."}`},
		{"rejected", flow.CallResult{Status: flow.CallStatusRejected, RunID: "a1"}, nil, true,
			`{"status":"rejected","runId":"a1","error":{"code":"rejected","message":"The admin rejected the call."}}`},
		{"voided", flow.CallResult{Status: flow.CallStatusRejected, RunID: "a1", Error: &flow.CallError{Message: "the request was voided: token revoked"}}, nil, true,
			`{"status":"rejected","runId":"a1","error":{"code":"rejected","message":"the request was voided: token revoked"}}`},
		{"expired", flow.CallResult{Status: flow.CallStatusExpired, RunID: "r1"}, nil, true,
			`{"status":"expired","runId":"r1","error":{"code":"expired","message":"The result is no longer available."}}`},
		{"unknown run", flow.CallResult{}, toolErr(service.ErrRunNotFound, "unknown run"), true,
			`{"status":"error","error":{"code":"run_not_found","message":"unknown run"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkShape(t, shape{isError: tc.isError, structured: tc.want}, shapeOf(t, fetchedResult(tc.res, tc.err)))
		})
	}
}

// An unknown tool name answers exactly like a tool the token may not see.
func TestUnknownToolLooksLikeAHiddenOne(t *testing.T) {
	hidden := callResult(flow.CallResult{}, toolErr(service.ErrToolNotFound, `unknown tool "garage"`), nil)
	assert.Equal(t, shapeOf(t, hidden), shapeOf(t, unknownTool("garage")))
}
