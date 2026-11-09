package mcpserver

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/toolschema"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// StaticToolPrefix starts the name of every built-in tool. Flow tools may
// not use it (trigger-tool refuses such names), so the two never clash.
const StaticToolPrefix = "pw_"

// GetResultTool is the built-in tool that reads the result of an earlier
// call (scope call).
const GetResultTool = "pw_get_result"

// StaticTool is a built-in tool (pw_*), advertised to tokens holding Scope.
// Its arguments are validated against InputSchema (or by RawInput) before
// Handler runs.
type StaticTool struct {
	Name        string
	Title       string
	Description string
	// InputSchema is a JSON object schema in the closed subset of
	// internal/toolschema; clients see its compiled (canonical) form.
	// Every built-in tool uses it, except the few whose arguments are
	// flows (RawInput).
	InputSchema json.RawMessage
	// RawInput, instead of InputSchema, is the input contract of a tool
	// whose arguments do not fit the closed subset (flow-shaped arguments:
	// nested objects, free-form node settings, any JSON payload).
	RawInput *RawInput
	// OutputSchema, when set, describes the structured content of the
	// tool's successful results.
	OutputSchema json.RawMessage
	// Annotations are advertised explicitly (every hint is sent).
	Annotations flow.Annotations
	// Scope is the token scope (storage.Scope*) the tool needs.
	Scope   string
	Handler StaticToolHandler
}

// MaxRawInputSchemaBytes bounds a RawInput schema, as toolschema bounds the
// closed ones.
const MaxRawInputSchemaBytes = toolschema.MaxSchemaBytes

// RawInput is the escape hatch of StaticTool for arguments outside
// toolschema's closed subset. Schema is advertised to clients as written;
// the endpoint enforces MaxBytes on the arguments' JSON as the client sent
// it, decodes them as every tool's (a JSON object, no duplicate names, valid
// UTF-8) and runs Validate before the handler. Validate must enforce at
// least what Schema describes (closed objects, types, bounds): the endpoint
// does not interpret Schema. Its refusals are invalid_arguments, counted in
// the rejections like every built-in tool's.
type RawInput struct {
	// Schema is a JSON Schema of type object, at most
	// MaxRawInputSchemaBytes.
	Schema json.RawMessage
	// MaxBytes bounds the arguments' JSON (1 to DefaultMaxRequestBodyBytes).
	MaxBytes int
	// Validate returns the problems of decoded arguments (none: valid).
	// Messages must not quote argument values.
	Validate func(args map[string]any) []toolschema.Issue
}

// StaticToolHandler serves a built-in tool. args were validated against the
// tool's input schema (or by its RawInput). A returned *service.ToolError becomes an isError
// result with its code; any other error is logged and answered as a failure
// without details.
type StaticToolHandler func(ctx context.Context, actor service.Actor, args map[string]any) (*mcp.CallToolResult, error)

// StaticPrompt is a built-in prompt, advertised to tokens holding Scope.
// Handler checks its own arguments. A returned *service.ToolError becomes a
// JSON-RPC invalid-params error with its message, a *jsonrpc.Error is sent
// as it is, and any other error is logged and answered as an internal error
// without details.
type StaticPrompt struct {
	Prompt  *mcp.Prompt
	Scope   string
	Handler func(ctx context.Context, actor service.Actor, args map[string]string) (*mcp.GetPromptResult, error)
}

// StructuredResult is the result of a built-in tool answering v: v as the
// structured content (wrapped as {"result": v} unless it encodes to a JSON
// object) and its JSON as the text block.
func StructuredResult(v any) (*mcp.CallToolResult, error) {
	data, err := marshalNoEscape(v)
	if err != nil {
		return nil, fmt.Errorf("encode result: %w", err)
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(data)}},
		StructuredContent: reply(data),
	}, nil
}

// staticTool is a registered StaticTool with its compiled schemas: the
// closed input schema, or the raw one as advertised.
type staticTool struct {
	StaticTool
	schema *toolschema.Schema
	raw    json.RawMessage
	out    *outputSchema
}

// inputSchema is the input schema clients see.
func (st *staticTool) inputSchema() json.RawMessage {
	if st.RawInput != nil {
		return st.raw
	}
	return st.schema.JSON()
}

// validate checks a call's arguments (raw as the client sent them) against
// the tool's input contract and returns them decoded, or the refusal.
func (st *staticTool) validate(raw json.RawMessage) (map[string]any, *service.ToolError) {
	if st.RawInput != nil && len(raw) > st.RawInput.MaxBytes {
		return nil, invalidArguments([]toolschema.Issue{{Pointer: "", Message: fmt.Sprintf(
			"the arguments exceed %d bytes of JSON", st.RawInput.MaxBytes)}})
	}
	args, refusal := decodeArgs(raw)
	if refusal != nil {
		return nil, refusal
	}
	var issues []toolschema.Issue
	if st.RawInput != nil {
		issues = st.RawInput.Validate(args)
	} else {
		issues = st.schema.Validate(args)
	}
	if len(issues) > 0 {
		return nil, invalidArguments(issues)
	}
	return args, nil
}

// compileRawInput checks a RawInput and returns its schema as advertised.
func compileRawInput(in *RawInput) (json.RawMessage, error) {
	switch {
	case in.Validate == nil:
		return nil, errors.New("raw input: no validator")
	case in.MaxBytes <= 0 || in.MaxBytes > DefaultMaxRequestBodyBytes:
		return nil, fmt.Errorf("raw input: the size bound must be 1 to %d bytes", DefaultMaxRequestBodyBytes)
	case len(in.Schema) > MaxRawInputSchemaBytes:
		return nil, fmt.Errorf("raw input: the schema exceeds %d bytes", MaxRawInputSchemaBytes)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(in.Schema, &s); err != nil {
		return nil, fmt.Errorf("raw input schema: %w", err)
	}
	if s.Type != "object" {
		return nil, errors.New(`raw input schema: the root must have type "object"`)
	}
	if _, err := s.Resolve(nil); err != nil {
		return nil, fmt.Errorf("raw input schema: %w", err)
	}
	return slices.Clone(in.Schema), nil
}

var knownScopes = []string{storage.ScopeCall, storage.ScopeRead, storage.ScopeAuthor, storage.ScopeDeploy}

func compileStaticTool(st StaticTool) (*staticTool, error) {
	switch {
	case !strings.HasPrefix(st.Name, StaticToolPrefix) || len(st.Name) == len(StaticToolPrefix):
		return nil, fmt.Errorf("mcpserver: built-in tool %q: the name must start with %q", st.Name, StaticToolPrefix)
	case !slices.Contains(knownScopes, st.Scope):
		return nil, fmt.Errorf("mcpserver: built-in tool %q: unknown scope %q", st.Name, st.Scope)
	case st.Handler == nil:
		return nil, fmt.Errorf("mcpserver: built-in tool %q has no handler", st.Name)
	case strings.TrimSpace(st.Description) == "":
		return nil, fmt.Errorf("mcpserver: built-in tool %q has no description", st.Name)
	}
	c := &staticTool{StaticTool: st}
	if st.RawInput != nil {
		if len(st.InputSchema) > 0 {
			return nil, fmt.Errorf("mcpserver: built-in tool %q has both an input schema and a raw input", st.Name)
		}
		raw, err := compileRawInput(st.RawInput)
		if err != nil {
			return nil, fmt.Errorf("mcpserver: built-in tool %q: %w", st.Name, err)
		}
		c.raw = raw
	} else {
		schema, err := toolschema.Compile(st.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("mcpserver: built-in tool %q: input schema: %w", st.Name, err)
		}
		c.schema = schema
	}
	if len(st.OutputSchema) > 0 {
		var err error
		if c.out, err = newOutputSchema(st.OutputSchema); err != nil {
			return nil, fmt.Errorf("mcpserver: built-in tool %q: %w", st.Name, err)
		}
	}
	return c, nil
}

// getResultTool is pw_get_result: the result of a call the token made that
// answered running or approval_required.
func (h *Handler) getResultTool() StaticTool {
	return StaticTool{
		Name:  GetResultTool,
		Title: "Get the result of a tool call",
		Description: fmt.Sprintf("Returns the result of an earlier tool call of this token that answered status "+
			"\"running\" or \"approval_required\", by its runId. A running call is waited for up to %d seconds; "+
			"call again while the status is still \"running\" or \"approval_required\".", int(h.resultWait.Seconds())),
		InputSchema: json.RawMessage(`{"type":"object","properties":{"runId":{"type":"string","minLength":1,"maxLength":128,` +
			`"description":"The runId of the call, from its result."}},"required":["runId"]}`),
		Annotations: flow.Annotations{ReadOnlyHint: true, IdempotentHint: true},
		Scope:       storage.ScopeCall,
		Handler: func(ctx context.Context, actor service.Actor, args map[string]any) (*mcp.CallToolResult, error) {
			runID, _ := args["runId"].(string)
			ctx, cancel := context.WithTimeout(ctx, h.resultWait)
			defer cancel()
			res, err := h.tools.Result(ctx, actor, runID)
			if err != nil && res.RunID == "" {
				return nil, err
			}
			return fetchedResult(res, err), nil
		},
	}
}

// decodeArgs decodes a tool call's arguments: a JSON object (absent or null
// means none). Duplicate names and invalid UTF-8 are refused, as on the
// REST twin.
func decodeArgs(raw json.RawMessage) (map[string]any, *service.ToolError) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return map[string]any{}, nil
	}
	var v any
	if err := jsonv2.Unmarshal([]byte(trimmed), &v); err != nil {
		return nil, invalidArguments([]toolschema.Issue{{Pointer: "", Message: "the arguments are not valid JSON: " + shorten(err.Error(), 200)}})
	}
	args, ok := v.(map[string]any)
	if !ok {
		return nil, invalidArguments([]toolschema.Issue{{Pointer: "", Message: "the arguments must be a JSON object"}})
	}
	return args, nil
}

// invalidArguments is the refusal of arguments outside a schema, issues
// sorted by pointer (as the tool service does).
func invalidArguments(issues []toolschema.Issue) *service.ToolError {
	sorted := slices.Clone(issues)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].Pointer < sorted[b].Pointer })
	return &service.ToolError{Kind: service.ErrInvalidArguments, Message: "the arguments do not match the tool's input schema", Issues: sorted}
}

// isInternal reports errors that are not the caller's doing: neither a
// service refusal nor the request ending.
func isInternal(err error) bool {
	var te *service.ToolError
	return err != nil && !errors.As(err, &te) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}
