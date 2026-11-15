package service

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
)

// Agent nodes (process-llm-agent) call the admin's flow tools through the
// tool service, which is their node.ToolCaller (ADR-0039,
// docs/design/llm.md §4). Their calls go through Call's one path (schema,
// limits, cooldowns, redaction) with the rules of agentNodeAccess. An agent
// node has no token, so its calls write no MCP audit row: each run is in
// its flow's activity, with the actor "agent-node:<flowID>/<nodeID>".

var _ node.ToolCaller = (*Tools)(nil)

// agentNodeAccess decides whether an agent node may call info (nil) or why
// not. Every message may be shown to the model.
func agentNodeAccess(actor Actor, info flow.ToolInfo) error {
	switch {
	case actor.FlowID == "" || actor.NodeID == "":
		return toolError(ErrPermissionDenied, "flow tools are called by agent nodes of a running flow only")
	case !slices.Contains(actor.Tools, info.ToolName):
		return toolError(ErrToolNotFound, fmt.Sprintf("unknown tool %q: it is not one of this agent node's tools", info.ToolName))
	case info.FlowID == actor.FlowID:
		// The run would wait for the agent node, which waits for the run.
		return toolError(ErrPermissionDenied, fmt.Sprintf("tool %q is published by this agent node's own flow, whose tools it cannot call", info.ToolName))
	case info.Risk == node.RiskPrivileged && !actor.Privileged:
		return toolError(ErrPermissionDenied, fmt.Sprintf("tool %q is privileged; this agent node may call privileged tools only with allowPrivileged turned on", info.ToolName))
	case info.Confirm == node.ToolConfirmClient:
		return toolError(ErrPermissionDenied, fmt.Sprintf("tool %q asks its caller to confirm each call with a person (confirm: client), and an agent node has no one to ask", info.ToolName))
	case info.Confirm != node.ToolConfirmNone:
		return toolError(ErrPermissionDenied, fmt.Sprintf("tool %q needs the admin's approval for each call (confirm: %s), which an agent node cannot wait for", info.ToolName, info.Confirm))
	}
	return nil
}

// AgentTools returns the running tools caller may call now, in the order
// its Tools names them (each once), as the model is shown them: the
// advertised input schema, the description (the title when there is none),
// the risk and the confirm policy. A name no running flow publishes, or one
// agentNodeAccess refuses, is left out, so every listed tool is callable.
func (t *Tools) AgentTools(caller node.AgentCaller) []node.AgentTool {
	actor := AgentNodeActor(caller)
	_, published := t.runtimes.Tools()
	byName := make(map[string]flow.ToolInfo, len(published))
	for _, ti := range published {
		byName[ti.ToolName] = ti
	}
	var tools []node.AgentTool
	seen := map[string]bool{}
	for _, name := range caller.Tools {
		ti, ok := byName[name]
		if seen[name] || !ok || ti.Schema == nil || t.checkAccess(actor, ti) != nil {
			continue
		}
		seen[name] = true
		description := ti.Description
		if description == "" {
			description = ti.Title
		}
		tools = append(tools, node.AgentTool{
			Name:        ti.ToolName,
			Description: description,
			InputSchema: ti.Schema.JSON(),
			Risk:        ti.Risk,
			Confirm:     ti.Confirm,
		})
	}
	return tools
}

// CallAgentTool calls the tool name for an agent node, by the rules of
// agentNodeAccess and those of every call (the input schema, the tool's
// maxConcurrent and cooldown, the node's own limit of
// DefaultPerTokenInflight calls at once), and waits for the run's outcome
// until the tool's timeout ("running") or ctx ends. Results and errors are
// redacted as an MCP agent's. A refusal is a *ToolError (for invalid
// arguments its message lists the problems, so the model can fix its
// call); after ctx ended the outcome is "running" with ctx's error.
func (t *Tools) CallAgentTool(ctx context.Context, caller node.AgentCaller, name string, args map[string]any) (node.ToolCallOutcome, error) {
	actor := AgentNodeActor(caller)
	if !slices.Contains(actor.Tools, name) {
		// Never looked up: what the node does not list does not exist.
		return node.ToolCallOutcome{}, toolError(ErrToolNotFound, fmt.Sprintf("unknown tool %q: it is not one of this agent node's tools", name))
	}
	res, err := t.call(ctx, actor, ToolByName(name), args, nil)
	if res.RunID == "" {
		return node.ToolCallOutcome{}, withIssues(err)
	}
	return agentOutcome(res), err
}

// agentOutcome is what an agent node is told of a call's (redacted) result.
func agentOutcome(res flow.CallResult) node.ToolCallOutcome {
	switch res.Status {
	case flow.CallStatusCompleted, flow.CallStatusCompletedNoReply:
		return node.ToolCallOutcome{Status: node.ToolCallOK, Result: res.Result}
	case flow.CallStatusRunning:
		return node.ToolCallOutcome{Status: node.ToolCallRunning}
	}
	msg := "the tool failed"
	if res.Error != nil {
		if res.Error.Message != "" {
			msg = res.Error.Message
		}
		if res.Error.NodeID != "" {
			msg = fmt.Sprintf("node %s (%s): %s", res.Error.NodeID, res.Error.NodeType, msg)
		}
	}
	return node.ToolCallOutcome{Status: node.ToolCallError, Error: capText(msg, maxErrorMessageBytes)}
}

// withIssues puts the problems of an invalid_arguments refusal in its
// message: a model reads the message, not the issues. Each issue echoes at
// most what the model sent (ADR-0021).
func withIssues(err error) error {
	te, ok := err.(*ToolError)
	if !ok || te.Kind != ErrInvalidArguments || len(te.Issues) == 0 {
		return err
	}
	parts := make([]string, len(te.Issues))
	for i, issue := range te.Issues {
		parts[i] = issue.String()
	}
	c := *te
	c.Message = capText(te.Message+": "+strings.Join(parts, "; "), maxErrorMessageBytes)
	return &c
}
