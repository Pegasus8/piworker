package mcpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/toolschema"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Keys of the _meta of every tool result: the outcome and the run, so a
// reply that happens to look like an envelope is never ambiguous.
const (
	metaStatus = "piworker/status"
	metaRunID  = "piworker/runId"
)

// Error codes of results that are not service refusals.
const (
	codeFlowError      = "flow_error"
	codeFailed         = "failed"
	codeRejected       = "rejected"
	codeExpired        = "expired"
	codeOutputMismatch = "output_schema_mismatch"
)

// Messages telling the agent what an outcome means and what to do next.
const (
	msgRunning          = "The flow is still running. Call pw_get_result with this runId to get its result."
	msgApprovalRequired = "The call waits for the admin's approval in PiWorker. Call pw_get_result with this runId later to get its result."
	msgNoReply          = "The flow ran to the end without replying."
	msgRejected         = "The admin rejected the call."
	msgExpired          = "The result is no longer available."
	msgFailed           = "the call failed; see the PiWorker log"
)

// envelope is the structured content of every result that is not a
// completed call's reply: the outcome, the run and, for failures, why.
type envelope struct {
	Status     string          `json:"status"`
	RunID      string          `json:"runId,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      *errorInfo      `json:"error,omitempty"`
	DurationMs int64           `json:"durationMs,omitempty"`
	Message    string          `json:"message,omitempty"`
}

type errorInfo struct {
	Code     string             `json:"code"`
	Message  string             `json:"message"`
	NodeID   string             `json:"nodeId,omitempty"`
	NodeType string             `json:"nodeType,omitempty"`
	Issues   []toolschema.Issue `json:"issues,omitempty"`
	// FlowIssues are a flow's Lint issues (authoring refusals).
	FlowIssues []flow.Issue `json:"flowIssues,omitempty"`
	// CurrentVersion is the flow's current version (a conflict on a stale
	// base version).
	CurrentVersion int   `json:"currentVersion,omitempty"`
	RetryAfterMs   int64 `json:"retryAfterMs,omitempty"`
}

// outputSchema is a tool's resolved output schema: clients validate the
// structured content of non-error results against it.
type outputSchema struct {
	raw      json.RawMessage
	resolved *jsonschema.Resolved
}

func newOutputSchema(raw json.RawMessage) (*outputSchema, error) {
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("output schema: %w", err)
	}
	resolved, err := s.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("output schema: %w", err)
	}
	return &outputSchema{raw: raw, resolved: resolved}, nil
}

func (o *outputSchema) validate(structured json.RawMessage) error {
	var v any
	if err := json.Unmarshal(structured, &v); err != nil {
		return err
	}
	return o.resolved.Validate(v)
}

// callResult maps the outcome of service.Tools.Call to an MCP result. A
// completed call's structured content is its reply (wrapped as {"result": …}
// unless it is a JSON object); every other outcome is an envelope, the
// structured content of non-errors and the text block of all. Refusals,
// flow errors and approval_required are errors (isError, without
// structured content), never JSON-RPC errors. out is the tool's output
// schema, if any: outcomes that cannot conform to it are errors too.
func callResult(res flow.CallResult, err error, out *outputSchema) *mcp.CallToolResult {
	if err != nil && res.RunID == "" {
		return errorResult(err)
	}
	if res.Status == "" {
		// The call's context ended while it waited; the run goes on.
		res.Status = flow.CallStatusRunning
	}
	if res.Status != flow.CallStatusCompleted {
		// With an output schema every such outcome is an error: the
		// envelope does not match the schema.
		return withRun(envelopeResult(outcome(res), isFailure(res.Status) || out != nil), res)
	}
	structured := reply(res.Result)
	if out != nil {
		if verr := out.validate(structured); verr != nil {
			env := outcome(res)
			env.Result = replyJSON(res.Result)
			env.Error = &errorInfo{Code: codeOutputMismatch, Message: "the flow's reply does not match the tool's output schema: " + verr.Error()}
			return withRun(envelopeResult(env, true), res)
		}
	}
	return withRun(&mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(replyJSON(res.Result))}},
		StructuredContent: structured,
	}, res)
}

// fetchedResult maps the outcome of service.Tools.Result (pw_get_result):
// always an envelope, with the reply under "result" once completed.
func fetchedResult(res flow.CallResult, err error) *mcp.CallToolResult {
	if err != nil && res.RunID == "" {
		return errorResult(err)
	}
	if res.Status == "" {
		res.Status = flow.CallStatusRunning
	}
	env := outcome(res)
	if res.Status == flow.CallStatusCompleted {
		env.Result = replyJSON(res.Result)
	}
	failed := isFailure(res.Status) && res.Status != flow.CallStatusApprovalRequired
	return withRun(envelopeResult(env, failed), res)
}

// errorResult maps a call that started nothing: a service refusal keeps its
// code and details; anything else is an internal failure whose details stay
// in the log.
func errorResult(err error) *mcp.CallToolResult {
	info := &errorInfo{Code: codeFailed, Message: msgFailed}
	var te *service.ToolError
	if errors.As(err, &te) {
		info = &errorInfo{Code: te.Code(), Message: te.Error(), Issues: te.Issues, FlowIssues: te.FlowIssues,
			CurrentVersion: te.CurrentVersion, RetryAfterMs: te.RetryAfter.Milliseconds()}
	}
	r := envelopeResult(envelope{Status: string(flow.CallStatusError), Error: info}, true)
	r.Meta = mcp.Meta{metaStatus: string(flow.CallStatusError)}
	return r
}

// unknownTool answers a call of a tool the server does not hold, exactly as
// the service answers a tool the token may not see.
func unknownTool(name string) *mcp.CallToolResult {
	return errorResult(&service.ToolError{Kind: service.ErrToolNotFound, Message: fmt.Sprintf("unknown tool %q", name)})
}

// outcome is the envelope of a run's result (without its reply).
func outcome(res flow.CallResult) envelope {
	env := envelope{Status: string(res.Status), RunID: res.RunID, DurationMs: res.DurationMs}
	switch res.Status {
	case flow.CallStatusRunning:
		env.Message = msgRunning
	case flow.CallStatusApprovalRequired:
		env.Message = msgApprovalRequired
	case flow.CallStatusCompletedNoReply:
		env.Message = msgNoReply
	case flow.CallStatusError:
		env.Error = &errorInfo{Code: codeFlowError, Message: msgFailed}
		if res.Error != nil {
			env.Error = &errorInfo{Code: codeFlowError, Message: res.Error.Message, NodeID: res.Error.NodeID, NodeType: res.Error.NodeType}
		}
	case flow.CallStatusRejected:
		env.Error = &errorInfo{Code: codeRejected, Message: msgRejected}
		if res.Error != nil && res.Error.Message != "" {
			env.Error.Message = res.Error.Message
		}
	case flow.CallStatusExpired:
		env.Error = &errorInfo{Code: codeExpired, Message: msgExpired}
	}
	return env
}

// isFailure reports the outcomes an agent must not mistake for success.
func isFailure(s flow.CallStatus) bool {
	switch s {
	case flow.CallStatusError, flow.CallStatusRejected, flow.CallStatusExpired, flow.CallStatusApprovalRequired:
		return true
	}
	return false
}

// envelopeResult is a result carrying env: as its text block and, unless it
// is an error, as its structured content (see withoutStructuredError).
func envelopeResult(env envelope, isError bool) *mcp.CallToolResult {
	data, err := marshalNoEscape(env)
	if err != nil {
		data = []byte(`{"status":"error","error":{"code":"failed","message":"the result cannot be encoded"}}`)
		isError = true
	}
	return withoutStructuredError(&mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(data)}},
		StructuredContent: json.RawMessage(data),
		IsError:           isError,
	})
}

// withoutStructuredError drops the structured content of an isError
// result, which keeps its envelope in the text block (and its outcome in
// _meta). Clients validate structured content against the tool's output
// schema even on errors (the reference TypeScript SDK refuses the whole
// result when it does not match), including a schema listed before the
// tool was hidden or changed, so an error envelope there would turn a
// refusal the agent should read into a protocol error.
func withoutStructuredError(r *mcp.CallToolResult) *mcp.CallToolResult {
	if r != nil && r.IsError {
		r.StructuredContent = nil
	}
	return r
}

func withRun(r *mcp.CallToolResult, res flow.CallResult) *mcp.CallToolResult {
	r.Meta = mcp.Meta{metaStatus: string(res.Status)}
	if res.RunID != "" {
		r.Meta[metaRunID] = res.RunID
	}
	return r
}

// replyJSON is a reply's JSON text ("null" when the flow replied nothing).
func replyJSON(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(trimmed)
}

// reply is a completed call's structured content: the reply when it is a
// JSON object, else {"result": reply} (structured content is an object).
func reply(raw json.RawMessage) json.RawMessage {
	text := replyJSON(raw)
	if text[0] == '{' {
		return text
	}
	out := make([]byte, 0, len(text)+len(`{"result":}`))
	out = append(out, `{"result":`...)
	out = append(out, text...)
	return append(out, '}')
}

// marshalNoEscape encodes v without HTML escaping and without a trailing
// newline.
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
