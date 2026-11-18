package node

import (
	"context"
	"encoding/json"
	"errors"
)

// Agent nodes (process-llm-agent) call the admin's flow tools through a
// ToolCaller the flow runtime puts on their context (ADR-0039,
// docs/design/llm.md §4). The service layer implements it with the same
// rules as every other tool call; builtin nodes never import flow or
// service, so the contract lives here.

// AgentTool is a published flow tool an agent node may call, as the model
// is shown it.
type AgentTool struct {
	Name        string
	Description string
	// InputSchema is the tool's advertised input schema (a JSON object
	// schema).
	InputSchema json.RawMessage
	Risk        Risk
	// Confirm is the tool's confirmation policy (ToolConfirmNone for every
	// tool an agent node may call).
	Confirm string
}

// AgentCaller identifies an agent node calling tools and what its settings
// allow.
type AgentCaller struct {
	// FlowID and NodeID are the agent node's (NodeInfoFromContext): a tool
	// of the node's own flow is refused, and each node has its own limits.
	FlowID, NodeID string
	// Tools are the tool names the node lists; no other tool is listed or
	// called.
	Tools []string
	// AllowPrivileged lets the node call privileged tools.
	AllowPrivileged bool
	// Waiting are the flows whose agent nodes wait, through tool calls, for
	// the run the node serves (WaitingFlows of its message's correlation
	// ID): a tool of one of them is refused, like one of the node's own
	// flow, because its run would wait for an agent node that waits for it.
	Waiting []string
}

// Statuses of a ToolCallOutcome.
const (
	// ToolCallOK: the run replied (or finished without a reply: Result is
	// then empty).
	ToolCallOK = "ok"
	// ToolCallError: the run failed; Error says why, as agents see errors.
	ToolCallError = "error"
	// ToolCallRunning: the tool's timeout passed before the reply; the run
	// goes on.
	ToolCallRunning = "running"
)

// ToolCallOutcome is the outcome of a run an agent node's call started.
type ToolCallOutcome struct {
	// Status is ToolCallOK, ToolCallError or ToolCallRunning.
	Status string
	// Result is the reply, redacted and capped as agents see results.
	Result json.RawMessage
	// Error is the run's error (Status ToolCallError), redacted as agents
	// see errors.
	Error string
}

// ToolCaller lists and calls the flow tools of agent nodes.
type ToolCaller interface {
	// AgentTools returns the tools caller may call now, in the order its
	// Tools lists them; a listed name that is not published, or that the
	// rules refuse, is left out.
	AgentTools(caller AgentCaller) []AgentTool
	// CallAgentTool calls the tool name with args and waits, at most until
	// ctx ends, for the outcome of the run it starts. An error means no
	// outcome: the call was refused before any run started (an unknown or
	// unlisted tool, a forbidden one, invalid arguments, a limit or a
	// cooldown), or ctx ended. Its message never quotes a secret and may
	// be shown to the model.
	CallAgentTool(ctx context.Context, caller AgentCaller, name string, args map[string]any) (ToolCallOutcome, error)
}

// ErrNoToolCaller is what the tool caller of a context without one returns
// for every call.
var ErrNoToolCaller = errors.New("no flow tools are available here: agent nodes call tools only in a deployed flow (never in a dry-run or a node test)")

type toolCallerKey struct{}

// WithToolCaller returns ctx carrying tc (nil: none). The flow runtime puts
// the one it was configured with on every node's context.
func WithToolCaller(ctx context.Context, tc ToolCaller) context.Context {
	return context.WithValue(ctx, toolCallerKey{}, tc)
}

// ToolCallerFromContext returns the tool caller of the runtime that runs the
// node. It is never nil: outside a runtime that has one, and in a sandboxed
// execution (a dry-run or a node test) whatever its context carries, it
// lists nothing and refuses every call with ErrNoToolCaller.
func ToolCallerFromContext(ctx context.Context) ToolCaller {
	if ctx == nil || InSandbox(ctx) {
		return noToolCaller{}
	}
	if tc, ok := ctx.Value(toolCallerKey{}).(ToolCaller); ok && tc != nil {
		return tc
	}
	return noToolCaller{}
}

type noToolCaller struct{}

func (noToolCaller) AgentTools(AgentCaller) []AgentTool { return nil }

func (noToolCaller) CallAgentTool(context.Context, AgentCaller, string, map[string]any) (ToolCallOutcome, error) {
	return ToolCallOutcome{}, ErrNoToolCaller
}
