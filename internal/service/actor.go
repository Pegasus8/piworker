// Package service holds the operations shared by the REST API and the MCP
// endpoint (docs/design/mcp.md §6): both are thin adapters over it, so
// permissions, argument validation, limits, approvals, redaction and audit
// are enforced in one place.
package service

import (
	"slices"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
)

// ActorKind says who is calling.
type ActorKind string

const (
	// ActorAgent is an MCP agent authenticated by an API token.
	ActorAgent ActorKind = "agent"
	// ActorEditor is the admin in the editor (cookie session).
	ActorEditor ActorKind = "editor"
	// ActorAgentNode is an agent node of a running flow
	// (process-llm-agent, ADR-0039). It has no token: it may call the
	// tools its settings list (see agentNodeAccess).
	ActorAgentNode ActorKind = "agent-node"
)

// Actor is the caller of a service operation. For agents it carries what
// their token may do; the editor may do everything the service offers.
type Actor struct {
	Kind       ActorKind
	TokenID    string
	TokenName  string
	Scopes     []string
	Privileged bool
	Grants     []storage.TokenGrant
	AllTools   bool
	// FlowID and NodeID name an agent node (ActorAgentNode), Tools the tool
	// names its settings list; its Privileged is allowPrivileged. Waiting
	// are the flows that wait for the run the node serves
	// (node.AgentCaller.Waiting).
	FlowID, NodeID string
	Tools          []string
	Waiting        []string
}

// AgentNodeActor is the actor of an agent node's tool calls.
func AgentNodeActor(c node.AgentCaller) Actor {
	return Actor{
		Kind:       ActorAgentNode,
		FlowID:     c.FlowID,
		NodeID:     c.NodeID,
		Tools:      slices.Clone(c.Tools),
		Privileged: c.AllowPrivileged,
		Waiting:    slices.Clone(c.Waiting),
	}
}

// AgentActor is the actor of a verified agent token.
func AgentActor(info agentauth.TokenInfo) Actor {
	return Actor{
		Kind:       ActorAgent,
		TokenID:    info.ID,
		TokenName:  info.Name,
		Scopes:     slices.Clone(info.Scopes),
		Privileged: info.Privileged,
		Grants:     slices.Clone(info.Grants),
		AllTools:   info.AllTools,
	}
}

// actorFromToken is AgentActor for a stored token (approval execution).
func actorFromToken(tok *storage.APIToken) Actor {
	return AgentActor(agentauth.TokenInfo{
		ID: tok.ID, Name: tok.Name, Scopes: tok.Scopes, Privileged: tok.Privileged,
		Grants: tok.Grants, AllTools: tok.AllTools, ExpiresAt: tok.ExpiresAt,
	})
}

// EditorActor is the admin using the editor.
func EditorActor() Actor { return Actor{Kind: ActorEditor} }

// HasScope reports whether an agent's token carries scope.
func (a Actor) HasScope(scope string) bool { return slices.Contains(a.Scopes, scope) }

func (a Actor) isAgent() bool { return a.Kind == ActorAgent }

func (a Actor) isAgentNode() bool { return a.Kind == ActorAgentNode }

// agentView reports whether the actor sees results as agents do (redacted,
// errors of nodes with hidden settings withheld): MCP agents and agent
// nodes, whose results go to a model.
func (a Actor) agentView() bool { return a.isAgent() || a.isAgentNode() }

// agentNodeKeyPrefix starts the key of an agent node.
const agentNodeKeyPrefix = "agent-node:"

// runWaiting are the flows that wait for a run the actor starts
// (flow.CallRequest.Waiting): for an agent node, those that wait for the
// run it serves and its own flow; nil for other callers, which wait
// outside any flow.
func (a Actor) runWaiting() []string {
	if !a.isAgentNode() {
		return nil
	}
	waiting := slices.Clone(a.Waiting)
	if !slices.Contains(waiting, a.FlowID) {
		waiting = append(waiting, a.FlowID)
	}
	return waiting
}

// runActor is the "actor" meta of runs the actor starts.
func (a Actor) runActor() string {
	switch {
	case a.isAgent():
		return "token:" + a.TokenName
	case a.isAgentNode():
		return a.key()
	}
	return string(ActorEditor)
}

// key identifies the actor as the owner of a run.
func (a Actor) key() string {
	switch {
	case a.isAgent():
		return "token:" + a.TokenID
	case a.isAgentNode():
		return agentNodeKeyPrefix + a.FlowID + "/" + a.NodeID
	}
	return string(a.Kind)
}

// slotKey is the actor's per-token limit key: a token's, or an agent
// node's, which is limited like one ("" for the editor, which has none).
func (a Actor) slotKey() string {
	if a.isAgent() || a.isAgentNode() {
		return a.key()
	}
	return ""
}

// ToolRef names a tool: by its name, or by its flow and trigger node.
type ToolRef struct {
	Name          string
	FlowID        string
	TriggerNodeID string
}

// ToolByName refers to a tool by its published name.
func ToolByName(name string) ToolRef { return ToolRef{Name: name} }

// ToolByNode refers to the tool of a flow's trigger-tool node.
func ToolByNode(flowID, triggerNodeID string) ToolRef {
	return ToolRef{FlowID: flowID, TriggerNodeID: triggerNodeID}
}

func (r ToolRef) String() string {
	if r.Name != "" {
		return r.Name
	}
	return r.FlowID + "/" + r.TriggerNodeID
}
