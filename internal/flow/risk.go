package flow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/toolschema"
)

// Tool risk analysis (docs/design/mcp.md §5, ADR-0013). For every published
// tool (an enabled node providing a node.ToolSpec) it walks the enabled part
// of the flow reachable from the tool and computes the tool's effective risk,
// its MCP annotations and its fingerprint, and reports the configurations that
// would let an agent's arguments do more than the admin published: dynamic
// destinations carrying credentials, shared state written by the tool, and
// stateful nodes that would mix calls. Deploy refuses a flow with an error
// issue (HTTP 400, ADR-0007's single validation path); flow.Lint can collect
// the same issues.
//
// A tool may set flow variables, which every branch of its flow can read. So
// when a tool reaches a set-var, the nodes of the flow that read flow
// variables, and everything downstream of them, count as reached by the tool
// too (for its risk, credentials and shared state, not for the rules on
// stateful nodes, which concern the tool's own calls).

// IssueSeverity says whether an Issue fails the deploy.
type IssueSeverity string

const (
	// IssueError fails the deploy.
	IssueError IssueSeverity = "error"
	// IssueWarning is logged and shown, but the flow deploys.
	IssueWarning IssueSeverity = "warning"
)

// Issue codes of the tool risk analysis.
const (
	// IssueDynamicDestinationCredentials: a node whose destination comes
	// from message data or variables also carries static credentials
	// (headers, URL userinfo or secrets, a password), which an agent could
	// then send anywhere.
	IssueDynamicDestinationCredentials = "dynamic_destination_credentials"
	// IssueToolGlobalVariable: a set-var reachable from a tool writes a
	// global variable, which other flows read (their URLs, commands, …).
	IssueToolGlobalVariable = "tool_global_variable"
	// IssueToolAggregate: process-aggregate reachable from a tool would
	// batch the arguments of different calls together.
	IssueToolAggregate = "tool_stateful_aggregate"
	// IssueToolJoinKey: process-join reachable from a tool must be keyed by
	// meta.correlationID, or it would join different calls.
	IssueToolJoinKey = "tool_stateful_join"
	// IssueToolDelayExceedsTimeout (warning): a process-delay reachable from
	// a tool waits longer than the tool's timeout, so calls report running.
	IssueToolDelayExceedsTimeout = "tool_delay_exceeds_timeout"
	// IssueToolStrictArguments: an argument of a privileged tool is not
	// strict (toolschema.Schema.RequireStrictFor).
	IssueToolStrictArguments = "tool_strict_arguments"
	// IssueToolNoSchema: a tool node provides no input schema (a node bug).
	IssueToolNoSchema = "tool_no_schema"
)

// Issue is one problem found in a flow, located by a path such as
// "nodes[2].config.url".
type Issue struct {
	Path     string        `json:"path"`
	Code     string        `json:"code"`
	Severity IssueSeverity `json:"severity"`
	Message  string        `json:"message"`
}

// String renders the issue as "path: message".
func (i Issue) String() string { return i.Path + ": " + i.Message }

// Annotations are the MCP tool hints PiWorker computes for a tool. They are
// always explicit (never omitted) and never lowered by the admin, except
// IdempotentHint, which is the admin's flag.
type Annotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
	IdempotentHint  bool `json:"idempotentHint"`
	OpenWorldHint   bool `json:"openWorldHint"`
}

// ToolInfo is a published tool of a running flow, as computed at deploy.
// It is shared read-only: callers must not modify Schema or OutputSchema.
type ToolInfo struct {
	FlowID, TriggerNodeID, ToolName, Title, Description string
	Schema                                              *toolschema.Schema
	OutputSchema                                        json.RawMessage
	Risk                                                node.Risk
	Annotations                                         Annotations
	// Fingerprint is "sha256:<hex>" over the canonical JSON of the risk, the
	// annotations, the advertised input schema and the confirm mode: grants
	// bind to it, so any change of what the tool may do changes it.
	Fingerprint   string
	Confirm       string
	Timeout       time.Duration
	MaxConcurrent int // effective: the admin's value, or the read-only default
	Cooldown      time.Duration
}

// RequiresUserInteraction reports whether MCP clients are asked to confirm
// calls (_meta["anthropic/requiresUserInteraction"]): every destructive tool,
// and every tool whose confirm mode is not "none" (ADR-0012).
func (t ToolInfo) RequiresUserInteraction() bool {
	return t.Annotations.DestructiveHint || (t.Confirm != "" && t.Confirm != node.ToolConfirmNone)
}

// readOnlyTypes may be reached by a read-only tool, besides process-* nodes.
var readOnlyTypes = map[string]bool{"action-reply": true, "action-log": true}

// AnalyzeTools computes the ToolInfo of every enabled node of f listed in
// specs (keyed by node ID: the ToolSpec of each instantiated tool trigger) and
// returns every issue found, sorted by path. Error issues must fail the
// deploy; issueError builds that error. Disabled nodes and connections
// touching them are ignored, as at runtime.
func AnalyzeTools(f *Flow, reg *node.Registry, specs map[string]node.ToolSpec) ([]ToolInfo, []Issue) {
	g := newToolGraph(f)
	var tools []ToolInfo
	issues := newIssueSet()
	var flowVarReach []int // computed once, when a tool sets a flow variable
	for _, idx := range g.order {
		n := &f.Nodes[idx]
		spec, ok := specs[n.ID]
		if !ok || !n.Enabled {
			continue
		}
		if spec.InputSchema == nil {
			issues.add(Issue{Path: fmt.Sprintf("nodes[%d]", idx), Code: IssueToolNoSchema, Severity: IssueError,
				Message: fmt.Sprintf("tool %q has no input schema", spec.Name)})
			continue
		}
		a := toolAnalysis{risk: node.RiskSafe, readOnly: true, spec: spec, issues: issues}
		direct := g.reachable(n.ID)
		reached := map[int]bool{idx: true}
		setsVars := false
		for _, r := range direct {
			reached[r] = true
			setsVars = setsVars || f.Nodes[r].Type == "set-var"
			a.visit(r, &f.Nodes[r], reg, true)
		}
		if setsVars {
			// A set-var of scope flow (any other scope is an error) is
			// read by every branch of the flow that reads flow variables.
			if flowVarReach == nil {
				flowVarReach = g.flowVariableReach(f)
			}
			for _, r := range flowVarReach {
				if !reached[r] {
					a.visit(r, &f.Nodes[r], reg, false)
				}
			}
		}
		if a.risk == node.RiskPrivileged {
			for _, si := range spec.InputSchema.RequireStrictFor(nil) {
				issues.add(Issue{
					Path:     fmt.Sprintf("nodes[%d].config.inputSchema", idx),
					Code:     IssueToolStrictArguments,
					Severity: IssueError,
					Message: fmt.Sprintf("tool %q is privileged, so its arguments must be strict: %s",
						spec.Name, si.String()),
				})
			}
		}
		tools = append(tools, a.info(f.ID, n.ID))
	}
	return tools, issues.sorted()
}

// issueError returns a *ValidationError listing the error issues, or nil when
// there are none (warnings never fail).
func issueError(issues []Issue) error {
	var msgs []string
	for _, i := range issues {
		if i.Severity == IssueError {
			msgs = append(msgs, i.String())
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return NewValidationError("%s", strings.Join(msgs, "; "))
}

// toolGraph is the enabled subgraph of a flow.
type toolGraph struct {
	order []int               // node indexes, in definition order
	index map[string]int      // node ID → index (enabled nodes only)
	next  map[string][]string // node ID → enabled targets
}

func newToolGraph(f *Flow) *toolGraph {
	g := &toolGraph{index: map[string]int{}, next: map[string][]string{}}
	for i := range f.Nodes {
		g.order = append(g.order, i)
		if f.Nodes[i].Enabled {
			if _, dup := g.index[f.Nodes[i].ID]; !dup {
				g.index[f.Nodes[i].ID] = i
			}
		}
	}
	for _, c := range f.Connections {
		_, srcOK := g.index[c.SourceNode]
		_, dstOK := g.index[c.TargetNode]
		if srcOK && dstOK {
			g.next[c.SourceNode] = append(g.next[c.SourceNode], c.TargetNode)
		}
	}
	return g
}

// reachable returns the indexes of the enabled nodes reachable from root
// (root excluded), in definition order.
func (g *toolGraph) reachable(root string) []int {
	return g.reachableFrom([]string{root}, false)
}

// flowVariableReach returns the indexes of the enabled nodes that read flow
// variables (readsFlowVariables) and of every node reachable from them, in
// definition order; never nil.
func (g *toolGraph) flowVariableReach(f *Flow) []int {
	var readers []string
	for _, idx := range g.order {
		n := &f.Nodes[idx]
		if n.Enabled && g.index[n.ID] == idx && readsFlowVariables(n) {
			readers = append(readers, n.ID)
		}
	}
	return append([]int{}, g.reachableFrom(readers, true)...)
}

// reachableFrom returns the indexes of the enabled nodes reachable from
// roots, and of the roots themselves when withRoots is set, in definition
// order.
func (g *toolGraph) reachableFrom(roots []string, withRoots bool) []int {
	seen := map[string]bool{}
	queue := append([]string(nil), roots...)
	var out []int
	for _, root := range roots {
		if !seen[root] {
			seen[root] = true
			if withRoots {
				out = append(out, g.index[root])
			}
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, nxt := range g.next[id] {
			if seen[nxt] {
				continue
			}
			seen[nxt] = true
			out = append(out, g.index[nxt])
			queue = append(queue, nxt)
		}
	}
	sort.Ints(out)
	return out
}

// issueSet collects issues once each (several tools may reach one node).
type issueSet struct {
	seen map[Issue]bool
	list []Issue
}

func newIssueSet() *issueSet { return &issueSet{seen: map[Issue]bool{}} }

func (s *issueSet) add(i Issue) {
	if !s.seen[i] {
		s.seen[i] = true
		s.list = append(s.list, i)
	}
}

func (s *issueSet) sorted() []Issue {
	out := append([]Issue(nil), s.list...)
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Path != out[b].Path {
			return out[a].Path < out[b].Path
		}
		return out[a].Message < out[b].Message
	})
	return out
}

// toolAnalysis accumulates what one tool can reach.
type toolAnalysis struct {
	spec     node.ToolSpec
	risk     node.Risk
	readOnly bool
	issues   *issueSet
}

func (a *toolAnalysis) raise(r node.Risk) {
	if riskRank(r) > riskRank(a.risk) {
		a.risk = r
	}
}

// riskRank orders risks; anything undeclared counts as privileged.
func riskRank(r node.Risk) int {
	switch r {
	case node.RiskSafe:
		return 0
	case node.RiskExternal:
		return 1
	default:
		return 2
	}
}

func (a *toolAnalysis) issue(idx int, key, code string, sev IssueSeverity, format string, args ...any) {
	path := fmt.Sprintf("nodes[%d]", idx)
	if key != "" {
		path += ".config." + key
	}
	a.issues.add(Issue{Path: path, Code: code, Severity: sev, Message: fmt.Sprintf(format, args...)})
}

// visit applies the rules of one reachable node. direct is false for a node
// the tool reaches only through flow variables: it shares the tool's data,
// not its calls, so the rules on stateful nodes do not apply.
func (a *toolAnalysis) visit(idx int, n *Node, reg *node.Registry, direct bool) {
	if !readOnlyTypes[n.Type] && !strings.HasPrefix(n.Type, "process-") {
		a.readOnly = false
	}
	info, ok := reg.Get(n.Type)
	if !ok {
		a.raise(node.RiskPrivileged)
	} else {
		a.raise(info.Risk)
	}
	cfg := n.Config

	switch n.Type {
	case "action-http":
		dynamic := dynamicDestination(configString(cfg, "url"))
		if dynamic || configBool(cfg, "allowPrivate") {
			a.raise(node.RiskPrivileged)
		}
		if dynamic {
			if httpHasHeaders(cfg) {
				a.credentialIssue(idx, "headers", "headers")
			}
			if urlHasCredentials(configString(cfg, "url")) {
				a.credentialIssue(idx, "url", "the URL's user:password@ part or a {{secret.NAME}}")
			}
		}
	case "action-notify":
		dynamic := dynamicDestination(configString(cfg, "url"))
		if dynamic || configBool(cfg, "allowPrivate") {
			a.raise(node.RiskPrivileged)
		}
		if dynamic && urlHasCredentials(configString(cfg, "url")) {
			a.credentialIssue(idx, "url", "a {{secret.NAME}} or user:password@ in the URL")
		}
	case "action-mqtt":
		// Retained messages and other subscribers make MQTT shared state.
		a.raise(node.RiskPrivileged)
		if dynamicDestination(configString(cfg, "broker")) {
			if configString(cfg, "password") != "" {
				a.credentialIssue(idx, "password", "a password")
			} else if usesSecretPlaceholder(configString(cfg, "username")) {
				a.credentialIssue(idx, "username", "a {{secret.NAME}} username")
			}
		}
	case "action-file":
		// Files are shared state: other flows and programs read them.
		a.raise(node.RiskPrivileged)
	case "set-var":
		if scope := configString(cfg, "scope"); scope != "flow" {
			if scope == "" {
				scope = "global"
			}
			a.issue(idx, "scope", IssueToolGlobalVariable, IssueError,
				"set-var writes the %s variable %q, which other flows can read; a node reachable from a tool (directly, or through the flow variables a tool sets) may only set flow variables (scope: flow)",
				scope, configString(cfg, "key"))
		}
	}
	if !direct {
		return
	}
	switch n.Type {
	case "process-aggregate":
		a.issue(idx, "", IssueToolAggregate, IssueError,
			"process-aggregate would batch the arguments of different tool calls together; it cannot be reached from a tool")
	case "process-join":
		if key, present := cfg["key"]; present {
			if s, ok := key.(string); !ok || strings.TrimSpace(s) != "meta.correlationID" {
				a.issue(idx, "key", IssueToolJoinKey, IssueError,
					"process-join reachable from a tool must use the key meta.correlationID, or it would join different tool calls")
			}
		}
	case "process-delay":
		if ms, ok := configNumber(cfg, "delay"); ok && a.spec.Timeout > 0 {
			ms = math.Min(ms, 300000) // the node's own cap
			if ms > float64(a.spec.Timeout.Milliseconds()) {
				a.issue(idx, "delay", IssueToolDelayExceedsTimeout, IssueWarning,
					"process-delay waits %.0f ms, longer than the %d ms timeout of tool %q: calls will report running and the agent must fetch the result later",
					ms, a.spec.Timeout.Milliseconds(), a.spec.Name)
			}
		}
	}
}

func (a *toolAnalysis) credentialIssue(idx int, key, what string) {
	a.issue(idx, key, IssueDynamicDestinationCredentials, IssueError,
		"the destination comes from message data or variables, so %s would be sent wherever an agent's arguments point; use a fixed scheme and host, or remove the credentials",
		what)
}

// info builds the ToolInfo once every reachable node was visited.
func (a *toolAnalysis) info(flowID, nodeID string) ToolInfo {
	ann := Annotations{
		ReadOnlyHint:    a.readOnly && a.risk == node.RiskSafe,
		DestructiveHint: a.risk == node.RiskPrivileged,
		IdempotentHint:  a.spec.Idempotent,
		OpenWorldHint:   a.risk == node.RiskPrivileged || a.risk == node.RiskExternal,
	}
	confirm := a.spec.Confirm
	if confirm == "" {
		confirm = node.ToolConfirmNone
	}
	return ToolInfo{
		FlowID:        flowID,
		TriggerNodeID: nodeID,
		ToolName:      a.spec.Name,
		Title:         a.spec.Title,
		Description:   a.spec.Description,
		Schema:        a.spec.InputSchema,
		OutputSchema:  a.spec.OutputSchema,
		Risk:          a.risk,
		Annotations:   ann,
		Fingerprint:   toolFingerprint(a.risk, ann, a.spec.InputSchema.JSON(), confirm),
		Confirm:       confirm,
		Timeout:       a.spec.Timeout,
		MaxConcurrent: a.spec.EffectiveMaxConcurrent(ann.ReadOnlyHint),
		Cooldown:      a.spec.Cooldown,
	}
}

// toolFingerprint hashes the canonical JSON of what a grant approves.
func toolFingerprint(risk node.Risk, ann Annotations, schema json.RawMessage, confirm string) string {
	// Fields in alphabetical order; schema is already canonical.
	data, err := json.Marshal(struct {
		Annotations Annotations     `json:"annotations"`
		Confirm     string          `json:"confirm"`
		InputSchema json.RawMessage `json:"inputSchema"`
		Risk        node.Risk       `json:"risk"`
	}{ann, confirm, schema, risk})
	if err != nil {
		// Only an invalid schema document could fail; never match a grant.
		data = []byte("invalid:" + err.Error())
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// readsFlowVariables reports whether a node may read the flow's variables:
// a get-var of scope flow, or any config text naming flowVars (templates and
// expressions) or $env (an expression reading its whole environment, which
// holds flowVars). It errs on the side of yes.
func readsFlowVariables(n *Node) bool {
	if n.Type == "get-var" && configString(n.Config, "scope") == "flow" {
		return true
	}
	return configTextContains(n.Config, "flowVars", "$env")
}

// configTextContains reports whether a string anywhere in v (a config value:
// nested maps and lists included) contains one of subs.
func configTextContains(v any, subs ...string) bool {
	switch x := v.(type) {
	case string:
		for _, sub := range subs {
			if strings.Contains(x, sub) {
				return true
			}
		}
	case map[string]any:
		for _, e := range x {
			if configTextContains(e, subs...) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if configTextContains(e, subs...) {
				return true
			}
		}
	}
	return false
}

// placeholderRe matches template placeholders the way the builtin nodes'
// renderTemplate does ({{payload.x}}, {{vars.y}}, {{secret.NAME}}, …).
var placeholderRe = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// isSecretPlaceholder reports whether a placeholder match is {{secret.NAME}}.
func isSecretPlaceholder(match string) bool {
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}"))
	head, rest, found := strings.Cut(inner, ".")
	return head == "secret" && found && rest != ""
}

// usesSecretPlaceholder reports whether a template references a secret.
func usesSecretPlaceholder(tmpl string) bool {
	for _, m := range placeholderRe.FindAllString(tmpl, -1) {
		if isSecretPlaceholder(m) {
			return true
		}
	}
	return false
}

const (
	dynamicMarker = "pwriskdynamic"
	secretMarker  = "pwrisksecret"
)

// renderMarkers replaces every placeholder by a marker: secrets (fixed by the
// admin) by secretMarker, everything else (message data, variables) by
// dynamicMarker. It reports whether a dynamic placeholder was found.
func renderMarkers(tmpl string) (string, bool) {
	dynamic := false
	out := placeholderRe.ReplaceAllStringFunc(tmpl, func(m string) string {
		if isSecretPlaceholder(m) {
			return secretMarker
		}
		dynamic = true
		return dynamicMarker
	})
	return out, dynamic
}

// dynamicDestination reports whether the scheme, host or port of a URL (or
// MQTT broker) template can come from message data or variables: a
// placeholder other than {{secret.NAME}} in the scheme or host, or one in a
// URL that does not start with a literal scheme and host.
func dynamicDestination(tmpl string) bool {
	rendered, dynamic := renderMarkers(tmpl)
	if !dynamic {
		return false
	}
	u, err := url.Parse(rendered)
	if err != nil || strings.HasPrefix(rendered, dynamicMarker) || u.Scheme == "" || u.Host == "" {
		return true
	}
	return strings.Contains(u.Scheme, dynamicMarker) || strings.Contains(u.Host, dynamicMarker)
}

// urlHasCredentials reports whether a URL template carries credentials: a
// user:password@ part or any {{secret.NAME}}.
func urlHasCredentials(tmpl string) bool {
	if usesSecretPlaceholder(tmpl) {
		return true
	}
	rendered, _ := renderMarkers(tmpl)
	u, err := url.Parse(rendered)
	return err == nil && u.User != nil
}

// httpHasHeaders reports whether an action-http config sets any header.
func httpHasHeaders(cfg map[string]any) bool {
	headers, _ := cfg["headers"].(map[string]any)
	for _, v := range headers {
		if s, ok := v.(string); ok && s != "" {
			return true
		}
	}
	return false
}

func configString(cfg map[string]any, key string) string {
	s, _ := cfg[key].(string)
	return s
}

func configBool(cfg map[string]any, key string) bool {
	b, _ := cfg[key].(bool)
	return b
}

func configNumber(cfg map[string]any, key string) (float64, bool) {
	switch v := cfg[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	}
	return 0, false
}

// toolSpecs returns the specs of the runtime's callable tool triggers.
func (r *FlowRuntime) toolSpecs() map[string]node.ToolSpec {
	specs := map[string]node.ToolSpec{}
	for id, trigger := range r.triggers {
		p, isTool := trigger.(node.ToolProvider)
		_, callable := trigger.(node.Emitter)
		if isTool && callable {
			specs[id] = p.ToolSpec()
		}
	}
	return specs
}

// analyzeRuntimeTools runs the tool risk analysis on a runtime about to
// start: error issues fail the deploy (a *ValidationError), warnings are
// logged, and the tools are kept for Tools.
func (m *RuntimeManager) analyzeRuntimeTools(rt *FlowRuntime) error {
	specs := rt.toolSpecs()
	if len(specs) == 0 {
		return nil
	}
	tools, issues := AnalyzeTools(rt.flow, m.registry, specs)
	if err := issueError(issues); err != nil {
		return err
	}
	for _, i := range issues {
		m.logger.Warn().
			Str("flowID", rt.flow.ID).
			Str("path", i.Path).
			Str("code", i.Code).
			Msg(i.Message)
	}
	rt.tools = tools
	return nil
}

// Tools returns the tools published by the running flows, sorted by tool
// name, and a version that changes on every deploy, undeploy and stop, so a
// caller can cache what it derives from the list. The slice is a copy; its
// Schema and OutputSchema are shared and must not be modified.
func (m *RuntimeManager) Tools() (version uint64, tools []ToolInfo) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, rt := range m.runtimes {
		if rt.IsRunning() {
			tools = append(tools, rt.tools...)
		}
	}
	sort.Slice(tools, func(a, b int) bool {
		if tools[a].ToolName != tools[b].ToolName {
			return tools[a].ToolName < tools[b].ToolName
		}
		if tools[a].FlowID != tools[b].FlowID {
			return tools[a].FlowID < tools[b].FlowID
		}
		return tools[a].TriggerNodeID < tools[b].TriggerNodeID
	})
	return m.toolsVersion, tools
}

// Tools returns the tools this runtime publishes (computed at deploy), in
// definition order. The slice is a copy; Schema and OutputSchema are shared.
func (r *FlowRuntime) Tools() []ToolInfo {
	return append([]ToolInfo(nil), r.tools...)
}
