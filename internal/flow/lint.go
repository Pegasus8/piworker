package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
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
	// IssueInvalidPosition: a node position is not finite or not within
	// ±MaxPosition. Positions do not affect a run, so deploy does not check
	// them; Lint and ApplyOps do.
	IssueInvalidPosition = "invalid_position"
	// IssueUndefinedSecret: a setting that cannot work without its secrets
	// (node.SecretRequirer: a bot token, a notification URL, a DSN)
	// references a secret the store does not define. The message names the
	// secret, never a value. Unlike the other errors it depends on the
	// secret store, not on the definition: the admin may define the secret
	// later (see EnvironmentIssue).
	IssueUndefinedSecret = "undefined_secret"
)

// EnvironmentIssue reports whether an issue depends on the environment the
// flow is checked in (the secrets defined) rather than on its definition:
// such an error still blocks a deploy, but not a dry-run (whose secret store
// is empty by design) or the storing of a draft (the admin may define the
// secret before approving it).
func EnvironmentIssue(i Issue) bool { return i.Code == IssueUndefinedSecret }

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
// ErrInvalidNodeConfig, HTTP 400). Lint hides setting values in the
// messages when it renders them (hideValues); errs are unchanged.
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
			out = append(out, nodeFinding(i, "", code, IssueError, err.Error(), deployErr))
			continue
		}
		instances[i] = inst
	}
	return instances, out
}

// closeInstances closes, in node order, the instances holding resources
// (node.Closer, ADR-0008) that a check built and will not run. Close errors
// are ignored: the instances never ran.
func closeInstances(instances map[int]node.Node) {
	idx := make([]int, 0, len(instances))
	for i := range instances {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		if c, ok := instances[i].(node.Closer); ok {
			_ = c.Close()
		}
	}
}

// requiredSecretFindings reports every secret a strict setting
// (node.SecretRequirer) of the instances (by node index) references and
// store does not define, in node ID order, then property, then order of
// appearance (deploy's order); the err is the *ValidationError deploy
// returns. Only names are reported, never values.
func requiredSecretFindings(f *Flow, instances map[int]node.Node, store *secrets.Store) []finding {
	idx := make([]int, 0, len(instances))
	for i := range instances {
		idx = append(idx, i)
	}
	sort.Slice(idx, func(a, b int) bool {
		if f.Nodes[idx[a]].ID != f.Nodes[idx[b]].ID {
			return f.Nodes[idx[a]].ID < f.Nodes[idx[b]].ID
		}
		return idx[a] < idx[b]
	})
	var out []finding
	for _, i := range idx {
		req, ok := instances[i].(node.SecretRequirer)
		if !ok {
			continue
		}
		required := req.RequiredSecrets()
		props := make([]string, 0, len(required))
		for prop := range required {
			props = append(props, prop)
		}
		sort.Strings(props)
		n := &f.Nodes[i]
		for _, prop := range props {
			for _, name := range required[prop] {
				if _, defined := store.Get(name); defined {
					continue
				}
				msg := fmt.Sprintf("config.%s references secret %q, which is not defined", prop, name)
				err := NewValidationError("node %s (%s): %s", n.ID, n.Type, msg)
				out = append(out, nodeFinding(i, prop, IssueUndefinedSecret, IssueError, msg, err))
			}
		}
	}
	return out
}

// minHiddenLength is the shortest setting value hideValues replaces in
// place. A shorter Sensitive value found in a message hides the whole
// message; a shorter value that is only not AgentVisible is left (it says
// little, and replacing it would mangle the message).
const minHiddenLength = 4

// shownValuePrefix is how many runes of an offending string node.CheckConfig
// quotes (its maxShownValue) before cutting it with "…".
const shownValuePrefix = 32

// wholeMessageHidden replaces a message that quotes a short Sensitive value.
const wholeMessageHidden = "the node rejected its configuration (details hidden: they quote a sensitive setting)"

// hiddenForm is one way a message may quote a hidden setting value.
type hiddenForm struct {
	text      string
	value     string // the value it quotes
	sensitive bool
}

// hideValues removes from msg, a message about node n, the values of the
// settings agents may not read (the read views' rule: Sensitive, not
// AgentVisible, undeclared, or of a type without a schema): strings at any
// depth (and numbers, as text), raw and in the forms careless node errors
// quote them (%q, %+q, JSON with and without HTML escaping, and
// CheckConfig's cut-off prefix). Lint messages are shown to agents, and with
// ops or a redacted draft they cover the admin's nodes too.
func hideValues(msg string, reg *node.Registry, n *Node) string {
	if n == nil || len(n.Config) == 0 {
		return msg
	}
	var schema map[string]node.ConfigProperty
	if info, ok := reg.Get(n.Type); ok && info.Config != nil {
		schema = info.Config.Properties
	}
	var forms []hiddenForm
	for name, v := range n.Config {
		p, declared := schema[name]
		if declared && p.AgentVisible && !p.Sensitive {
			continue
		}
		for _, value := range appendLeafStrings(nil, v) {
			for _, text := range quotedForms(value) {
				forms = append(forms, hiddenForm{text: text, value: value, sensitive: p.Sensitive})
			}
		}
	}
	sort.SliceStable(forms, func(a, b int) bool { return len(forms[a].text) > len(forms[b].text) })
	for _, f := range forms {
		if f.text == "" || !strings.Contains(msg, f.text) {
			continue
		}
		if len(f.value) < minHiddenLength {
			if f.sensitive {
				return wholeMessageHidden
			}
			continue
		}
		msg = strings.ReplaceAll(msg, f.text, HiddenValue)
	}
	return msg
}

// quotedForms returns v and the forms a message may quote it in: the
// insides of its Go (%q, %+q) and JSON quotings, and for a long v its first
// shownValuePrefix runes, raw and Go-quoted.
func quotedForms(v string) []string {
	inner := func(q string) string { return q[1 : len(q)-1] }
	forms := []string{v, inner(strconv.Quote(v)), inner(strconv.QuoteToASCII(v))}
	if js, err := json.Marshal(v); err == nil {
		forms = append(forms, inner(string(js)))
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err == nil {
		forms = append(forms, inner(strings.TrimSpace(buf.String())))
	}
	if runes := []rune(v); len(runes) > shownValuePrefix {
		prefix := string(runes[:shownValuePrefix])
		forms = append(forms, prefix, inner(strconv.Quote(prefix)))
	}
	return forms
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
// of the tool risk analysis and the positions out of range
// (IssueInvalidPosition, which deploy does not check), located by JSON-pointer-like paths ("/nodes",
// "/nodes/{nodeId}", "/nodes/{nodeId}/config/{prop}", "/connections/{id}"),
// in a deterministic order: flow-level issues first, then nodes in
// definition order, then connections in definition order; within one
// location by path, errors before warnings, then code and message. It never
// modifies f and never returns nil; the node instances it builds are closed
// before it returns. Messages never quote the values of Sensitive
// properties. The secrets strict settings require are checked against
// secrets.DefaultStore, the store deploy resolves them in (LintContext takes
// the context's). A flow without error issues deploys, except for what only
// starting it can tell (an exclusive resource already taken: 409).
func Lint(f *Flow, reg *node.Registry) []Issue {
	return LintContext(context.Background(), f, reg)
}

// LintContext is Lint checking the secrets strict settings require
// (IssueUndefinedSecret) against the store of ctx (secrets.FromContext,
// DefaultStore when ctx carries none), exactly as deploy checks them.
func LintContext(ctx context.Context, f *Flow, reg *node.Registry) []Issue {
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
	defer closeInstances(instances)
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

	// Callable triggers need an active connection: from one of the
	// trigger's output ports to an enabled node of a registered type. A
	// target that fails its own checks still counts (deploy reports it
	// first; Lint reports it once, on that node).
	byID := map[string]node.Node{}
	runnable := map[string]bool{}
	for i := range f.Nodes {
		n := &f.Nodes[i]
		if in, ok := instances[i]; ok {
			byID[n.ID] = in
		}
		if n.Enabled && reg.Has(n.Type) {
			runnable[n.ID] = true
		}
	}
	all = append(all, callTriggerFindings(f, byID, func(id string) bool {
		_, outputs := byID[id].Ports()
		for _, c := range f.Connections {
			if c.SourceNode == id && runnable[c.TargetNode] && hasPort(outputs, c.SourcePort) {
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

	// Secrets the strict settings cannot work without (deploy's last check).
	all = append(all, requiredSecretFindings(f, instances, secrets.FromContext(ctx))...)

	all = append(all, positionFindings(f)...)
	return lintIssues(f, reg, all)
}

// positionFindings reports the nodes whose position ValidPosition refuses.
// Positions do not affect a run, so only Lint (not deploy) checks them: they
// bound what a draft can store and the layout has to work with.
func positionFindings(f *Flow) []finding {
	var out []finding
	for i := range f.Nodes {
		if !ValidPosition(f.Nodes[i].Position) {
			out = append(out, nodeFinding(i, "", IssueInvalidPosition, IssueError, positionMessage, nil))
		}
	}
	return out
}

// lintIssues renders findings as Issues in Lint's order, once each, with
// the setting values agents may not read hidden from node messages.
func lintIssues(f *Flow, reg *node.Registry, fds []finding) []Issue {
	type keyed struct {
		section, index int
		issue          Issue
	}
	seen := map[Issue]bool{}
	list := make([]keyed, 0, len(fds))
	for _, fd := range fds {
		msg := fd.msg
		if fd.node >= 0 && fd.node < len(f.Nodes) {
			msg = hideValues(msg, reg, &f.Nodes[fd.node])
		}
		k := keyed{issue: Issue{Path: fd.pointerPath(f), Code: fd.code, Severity: fd.sev, Message: msg}}
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
// or action-notify URL, or of an action-mqtt broker) or any RiskSwitch
// property of its schema turned on (allowPrivate, allowDashArgs, …) makes it
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
	if info.Config != nil {
		for name, p := range info.Config.Properties {
			if p.RiskSwitch && configBool(cfg, name) {
				return node.RiskPrivileged
			}
		}
	}
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
