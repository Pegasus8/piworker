package flow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Pegasus8/piworker/internal/node"
)

// Flow linting (docs/design/mcp.md §10, ADR-0007). Deploy and Lint run the
// same checks — the collectors below — in the same order: config typing
// (node.CheckConfig), node instantiation (unknown types, factory and
// Validate errors), structure (IDs, triggers, connections, ports, cycles),
// callable triggers without an active connection, and the tool risk rules of
// AnalyzeTools. Deploy fails on the first error of a stage (config typing and
// the tool rules aggregate theirs) with the same errors and messages as
// before; Lint collects every issue of every stage.

// Issue codes of the structural, registry and config checks. The tool risk
// rules have their own codes (risk.go). Codes are stable: agents and the
// editor match on them.
const (
	// IssueEmptyNodeID: a node has no ID.
	IssueEmptyNodeID = "empty_node_id"
	// IssueDuplicateNodeID: two nodes share an ID.
	IssueDuplicateNodeID = "duplicate_node_id"
	// IssueNoTrigger: a non-empty flow has no enabled trigger.
	IssueNoTrigger = "no_trigger"
	// IssueDuplicateConnectionID: two connections share an ID.
	IssueDuplicateConnectionID = "duplicate_connection_id"
	// IssueConnectionUnknownNode: a connection names a node that does not exist.
	IssueConnectionUnknownNode = "connection_unknown_node"
	// IssueConnectionUnknownPort: a connection names a port its node lacks.
	IssueConnectionUnknownPort = "connection_unknown_port"
	// IssueSelfLoop: a connection connects a node to itself.
	IssueSelfLoop = "self_loop"
	// IssueCycle: the connections form a cycle.
	IssueCycle = "cycle"
	// IssueUnknownType: an enabled node's type is not registered.
	IssueUnknownType = "unknown_type"
	// IssueConfigType: a config value does not match its schema (type,
	// enum, bounds).
	IssueConfigType = "config_type"
	// IssueInvalidConfig: the node's factory or Validate rejected its
	// configuration.
	IssueInvalidConfig = "invalid_config"
	// IssueTriggerNotConnected: a callable trigger (trigger-tool) has no
	// active downstream connection.
	IssueTriggerNotConnected = "trigger_not_connected"
)

// finding is one problem a deploy-time check found, located by indexes so
// that both deploy's legacy paths ("nodes[2].config.url") and Lint's
// JSON-pointer-like paths ("/nodes/{id}/config/url") can be rendered.
type finding struct {
	node int    // index in Flow.Nodes, or -1
	conn int    // index in Flow.Connections, or -1
	path string // a flow-level path ("/nodes") when node and conn are -1
	prop string // config property of the node, "" for the whole node
	code string
	sev  IssueSeverity
	msg  string // the message, without path
	// err is what deploy returns when this is the first error of its stage
	// (nil for the stages that aggregate their messages).
	err error
}

func nodeFinding(idx int, prop, code string, sev IssueSeverity, msg string, err error) finding {
	return finding{node: idx, conn: -1, prop: prop, code: code, sev: sev, msg: msg, err: err}
}

// legacyPath renders the path deploy messages and AnalyzeTools use.
func (fd finding) legacyPath() string {
	switch {
	case fd.node >= 0:
		p := fmt.Sprintf("nodes[%d]", fd.node)
		if fd.prop != "" {
			p += ".config." + fd.prop
		}
		return p
	case fd.conn >= 0:
		return fmt.Sprintf("connections[%d]", fd.conn)
	}
	return strings.TrimPrefix(fd.path, "/")
}

// pointerPath renders Lint's path.
func (fd finding) pointerPath(f *Flow) string {
	switch {
	case fd.node >= 0 && fd.node < len(f.Nodes):
		if f.Nodes[fd.node].ID == "" {
			return "/nodes"
		}
		return nodePointer(f.Nodes[fd.node].ID, fd.prop)
	case fd.conn >= 0 && fd.conn < len(f.Connections):
		if f.Connections[fd.conn].ID == "" {
			return "/connections"
		}
		return "/connections/" + pointerEscape(f.Connections[fd.conn].ID)
	}
	return fd.path
}

var pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

// pointerEscape escapes a JSON pointer reference token (RFC 6901).
func pointerEscape(s string) string { return pointerEscaper.Replace(s) }

// nodePointer is "/nodes/{id}", plus "/config/{prop}" when prop is set.
func nodePointer(id, prop string) string {
	p := "/nodes/" + pointerEscape(id)
	if prop != "" {
		p += "/config/" + pointerEscape(prop)
	}
	return p
}

// firstError returns the err of the first error finding, or nil.
func firstError(fds []finding) error {
	for _, fd := range fds {
		if fd.sev == IssueError && fd.err != nil {
			return fd.err
		}
	}
	return nil
}

// configFindings checks the config of every enabled, registered node against
// its schema (node.CheckConfig). Disabled nodes are never instantiated;
// unregistered types are reported by instantiation.
func configFindings(f *Flow, reg *node.Registry) []finding {
	var out []finding
	for i := range f.Nodes {
		n := &f.Nodes[i]
		if !n.Enabled {
			continue
		}
		info, ok := reg.Get(n.Type)
		if !ok {
			continue
		}
		for _, issue := range node.CheckConfig(info.Config, n.Config) {
			out = append(out, nodeFinding(i, issue.Path, IssueConfigType, IssueError, issue.Message, nil))
		}
	}
	return out
}

// instantiateNodes creates every enabled node (except those in skip) and
// calls its Validate, as deploy does. It returns the instances by node index
// and a finding per node that could not be created or validated; the
// finding's err is the NodeExecutionError deploy returns (wrapping
// ErrInvalidNodeConfig, HTTP 400). Messages hide the node's Sensitive
// values; errs are unchanged.
func instantiateNodes(f *Flow, reg *node.Registry, skip map[int]bool) (map[int]node.Node, []finding) {
	instances := map[int]node.Node{}
	var out []finding
	for i := range f.Nodes {
		n := &f.Nodes[i]
		if !n.Enabled || skip[i] {
			continue
		}
		inst, err := reg.Create(n.Type, n.Config)
		code := IssueInvalidConfig
		if err != nil && !reg.Has(n.Type) {
			code = IssueUnknownType
		}
		if err == nil {
			// Enforce the documented contract that a node's configuration
			// is validated before deploy, so an invalid config fails fast
			// and visibly instead of running with silently-coerced values.
			if err = inst.Validate(); err != nil {
				code = IssueInvalidConfig
			}
		}
		if err != nil {
			// Unknown types are an invalid flow too (HTTP 400).
			deployErr := NewNodeExecutionError(n.ID, n.Type, fmt.Errorf("%w: %w", ErrInvalidNodeConfig, err))
			out = append(out, nodeFinding(i, "", code, IssueError, hideSensitive(err.Error(), reg, n), deployErr))
			continue
		}
		instances[i] = inst
	}
	return instances, out
}

// minHiddenLength is the shortest Sensitive value hideSensitive replaces in
// place; a shorter one found in a message hides the whole message.
const minHiddenLength = 4

// hideSensitive removes from msg the values of n's Sensitive properties
// (strings anywhere inside them), so a node error that quotes its config
// never shows a credential.
func hideSensitive(msg string, reg *node.Registry, n *Node) string {
	info, ok := reg.Get(n.Type)
	if !ok || info.Config == nil {
		return msg
	}
	var values []string
	for name, p := range info.Config.Properties {
		if p.Sensitive {
			values = appendLeafStrings(values, n.Config[name])
		}
	}
	sort.Slice(values, func(a, b int) bool { return len(values[a]) > len(values[b]) })
	for _, v := range values {
		if !strings.Contains(msg, v) {
			continue
		}
		if len(v) < minHiddenLength {
			return "the node rejected its configuration (details hidden: they quote a sensitive setting)"
		}
		msg = strings.ReplaceAll(msg, v, "<hidden>")
	}
	return msg
}

// appendLeafStrings appends the non-empty strings (and numbers, as text)
// inside v.
func appendLeafStrings(out []string, v any) []string {
	switch x := v.(type) {
	case string:
		if x != "" {
			out = append(out, x)
		}
	case float64, int, int64:
		out = append(out, fmt.Sprint(x))
	case map[string]any:
		for _, e := range x {
			out = appendLeafStrings(out, e)
		}
	case []any:
		for _, e := range x {
			out = appendLeafStrings(out, e)
		}
	}
	return out
}

// portsFunc returns the ports of node i, or known=false when they cannot be
// known (an enabled node of an unregistered type), which skips port checks.
type portsFunc func(i int) (inputs, outputs []Port, known bool)

// storedPorts uses the ports stored on the nodes (deploy has just set them
// from the instances).
func storedPorts(f *Flow) portsFunc {
	return func(i int) ([]Port, []Port, bool) { return f.Nodes[i].Inputs, f.Nodes[i].Outputs, true }
}

// structureFindings runs validate's structural checks and collects every
// problem in validate's order, each with the exact error validate returns
// for it: node IDs, an enabled trigger (isTrigger), connections (IDs, nodes,
// self-loops, ports) and cycles. An empty flow is valid.
func structureFindings(f *Flow, isTrigger func(i int) bool, ports portsFunc) []finding {
	if len(f.Nodes) == 0 {
		return nil
	}
	var out []finding
	structural := func(idx, conn int, code string, err *ValidationError) {
		out = append(out, finding{node: idx, conn: conn, code: code, sev: IssueError, msg: err.Message, err: err})
	}

	nodeMap := make(map[string]int, len(f.Nodes))
	for i := range f.Nodes {
		n := &f.Nodes[i]
		if n.ID == "" {
			structural(i, -1, IssueEmptyNodeID, NewValidationError("node at index %d (type %s) has an empty ID", i, n.Type))
			continue
		}
		if _, dup := nodeMap[n.ID]; dup {
			structural(i, -1, IssueDuplicateNodeID, NewValidationError("duplicate node ID %s", n.ID))
			continue
		}
		nodeMap[n.ID] = i
	}

	// If there are nodes, there must be at least one trigger node.
	hasTrigger := false
	for i := range f.Nodes {
		if f.Nodes[i].Enabled && isTrigger(i) {
			hasTrigger = true
			break
		}
	}
	if !hasTrigger {
		out = append(out, finding{node: -1, conn: -1, path: "/nodes", code: IssueNoTrigger, sev: IssueError,
			msg: ErrNoTriggerNodes.Error(), err: ErrNoTriggerNodes})
	}

	// Connections must reference existing nodes and ports.
	connIDs := make(map[string]struct{}, len(f.Connections))
	adjacency := make(map[string][]string)
	for ci, conn := range f.Connections {
		if conn.ID != "" {
			if _, dup := connIDs[conn.ID]; dup {
				structural(-1, ci, IssueDuplicateConnectionID, NewValidationError("duplicate connection ID %s", conn.ID))
				continue
			}
			connIDs[conn.ID] = struct{}{}
		}
		si, ok := nodeMap[conn.SourceNode]
		if !ok {
			structural(-1, ci, IssueConnectionUnknownNode, NewValidationError("connection %s references non-existent source node %s", conn.ID, conn.SourceNode))
			continue
		}
		ti, ok := nodeMap[conn.TargetNode]
		if !ok {
			structural(-1, ci, IssueConnectionUnknownNode, NewValidationError("connection %s references non-existent target node %s", conn.ID, conn.TargetNode))
			continue
		}
		if conn.SourceNode == conn.TargetNode {
			structural(-1, ci, IssueSelfLoop, NewValidationError("connection %s connects node %s to itself", conn.ID, conn.SourceNode))
			continue
		}
		if _, outputs, known := ports(si); known && !hasPort(outputs, conn.SourcePort) {
			structural(-1, ci, IssueConnectionUnknownPort, NewValidationError("connection %s references non-existent source port %s", conn.ID, conn.SourcePort))
			continue
		}
		if inputs, _, known := ports(ti); known && !hasPort(inputs, conn.TargetPort) {
			structural(-1, ci, IssueConnectionUnknownPort, NewValidationError("connection %s references non-existent target port %s", conn.ID, conn.TargetPort))
			continue
		}
		adjacency[conn.SourceNode] = append(adjacency[conn.SourceNode], conn.TargetNode)
	}

	if cycle := findCycle(f.Nodes, adjacency); cycle != "" {
		structural(nodeMap[cycle], -1, IssueCycle, NewValidationError("flow contains a cycle through node %s", cycle))
	}
	return out
}

// callTriggerFindings reports, in node ID order, the callable triggers among
// nodes (instances by node ID: TriggerNode and node.Emitter) for which active
// is false: calls to them could never do anything.
func callTriggerFindings(f *Flow, nodes map[string]node.Node, active func(id string) bool) []finding {
	ids := make([]string, 0, len(nodes))
	for id, inst := range nodes {
		_, isTrigger := inst.(node.TriggerNode)
		_, callable := inst.(node.Emitter)
		if isTrigger && callable {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var out []finding
	for _, id := range ids {
		if active(id) {
			continue
		}
		idx, nodeType := -1, ""
		for i := range f.Nodes {
			if f.Nodes[i].ID == id {
				idx, nodeType = i, f.Nodes[i].Type
				break
			}
		}
		err := NewValidationError("callable trigger %s (%s) has no active connection: connect it to at least one enabled node", id, nodeType)
		out = append(out, nodeFinding(idx, "", IssueTriggerNotConnected, IssueError, err.Message, err))
	}
	return out
}

// Lint returns every issue deploy would find in f (errors) plus the warnings
// of the tool risk analysis, located by JSON-pointer-like paths ("/nodes",
// "/nodes/{nodeId}", "/nodes/{nodeId}/config/{prop}", "/connections/{id}"),
// in a deterministic order: flow-level issues first, then nodes in
// definition order, then connections in definition order; within one
// location by path, errors before warnings, then code and message. It never
// modifies f and never returns nil. Messages never quote the values of
// Sensitive properties. A flow without error issues deploys, except for
// what only starting it can tell (an exclusive resource already taken: 409).
func Lint(f *Flow, reg *node.Registry) []Issue {
	if f == nil {
		return []Issue{}
	}
	if reg == nil {
		reg = node.NewRegistry()
	}
	var all []finding

	// Config typing; a node whose config is mistyped is not instantiated,
	// as deploy stops before instantiating it.
	cfg := configFindings(f, reg)
	all = append(all, cfg...)
	skip := map[int]bool{}
	for _, fd := range cfg {
		skip[fd.node] = true
	}
	instances, instFindings := instantiateNodes(f, reg, skip)
	all = append(all, instFindings...)

	// Structure, with the ports and triggers deploy would see: the
	// instances' for enabled nodes (the registry's when a node could not be
	// instantiated, unknown for unregistered types) and the stored ones for
	// disabled nodes.
	isTrigger := func(i int) bool {
		if inst, ok := instances[i]; ok {
			if _, trigger := inst.(node.TriggerNode); trigger {
				return true
			}
		}
		info, ok := reg.Get(f.Nodes[i].Type)
		return ok && info.Category == NodeCategoryInput
	}
	ports := func(i int) ([]Port, []Port, bool) {
		n := &f.Nodes[i]
		if !n.Enabled {
			return n.Inputs, n.Outputs, true
		}
		if inst, ok := instances[i]; ok {
			in, out := inst.Ports()
			return in, out, true
		}
		if info, ok := reg.Get(n.Type); ok {
			return info.Inputs, info.Outputs, true
		}
		return nil, nil, false
	}
	all = append(all, structureFindings(f, isTrigger, ports)...)

	// Callable triggers need an active connection: to an instantiated node,
	// from one of the trigger's output ports.
	byID := map[string]node.Node{}
	for i := range f.Nodes {
		if in, ok := instances[i]; ok {
			byID[f.Nodes[i].ID] = in
		}
	}
	all = append(all, callTriggerFindings(f, byID, func(id string) bool {
		_, outputs := byID[id].Ports()
		for _, c := range f.Connections {
			if _, target := byID[c.TargetNode]; c.SourceNode == id && target && hasPort(outputs, c.SourcePort) {
				return true
			}
		}
		return false
	})...)

	// Tool risk rules, for the tools deploy would publish.
	specs := map[string]node.ToolSpec{}
	for i := range f.Nodes {
		in, ok := instances[i]
		if !ok {
			continue
		}
		_, trigger := in.(node.TriggerNode)
		p, isTool := in.(node.ToolProvider)
		_, callable := in.(node.Emitter)
		if trigger && isTool && callable {
			specs[f.Nodes[i].ID] = p.ToolSpec()
		}
	}
	_, tools := analyzeTools(f, reg, specs)
	all = append(all, tools...)

	return lintIssues(f, all)
}

// lintIssues renders findings as Issues in Lint's order, once each.
func lintIssues(f *Flow, fds []finding) []Issue {
	type keyed struct {
		section, index int
		issue          Issue
	}
	seen := map[Issue]bool{}
	list := make([]keyed, 0, len(fds))
	for _, fd := range fds {
		k := keyed{issue: Issue{Path: fd.pointerPath(f), Code: fd.code, Severity: fd.sev, Message: fd.msg}}
		switch {
		case fd.node >= 0:
			k.section, k.index = 1, fd.node
		case fd.conn >= 0:
			k.section, k.index = 2, fd.conn
		}
		if seen[k.issue] {
			continue
		}
		seen[k.issue] = true
		list = append(list, k)
	}
	sort.SliceStable(list, func(a, b int) bool {
		x, y := list[a], list[b]
		if x.section != y.section {
			return x.section < y.section
		}
		if x.index != y.index {
			return x.index < y.index
		}
		if x.issue.Path != y.issue.Path {
			return x.issue.Path < y.issue.Path
		}
		if x.issue.Severity != y.issue.Severity {
			return x.issue.Severity == IssueError
		}
		if x.issue.Code != y.issue.Code {
			return x.issue.Code < y.issue.Code
		}
		return x.issue.Message < y.issue.Message
	})
	out := make([]Issue, len(list))
	for i, k := range list {
		out[i] = k.issue
	}
	return out
}

// Risk returns the highest instance risk over the enabled nodes of f (safe
// for an empty or nil flow): each node's type risk, escalated to privileged
// by the same instance rules the tool risk analysis applies (instanceRisk).
func Risk(f *Flow, reg *node.Registry) node.Risk {
	risk := node.RiskSafe
	if f == nil {
		return risk
	}
	for i := range f.Nodes {
		if f.Nodes[i].Enabled {
			risk = maxRisk(risk, instanceRisk(&f.Nodes[i], reg))
		}
	}
	return risk
}

// instanceRisk is a node's type risk escalated by its configuration: a
// dynamic destination (a placeholder in the scheme or host of an action-http
// or action-notify URL, or of an action-mqtt broker) or allowPrivate makes it
// privileged. An unregistered type, or one declaring no valid risk, is
// privileged.
func instanceRisk(n *Node, reg *node.Registry) node.Risk {
	if reg == nil {
		return node.RiskPrivileged
	}
	info, ok := reg.Get(n.Type)
	if !ok || !info.Risk.Valid() {
		return node.RiskPrivileged
	}
	cfg := n.Config
	switch n.Type {
	case "action-http", "action-notify":
		if dynamicDestination(configString(cfg, "url")) || configBool(cfg, "allowPrivate") {
			return node.RiskPrivileged
		}
	case "action-mqtt":
		if dynamicDestination(configString(cfg, "broker")) {
			return node.RiskPrivileged
		}
	}
	return info.Risk
}

// maxRisk returns the higher of two risks (undeclared counts as privileged).
func maxRisk(a, b node.Risk) node.Risk {
	if !b.Valid() {
		b = node.RiskPrivileged
	}
	if riskRank(b) > riskRank(a) {
		return b
	}
	return a
}
