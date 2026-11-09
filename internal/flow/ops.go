package flow

import (
	"fmt"
	"regexp"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/google/uuid"
)

// Flow edit operations (docs/design/mcp.md §10): how an agent changes a flow
// without resending all of it. ApplyOps applies them to a copy of a base
// flow; Lint then checks the result like any other flow.

// Op kinds.
const (
	OpAddNode    = "add_node"
	OpUpdateNode = "update_node"
	OpRemoveNode = "remove_node"
	OpConnect    = "connect"
	OpDisconnect = "disconnect"
)

// MaxOps bounds the operations of one ApplyOps call.
const MaxOps = 200

// Issue codes of ApplyOps (path "/ops/{index}", or "/ops" for the batch).
const (
	// IssueTooManyOps: more than MaxOps operations.
	IssueTooManyOps = "too_many_ops"
	// IssueUnknownOp: the op field names no operation.
	IssueUnknownOp = "unknown_op"
	// IssueInvalidOp: a required field is missing, or the fields conflict.
	IssueInvalidOp = "invalid_op"
	// IssueInvalidNodeID: a node ID given to add_node is not 1-64 letters,
	// digits, '-' or '_'.
	IssueInvalidNodeID = "invalid_node_id"
	// IssueNodeExists: add_node with the ID of an existing node.
	IssueNodeExists = "node_exists"
	// IssueNodeNotFound: an operation names a node that does not exist.
	IssueNodeNotFound = "node_not_found"
	// IssueConnectionExists: connect with the endpoints of an existing
	// connection.
	IssueConnectionExists = "connection_exists"
	// IssueConnectionNotFound: disconnect matches no connection.
	IssueConnectionNotFound = "connection_not_found"
)

// positionMessage explains IssueInvalidPosition (shared with Lint).
var positionMessage = fmt.Sprintf("a node position must be finite and within ±%d on both axes", int(MaxPosition))

// Op is one edit operation. Which fields apply depends on Op:
//
//	{"op":"add_node","node":{"id"?,"type","config"?,"enabled"?,"position"?}}
//	{"op":"update_node","nodeId","config"?,"enabled"?,"position"?}
//	{"op":"remove_node","nodeId"}
//	{"op":"connect","sourceNode","sourcePort","targetNode","targetPort"}
//	{"op":"disconnect","connectionId"} or with the four endpoint fields
//
// update_node's config is a JSON merge patch (RFC 7386): a key set to null
// is deleted, an object is merged into an object, anything else replaces.
type Op struct {
	Op           string         `json:"op"`
	Node         *OpNode        `json:"node,omitempty"`
	NodeID       string         `json:"nodeId,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
	Enabled      *bool          `json:"enabled,omitempty"`
	Position     *Position      `json:"position,omitempty"`
	SourceNode   string         `json:"sourceNode,omitempty"`
	SourcePort   string         `json:"sourcePort,omitempty"`
	TargetNode   string         `json:"targetNode,omitempty"`
	TargetPort   string         `json:"targetPort,omitempty"`
	ConnectionID string         `json:"connectionId,omitempty"`
}

// OpNode is the node of an add_node operation. Its category and ports come
// from the registry; an absent ID is generated, an absent position is chosen
// by AutoLayout, and enabled defaults to true.
type OpNode struct {
	ID       string         `json:"id,omitempty"`
	Type     string         `json:"type"`
	Config   map[string]any `json:"config,omitempty"`
	Enabled  *bool          `json:"enabled,omitempty"`
	Position *Position      `json:"position,omitempty"`
}

// OpNodeIDPattern is the syntax of the node IDs an operation may choose:
// they appear in paths (/nodes/{id}) and URLs. The MCP endpoint advertises
// and checks it as it is.
const OpNodeIDPattern = `^[A-Za-z0-9_-]{1,64}$`

var opNodeIDPattern = regexp.MustCompile(OpNodeIDPattern)

// ApplyOps applies ops in order to a copy of base (nil: an empty flow) and
// returns it, with the nodes added without a position placed by AutoLayout.
// base is never modified. Every failing operation is reported (path
// "/ops/{index}"; later operations see the effect of the earlier ones that
// succeeded); any issue means no flow is returned. More than MaxOps
// operations is a single issue at "/ops". ApplyOps checks the operations
// only: Lint checks the resulting flow.
func ApplyOps(base *Flow, ops []Op, reg *node.Registry) (*Flow, []Issue) {
	if len(ops) > MaxOps {
		return nil, []Issue{{Path: "/ops", Code: IssueTooManyOps, Severity: IssueError,
			Message: fmt.Sprintf("%d operations; at most %d are applied at once", len(ops), MaxOps)}}
	}
	if reg == nil {
		reg = node.NewRegistry()
	}
	next := cloneFlow(base)
	var (
		issues   []Issue
		unplaced []string
	)
	for i, op := range ops {
		if code, msg := applyOp(next, op, reg, &unplaced); code != "" {
			issues = append(issues, Issue{Path: fmt.Sprintf("/ops/%d", i), Code: code, Severity: IssueError, Message: msg})
		}
	}
	if len(issues) > 0 {
		return nil, issues
	}
	AutoLayout(next, unplaced)
	return next, []Issue{}
}

// applyOp applies one operation to f, or returns the code and message of
// why it cannot (f is then unchanged).
func applyOp(f *Flow, op Op, reg *node.Registry, unplaced *[]string) (string, string) {
	switch op.Op {
	case OpAddNode:
		return addNodeOp(f, op, reg, unplaced)
	case OpUpdateNode:
		if op.NodeID == "" {
			return IssueInvalidOp, "update_node needs nodeId"
		}
		n := f.GetNode(op.NodeID)
		if n == nil {
			return IssueNodeNotFound, fmt.Sprintf("there is no node %q", op.NodeID)
		}
		if op.Config == nil && op.Enabled == nil && op.Position == nil {
			return IssueInvalidOp, "update_node changes nothing: give config, enabled or position"
		}
		if op.Position != nil && !ValidPosition(*op.Position) {
			return IssueInvalidPosition, positionMessage
		}
		if op.Config != nil {
			if n.Config == nil {
				n.Config = map[string]any{}
			}
			mergePatch(n.Config, op.Config)
		}
		if op.Enabled != nil {
			n.Enabled = *op.Enabled
		}
		if op.Position != nil {
			n.Position = *op.Position
		}
	case OpRemoveNode:
		if op.NodeID == "" {
			return IssueInvalidOp, "remove_node needs nodeId"
		}
		if f.GetNode(op.NodeID) == nil {
			return IssueNodeNotFound, fmt.Sprintf("there is no node %q", op.NodeID)
		}
		f.RemoveNode(op.NodeID)
	case OpConnect:
		return connectOp(f, op, reg)
	case OpDisconnect:
		return disconnectOp(f, op)
	default:
		return IssueUnknownOp, fmt.Sprintf("unknown op %q (add_node, update_node, remove_node, connect, disconnect)", op.Op)
	}
	return "", ""
}

func addNodeOp(f *Flow, op Op, reg *node.Registry, unplaced *[]string) (string, string) {
	spec := op.Node
	if spec == nil {
		return IssueInvalidOp, "add_node needs node"
	}
	if spec.Type == "" {
		return IssueInvalidOp, "add_node needs node.type"
	}
	info, ok := reg.Get(spec.Type)
	if !ok {
		return IssueUnknownType, fmt.Sprintf("unknown node type: %s", spec.Type)
	}
	id := spec.ID
	if id == "" {
		id = uuid.NewString()
	} else if !opNodeIDPattern.MatchString(id) {
		return IssueInvalidNodeID, "a node ID must be 1-64 letters, digits, '-' or '_'"
	}
	if f.GetNode(id) != nil {
		return IssueNodeExists, fmt.Sprintf("a node %q already exists", id)
	}
	if spec.Position != nil && !ValidPosition(*spec.Position) {
		return IssueInvalidPosition, positionMessage
	}
	n := Node{
		ID:       id,
		Type:     spec.Type,
		Category: info.Category,
		Config:   map[string]any{},
		Inputs:   append([]Port{}, info.Inputs...),
		Outputs:  append([]Port{}, info.Outputs...),
		Enabled:  true,
	}
	for k, v := range spec.Config {
		if v != nil {
			n.Config[k] = cloneValue(v)
		}
	}
	if spec.Enabled != nil {
		n.Enabled = *spec.Enabled
	}
	if spec.Position != nil {
		n.Position = *spec.Position
	} else {
		*unplaced = append(*unplaced, id)
	}
	f.Nodes = append(f.Nodes, n)
	return "", ""
}

// opPorts returns a node's ports: its registered type's, or the stored ones
// for an unregistered type.
func opPorts(n *Node, reg *node.Registry) (inputs, outputs []Port) {
	if info, ok := reg.Get(n.Type); ok {
		return info.Inputs, info.Outputs
	}
	return n.Inputs, n.Outputs
}

func connectOp(f *Flow, op Op, reg *node.Registry) (string, string) {
	if op.SourceNode == "" || op.SourcePort == "" || op.TargetNode == "" || op.TargetPort == "" {
		return IssueInvalidOp, "connect needs sourceNode, sourcePort, targetNode and targetPort"
	}
	src, dst := f.GetNode(op.SourceNode), f.GetNode(op.TargetNode)
	if src == nil {
		return IssueNodeNotFound, fmt.Sprintf("there is no node %q", op.SourceNode)
	}
	if dst == nil {
		return IssueNodeNotFound, fmt.Sprintf("there is no node %q", op.TargetNode)
	}
	if op.SourceNode == op.TargetNode {
		return IssueSelfLoop, fmt.Sprintf("a node cannot be connected to itself (%s)", op.SourceNode)
	}
	if _, outputs := opPorts(src, reg); !hasPort(outputs, op.SourcePort) {
		return IssueConnectionUnknownPort, fmt.Sprintf("node %s (%s) has no output port %q", src.ID, src.Type, op.SourcePort)
	}
	if inputs, _ := opPorts(dst, reg); !hasPort(inputs, op.TargetPort) {
		return IssueConnectionUnknownPort, fmt.Sprintf("node %s (%s) has no input port %q", dst.ID, dst.Type, op.TargetPort)
	}
	for _, c := range f.Connections {
		if c.SourceNode == op.SourceNode && c.SourcePort == op.SourcePort && c.TargetNode == op.TargetNode && c.TargetPort == op.TargetPort {
			return IssueConnectionExists, fmt.Sprintf("connection %s already connects these ports", c.ID)
		}
	}
	f.Connections = append(f.Connections, *NewConnection(op.SourceNode, op.SourcePort, op.TargetNode, op.TargetPort))
	return "", ""
}

func disconnectOp(f *Flow, op Op) (string, string) {
	endpoints := op.SourceNode != "" || op.SourcePort != "" || op.TargetNode != "" || op.TargetPort != ""
	match := func(c Connection) bool {
		return c.SourceNode == op.SourceNode && c.SourcePort == op.SourcePort && c.TargetNode == op.TargetNode && c.TargetPort == op.TargetPort
	}
	switch {
	case op.ConnectionID != "" && endpoints:
		return IssueInvalidOp, "disconnect takes connectionId or the four endpoint fields, not both"
	case op.ConnectionID != "":
		match = func(c Connection) bool { return c.ID == op.ConnectionID }
	case op.SourceNode == "" || op.SourcePort == "" || op.TargetNode == "" || op.TargetPort == "":
		return IssueInvalidOp, "disconnect needs connectionId, or sourceNode, sourcePort, targetNode and targetPort"
	}
	kept := make([]Connection, 0, len(f.Connections))
	for _, c := range f.Connections {
		if !match(c) {
			kept = append(kept, c)
		}
	}
	if len(kept) == len(f.Connections) {
		return IssueConnectionNotFound, "no connection matches"
	}
	f.Connections = kept
	return "", ""
}

// mergePatch applies an RFC 7386 merge patch to target in place: null
// deletes, an object merges into an object, anything else replaces (with a
// copy, so target never shares values with the patch).
func mergePatch(target, patch map[string]any) {
	for k, v := range patch {
		if v == nil {
			delete(target, k)
			continue
		}
		if pv, ok := v.(map[string]any); ok {
			if tv, ok := target[k].(map[string]any); ok {
				mergePatch(tv, pv)
				continue
			}
			obj := map[string]any{}
			mergePatch(obj, pv)
			target[k] = obj
			continue
		}
		target[k] = cloneValue(v)
	}
}

// cloneFlow deep-copies a flow's definition (nil: an empty flow).
func cloneFlow(f *Flow) *Flow {
	out := &Flow{Nodes: []Node{}, Connections: []Connection{}}
	if f == nil {
		return out
	}
	out.ID, out.Name, out.Description = f.ID, f.Name, f.Description
	out.State, out.Version = f.State, f.Version
	out.CreatedAt, out.UpdatedAt = f.CreatedAt, f.UpdatedAt
	if f.Variables != nil {
		out.Variables, _ = cloneValue(f.Variables).(map[string]any)
	}
	for _, n := range f.Nodes {
		c := n
		if n.Config != nil {
			c.Config, _ = cloneValue(n.Config).(map[string]any)
		}
		c.Inputs, c.Outputs = clonePorts(n.Inputs), clonePorts(n.Outputs)
		out.Nodes = append(out.Nodes, c)
	}
	out.Connections = append(out.Connections, f.Connections...)
	return out
}

// clonePorts copies a port list, keeping nil as nil.
func clonePorts(ports []Port) []Port {
	if ports == nil {
		return nil
	}
	return append(make([]Port, 0, len(ports)), ports...)
}

// cloneValue deep-copies JSON-like values (maps and slices of any).
func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = cloneValue(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = cloneValue(e)
		}
		return out
	case []string:
		return append([]string(nil), x...)
	}
	return v
}
