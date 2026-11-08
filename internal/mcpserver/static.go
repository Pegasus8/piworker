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
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// StaticToolPrefix starts the name of every built-in tool. Flow tools may
// not use it (trigger-tool refuses such names), so the two never clash.
const StaticToolPrefix = "pw_"

// GetResultTool is the built-in tool that reads the result of an earlier
// call (scope call).
const GetResultTool = "pw_get_result"

// StaticTool is a built-in tool (pw_*), advertised to tokens holding Scope.
// Its arguments are validated against InputSchema before Handler runs.
type StaticTool struct {
	Name        string
	Title       string
	Description string
	// InputSchema is a JSON object schema in the closed subset of
	// internal/toolschema; clients see its compiled (canonical) form.
	InputSchema json.RawMessage
	// OutputSchema, when set, describes the structured content of the
	// tool's successful results.
	OutputSchema json.RawMessage
	// Annotations are advertised explicitly (every hint is sent).
	Annotations flow.Annotations
	// Scope is the token scope (storage.Scope*) the tool needs.
	Scope   string
	Handler StaticToolHandler
}

// StaticToolHandler serves a built-in tool. args were validated against the
// tool's input schema. A returned *service.ToolError becomes an isError
// result with its code; any other error is logged and answered as a failure
// without details.
type StaticToolHandler func(ctx context.Context, actor service.Actor, args map[string]any) (*mcp.CallToolResult, error)

// StaticPrompt is a built-in prompt, advertised to tokens holding Scope.
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

// staticTool is a registered StaticTool with its compiled schemas.
type staticTool struct {
	StaticTool
	schema *toolschema.Schema
	out    *outputSchema
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
	schema, err := toolschema.Compile(st.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: built-in tool %q: input schema: %w", st.Name, err)
	}
	c := &staticTool{StaticTool: st, schema: schema}
	if len(st.OutputSchema) > 0 {
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
func decodeArgs(raw json.RawMessage) (map[string]any, error) {
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
