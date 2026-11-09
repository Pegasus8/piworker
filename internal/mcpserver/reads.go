package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ReadService serves the read views (*service.Reads): what an agent with
// the read scope may observe of flows, runs and the system, redacted and
// bounded by the service.
type ReadService interface {
	ListFlows(ctx context.Context, actor service.Actor) (service.FlowList, error)
	GetFlow(ctx context.Context, actor service.Actor, flowID string) (service.FlowView, error)
	ListRuns(ctx context.Context, actor service.Actor, flowID string, limit int, before string) (service.RunList, error)
	GetRun(ctx context.Context, actor service.Actor, flowID, runID string) (service.RunView, error)
	SystemStatus(ctx context.Context, actor service.Actor) (service.SystemStatusView, error)
	DiagnoseFlow(ctx context.Context, actor service.Actor, flowID string) (string, error)
}

// Names of the read tools and prompt (scope read).
const (
	ListFlowsTool      = "pw_list_flows"
	GetFlowTool        = "pw_get_flow"
	ListRunsTool       = "pw_list_runs"
	GetRunTool         = "pw_get_run"
	SystemStatusTool   = "pw_system_status"
	DiagnoseFlowPrompt = "diagnose_flow"
)

// idPattern is the syntax of the flow and run ids the read tools take:
// PiWorker's ids are UUIDs or names of letters, digits, '-' and '_' (a few
// more id characters are accepted for flows saved by older versions).
const idPattern = `^[A-Za-z0-9_.:-]{1,128}$`

var idSyntax = regexp.MustCompile(idPattern)

// readAnnotations: the read tools change nothing and reach nothing outside
// PiWorker.
var readAnnotations = flow.Annotations{ReadOnlyHint: true, DestructiveHint: false, IdempotentHint: true, OpenWorldHint: false}

// Input schemas of the read tools (closed toolschema subset).
const (
	flowIDProperty = `"flowId":{"type":"string","pattern":"` + idPattern + `","maxLength":128,` +
		`"description":"The flow's id, from pw_list_flows."}`
	noArguments = `{"type":"object","properties":{}}`
)

// ReadTools returns the read tools (scope read) serving reads:
// pw_list_flows, pw_get_flow, pw_list_runs, pw_get_run and
// pw_system_status. Their arguments are validated against their input
// schemas before the view runs; a view's *service.ToolError (forbidden,
// flow_not_found, run_not_found) is an isError result with its code.
func ReadTools(reads ReadService) []StaticTool {
	return []StaticTool{
		{
			Name:  ListFlowsTool,
			Title: "List flows",
			Description: "Lists the flows stored in PiWorker, newest first (at most 200): id, name, state (running, " +
				"stopped, or failed: meant to run but not running), node count, the tools each flow publishes and its " +
				"latest run. Use pw_get_flow for a flow's nodes and connections.",
			InputSchema:  json.RawMessage(noArguments),
			OutputSchema: json.RawMessage(flowListSchema),
			Annotations:  readAnnotations,
			Scope:        storage.ScopeRead,
			Handler: func(ctx context.Context, actor service.Actor, _ map[string]any) (*mcp.CallToolResult, error) {
				return viewResult(reads.ListFlows(ctx, actor))
			},
		},
		{
			Name:  GetFlowTool,
			Title: "Get a flow",
			Description: "Shows one flow: its nodes (type, enabled, risk, settings), connections and the tools it " +
				"publishes while running. Settings that are credentials or not shown to agents read \"<redacted>\": " +
				"they are hidden on purpose and cannot be read over MCP.",
			InputSchema:  json.RawMessage(`{"type":"object","properties":{` + flowIDProperty + `},"required":["flowId"]}`),
			OutputSchema: json.RawMessage(flowViewSchema),
			Annotations:  readAnnotations,
			Scope:        storage.ScopeRead,
			Handler: func(ctx context.Context, actor service.Actor, args map[string]any) (*mcp.CallToolResult, error) {
				return viewResult(reads.GetFlow(ctx, actor, stringArg(args, "flowId")))
			},
		},
		{
			Name:  ListRunsTool,
			Title: "List a flow's runs",
			Description: "Lists the recorded runs of a flow, newest first: status, who started it (trigger, editor or " +
				"agent), timings and the counts of node executions and errors. For the next (older) page pass the " +
				"nextBefore of this page as before.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + flowIDProperty + `,` +
				`"limit":{"type":"integer","minimum":1,"maximum":100,"description":"Runs per page, 1 to 100 (default 20)."},` +
				`"before":{"type":"string","pattern":"` + idPattern + `","maxLength":128,` +
				`"description":"List the runs older than this run id (the nextBefore of the previous page)."}},` +
				`"required":["flowId"]}`),
			OutputSchema: json.RawMessage(runListSchema),
			Annotations:  readAnnotations,
			Scope:        storage.ScopeRead,
			Handler: func(ctx context.Context, actor service.Actor, args map[string]any) (*mcp.CallToolResult, error) {
				limit, _ := args["limit"].(float64) // validated: an integer from 1 to 100
				return viewResult(reads.ListRuns(ctx, actor, stringArg(args, "flowId"), int(limit), stringArg(args, "before")))
			},
		},
		{
			Name:  GetRunTool,
			Title: "Get a run",
			Description: "Shows one recorded run of a flow with its node executions in order: node, type, phase, " +
				"duration and error. Payloads are not recorded.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + flowIDProperty + `,` +
				`"runId":{"type":"string","pattern":"` + idPattern + `","maxLength":128,` +
				`"description":"The run's id, from pw_list_runs."}},"required":["flowId","runId"]}`),
			OutputSchema: json.RawMessage(runViewSchema),
			Annotations:  readAnnotations,
			Scope:        storage.ScopeRead,
			Handler: func(ctx context.Context, actor service.Actor, args map[string]any) (*mcp.CallToolResult, error) {
				return viewResult(reads.GetRun(ctx, actor, stringArg(args, "flowId"), stringArg(args, "runId")))
			},
		},
		{
			Name:  SystemStatusTool,
			Title: "Get the system status",
			Description: "Shows PiWorker's version, platform and uptime, the number of running and stored flows and " +
				"of published tools, the MCP limits, the agent deploy level and, when known, the free disk, memory " +
				"and CPU temperature of the device.",
			InputSchema:  json.RawMessage(noArguments),
			OutputSchema: json.RawMessage(systemStatusSchema),
			Annotations:  readAnnotations,
			Scope:        storage.ScopeRead,
			Handler: func(ctx context.Context, actor service.Actor, _ map[string]any) (*mcp.CallToolResult, error) {
				return viewResult(reads.SystemStatus(ctx, actor))
			},
		},
	}
}

// ReadPrompts returns the read prompts (scope read): diagnose_flow{flowId}.
func ReadPrompts(reads ReadService) []StaticPrompt {
	return []StaticPrompt{{
		Prompt: &mcp.Prompt{
			Name:        DiagnoseFlowPrompt,
			Title:       "Diagnose a flow",
			Description: "Steps to find out, with the read tools only, why a flow fails or does not behave as expected.",
			Arguments: []*mcp.PromptArgument{{
				Name: "flowId", Title: "Flow id", Description: "The flow's id, from pw_list_flows.", Required: true,
			}},
		},
		Scope: storage.ScopeRead,
		Handler: func(ctx context.Context, actor service.Actor, args map[string]string) (*mcp.GetPromptResult, error) {
			if err := checkPromptArgs(args, "flowId"); err != nil {
				return nil, err
			}
			text, err := reads.DiagnoseFlow(ctx, actor, args["flowId"])
			if err != nil {
				return nil, err
			}
			return &mcp.GetPromptResult{
				Description: "Diagnose a PiWorker flow with the read tools",
				Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}},
			}, nil
		},
	}}
}

// checkPromptArgs requires exactly the named id arguments, each of the id
// syntax: a JSON-RPC invalid-params error otherwise.
func checkPromptArgs(args map[string]string, ids ...string) error {
	for name := range args {
		known := false
		for _, id := range ids {
			known = known || name == id
		}
		if !known {
			return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("unknown argument %q", shorten(name, 64))}
		}
	}
	for _, id := range ids {
		v, ok := args[id]
		if !ok {
			return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("the argument %q is required", id)}
		}
		if !idSyntax.MatchString(v) {
			return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf(
				"the argument %q must be 1 to 128 letters, digits or '_', '-', '.', ':'", id)}
		}
	}
	return nil
}

// viewResult answers a read view: the view as structured content and its
// JSON as the text block, or the view's error.
func viewResult(view any, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		return nil, err
	}
	return StructuredResult(view)
}

// stringArg is a validated optional string argument ("" when absent).
func stringArg(args map[string]any, name string) string {
	s, _ := args[name].(string)
	return s
}
