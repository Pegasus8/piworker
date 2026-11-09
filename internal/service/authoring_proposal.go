package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/google/uuid"
)

// Resolving and checking an agent's proposal (SaveDraft, Validate): the
// request is bounded, the proposed flow built from the current version (ops
// applied to it, or a whole flow replacing it), "<redacted>" resolved to the
// base values, and the authoring rules checked.

// Node types whose instances agents with the author scope may not add,
// change or remove: they define the tools agents call.
const (
	toolTriggerType = "trigger-tool"
	toolReplyType   = "action-reply"
	webhookType     = "trigger-webhook"
	webhookPathProp = "path"
)

// maxAgentIssueBytes bounds an issue message shown to an agent.
const maxAgentIssueBytes = 2 << 10

// proposalRequest is what SaveDraft and Validate resolve.
type proposalRequest struct {
	flowID      string
	baseVersion int
	// checkBase: baseVersion must be the current version (SaveDraft).
	checkBase bool
	flow      *flow.Flow
	ops       []flow.Op
	name      string
	// requireName: a new flow needs a name (SaveDraft).
	requireName bool
	// current: with a flowID and neither flow nor ops, the proposal is the
	// current version as it is (Validate).
	current bool
	// editor: the admin's own proposal (Validate): the author restrictions
	// and the authoring issues do not apply.
	editor bool
}

// proposal is a resolved proposal.
type proposal struct {
	// base is the flow's current version (nil for a new flow).
	base *flow.Flow
	// next is the proposed flow, every "<redacted>" resolved.
	next *flow.Flow
	// issues are the authoring issues (IssueSensitiveLiteral,
	// IssueUnresolvedRedaction, IssueUnknownSecret).
	issues []flow.Issue
	// wrote holds, by node and property, the values the agent wrote (never
	// the sentinel): a whole flow's config values, the configs of add_node
	// ops and the patches of update_node ops (the patch, not the merged
	// result).
	wrote map[propKey][]any
	// kept marks the properties of next that hold base content the agent
	// did not write: resolved from "<redacted>", left alone by the ops, or
	// an object patch merged into the base's object. Only these may be
	// compared with the base: comparing what the agent wrote would let it
	// confirm a guess of a value it may not read.
	kept map[propKey]bool
	// nameKept: next's name is the base's, which the agent did not write.
	nameKept bool
}

// propKey names a config property of a node.
type propKey struct{ node, prop string }

// resolveProposal builds and checks the proposed flow of an agent: request
// shape and limits (invalid_arguments), the base (flow_not_found, the
// base version: conflict), the ops' issues (invalid_arguments with
// FlowIssues), the author restrictions (forbidden). The authoring issues are
// returned with the proposal; the caller lints it.
func (a *Authoring) resolveProposal(req proposalRequest) (*proposal, error) {
	hasFlow, hasOps := req.flow != nil, len(req.ops) > 0
	asIs := req.current && req.flowID != "" && !hasFlow && !hasOps
	switch {
	case hasFlow && hasOps:
		return nil, toolError(ErrInvalidArguments, "send either flow or ops, not both")
	case !hasFlow && !hasOps && !asIs:
		return nil, toolError(ErrInvalidArguments, "flow or ops is required")
	case req.flowID == "" && hasOps:
		return nil, toolError(ErrInvalidArguments, "ops change an existing flow: name it with flowId (a new flow is a whole flow)")
	case req.flowID == "" && req.baseVersion != 0:
		return nil, toolError(ErrInvalidArguments, "a new flow has no baseVersion")
	case utf8.RuneCountInString(req.name) > MaxDraftNameChars:
		return nil, toolError(ErrInvalidArguments, fmt.Sprintf("the name is over %d characters", MaxDraftNameChars))
	}
	if hasFlow {
		if err := checkJSONSize("the flow", req.flow); err != nil {
			return nil, err
		}
	}
	if hasOps {
		if err := checkJSONSize("the ops", req.ops); err != nil {
			return nil, err
		}
	}

	p := &proposal{}
	if req.flowID != "" {
		base, err := a.loadFlow(req.flowID)
		if err != nil {
			return nil, err
		}
		if req.checkBase {
			if req.baseVersion == 0 {
				return nil, &ToolError{Kind: ErrInvalidArguments, CurrentVersion: base.Version,
					Message: fmt.Sprintf("baseVersion is required for an existing flow: the version the proposal is based on (the current one is %d)", base.Version)}
			}
			if req.baseVersion != base.Version {
				return nil, staleBase(base.Version)
			}
		}
		p.base = base
	}

	p.wrote, p.kept = map[propKey][]any{}, map[propKey]bool{}
	var opsOrigin map[propKey]valueOrigin
	var replaced map[string]bool
	if asIs {
		next, err := cloneFlow(p.base)
		if err != nil {
			return nil, fmt.Errorf("copy flow: %w", err)
		}
		p.next, p.nameKept = next, true
		for i := range next.Nodes {
			for prop := range next.Nodes[i].Config {
				p.kept[propKey{next.Nodes[i].ID, prop}] = true
			}
		}
	} else if hasOps {
		next, issues := flow.ApplyOps(p.base, req.ops, a.registry)
		if len(issues) > 0 {
			return nil, &ToolError{Kind: ErrInvalidArguments, Message: "the ops cannot be applied", FlowIssues: issues}
		}
		p.next, p.nameKept = next, true
		opsOrigin, replaced = opWrites(p.base, req.ops, p.wrote)
	} else {
		next, err := cloneFlow(req.flow)
		if err != nil {
			return nil, toolError(ErrInvalidArguments, "the flow cannot be read")
		}
		a.normalizeNodes(next)
		for i := range next.Nodes {
			for prop, v := range next.Nodes[i].Config {
				recordWrite(p.wrote, propKey{next.Nodes[i].ID, prop}, v)
			}
		}
		if p.base != nil {
			if next.Name == "" {
				next.Name, p.nameKept = p.base.Name, true
			}
			if next.Description == "" {
				next.Description = p.base.Description
			}
			if next.Variables == nil {
				next.Variables = cloneVariables(p.base.Variables)
			}
		}
		flow.AutoLayout(next, newUnplacedNodes(p.base, next))
		p.next = next
	}
	next := p.next
	if req.name != "" {
		next.Name, p.nameKept = req.name, false
	}
	next.ID = req.flowID
	next.State, next.Version = "", 0
	next.CreatedAt, next.UpdatedAt = time.Time{}, time.Time{}
	if req.requireName && p.base == nil && strings.TrimSpace(next.Name) == "" {
		return nil, toolError(ErrInvalidArguments, "a new flow needs a name")
	}
	// The bound is on the names drafts set: a stored name the editor made
	// longer is kept as it is.
	if (p.base == nil || next.Name != p.base.Name) && utf8.RuneCountInString(next.Name) > MaxDraftNameChars {
		return nil, toolError(ErrInvalidArguments, fmt.Sprintf("the name is over %d characters", MaxDraftNameChars))
	}
	if len(next.Nodes) > MaxDraftNodes {
		return nil, toolError(ErrInvalidArguments, fmt.Sprintf("the flow has %d nodes; at most %d", len(next.Nodes), MaxDraftNodes))
	}
	if len(next.Connections) > MaxDraftConnections {
		return nil, toolError(ErrInvalidArguments, fmt.Sprintf("the flow has %d connections; at most %d", len(next.Connections), MaxDraftConnections))
	}

	p.issues = a.resolveRedactions(p.base, next, p.kept)
	if opsOrigin != nil {
		// What the ops left alone (or merged into) on the base's nodes is
		// the base's.
		for i := range next.Nodes {
			n := &next.Nodes[i]
			if replaced[n.ID] || baseNode(p.base, n) == nil {
				continue
			}
			for prop := range n.Config {
				if opsOrigin[propKey{n.ID, prop}] != fromAgent {
					p.kept[propKey{n.ID, prop}] = true
				}
			}
		}
	}
	if req.editor {
		p.issues = nil
		return p, nil
	}
	if err := a.authorRestrictions(p.base, next, p.kept); err != nil {
		return nil, err
	}
	p.issues = append(p.issues, a.sensitiveLiteralIssues(next, p.wrote)...)
	p.issues = append(p.issues, a.unknownSecretIssues(next, p.wrote)...)
	return p, nil
}

// valueOrigin says where the config value of an ops proposal comes from.
type valueOrigin int

const (
	fromBase  valueOrigin = iota // untouched (the zero value)
	fromAgent                    // written by the agent
	mergedIn                     // an object patch merged into the base's object
)

// recordWrite records v as written by the agent at k (the sentinel and null
// write nothing).
func recordWrite(wrote map[propKey][]any, k propKey, v any) {
	if v == nil {
		return
	}
	if s, ok := v.(string); ok && s == RedactedValue {
		return
	}
	wrote[k] = append(wrote[k], v)
}

// opWrites replays the config writes of ops on base: it records the values
// the agent wrote in wrote and returns where each written property's value
// comes from (absent: untouched, the base's) and the node IDs whose base
// node was removed or added again (nothing of theirs is the base's but what
// a sentinel resolves).
func opWrites(base *flow.Flow, ops []flow.Op, wrote map[propKey][]any) (map[propKey]valueOrigin, map[string]bool) {
	origin := map[propKey]valueOrigin{}
	replaced := map[string]bool{}
	baseObject := func(id, prop string) bool {
		if base == nil || replaced[id] {
			return false
		}
		if b := base.GetNode(id); b != nil {
			_, ok := b.Config[prop].(map[string]any)
			return ok
		}
		return false
	}
	for _, op := range ops {
		switch op.Op {
		case flow.OpAddNode:
			if op.Node == nil {
				continue
			}
			replaced[op.Node.ID] = true
			for prop, v := range op.Node.Config {
				k := propKey{op.Node.ID, prop}
				recordWrite(wrote, k, v)
				origin[k] = fromAgent
			}
		case flow.OpRemoveNode:
			replaced[op.NodeID] = true
		case flow.OpUpdateNode:
			for prop, v := range op.Config {
				k := propKey{op.NodeID, prop}
				recordWrite(wrote, k, v)
				_, patch := v.(map[string]any)
				switch {
				case v == RedactedValue:
					origin[k] = fromBase // resolved to the base's value
				case patch && origin[k] != fromAgent && baseObject(op.NodeID, prop):
					origin[k] = mergedIn
				default:
					origin[k] = fromAgent
				}
			}
		}
	}
	return origin, replaced
}

// staleBase is the conflict of a proposal based on another version.
func staleBase(current int) *ToolError {
	return &ToolError{Kind: ErrConflict, CurrentVersion: current,
		Message: fmt.Sprintf("the flow changed since the version the proposal is based on: the current version is %d; read it again and redo the proposal", current)}
}

// checkJSONSize refuses a value over MaxDraftFlowBytes of JSON.
func checkJSONSize(what string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return toolError(ErrInvalidArguments, what+" cannot be encoded as JSON")
	}
	if len(data) > MaxDraftFlowBytes {
		return toolError(ErrInvalidArguments, fmt.Sprintf("%s is over %d bytes of JSON", what, MaxDraftFlowBytes))
	}
	return nil
}

// loadFlow reads a stored flow (unknown: flow_not_found).
func (a *Authoring) loadFlow(flowID string) (*flow.Flow, error) {
	f, err := a.store.GetFlow(flowID)
	if errors.Is(err, storage.ErrFlowNotFound) || errors.Is(err, storage.ErrInvalidFlowData) {
		return nil, toolError(ErrFlowNotFound, fmt.Sprintf("unknown flow %q", capText(flowID, maxViewIDBytes)))
	}
	if err != nil {
		return nil, fmt.Errorf("get flow: %w", err)
	}
	return f, nil
}

// cloneFlow deep-copies a flow through its JSON.
func cloneFlow(f *flow.Flow) (*flow.Flow, error) {
	data, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	return flow.FromJSON(data)
}

func cloneVariables(v map[string]any) map[string]any {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

// normalizeNodes fills, as ApplyOps does for the nodes it adds, the
// category and ports of nodes of registered types (an agent's whole flow
// may omit them), gives a node without config an empty one and a
// connection without an ID a generated one.
func (a *Authoring) normalizeNodes(f *flow.Flow) {
	for i := range f.Nodes {
		n := &f.Nodes[i]
		if info, ok := a.registry.Get(n.Type); ok {
			n.Category = info.Category
			n.Inputs = append([]flow.Port{}, info.Inputs...)
			n.Outputs = append([]flow.Port{}, info.Outputs...)
		}
		if n.Config == nil {
			n.Config = map[string]any{}
		}
	}
	if f.Nodes == nil {
		f.Nodes = []flow.Node{}
	}
	if f.Connections == nil {
		f.Connections = []flow.Connection{}
	}
	for i := range f.Connections {
		if f.Connections[i].ID == "" {
			f.Connections[i].ID = uuid.NewString()
		}
	}
}

// newUnplacedNodes lists the nodes of next without a position that base does
// not have (AutoLayout places them).
func newUnplacedNodes(base, next *flow.Flow) []string {
	var ids []string
	for _, n := range next.Nodes {
		if n.Position == (flow.Position{}) && (base == nil || base.GetNode(n.ID) == nil) {
			ids = append(ids, n.ID)
		}
	}
	return ids
}

// baseNode returns base's node with next node n's ID and type (nil: none, or
// a node of another type).
func baseNode(base *flow.Flow, n *flow.Node) *flow.Node {
	if base == nil {
		return nil
	}
	for i := range base.Nodes {
		if b := &base.Nodes[i]; b.ID == n.ID {
			if b.Type != n.Type {
				return nil
			}
			return b
		}
	}
	return nil
}

// schemaOf returns the config schema of a registered type (nil otherwise).
func (a *Authoring) schemaOf(nodeType string) *node.ConfigSchema {
	if info, ok := a.registry.Get(nodeType); ok {
		return info.Config
	}
	return nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// configPointer is Lint's path of a node's config property.
func configPointer(nodeID, prop string) string {
	return nodePointer(nodeID) + "/config/" + pointerEscape(prop)
}

func nodePointer(nodeID string) string { return "/nodes/" + pointerEscape(nodeID) }

var pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

// pointerEscape escapes a JSON pointer segment (RFC 6901).
func pointerEscape(s string) string { return pointerEscaper.Replace(s) }

// resolveRedactions replaces every top-level config value "<redacted>" of
// next (what the read views show of a hidden value) with base's value of
// that property at the same node ID and type, marking it kept. A sentinel
// without a base value is an IssueUnresolvedRedaction.
func (a *Authoring) resolveRedactions(base, next *flow.Flow, kept map[propKey]bool) []flow.Issue {
	var issues []flow.Issue
	for i := range next.Nodes {
		n := &next.Nodes[i]
		b := baseNode(base, n)
		for _, prop := range sortedKeys(n.Config) {
			if s, ok := n.Config[prop].(string); !ok || s != RedactedValue {
				continue
			}
			if b != nil {
				if bv, ok := b.Config[prop]; ok {
					n.Config[prop] = cloneJSONValue(bv)
					kept[propKey{n.ID, prop}] = true
					continue
				}
			}
			issues = append(issues, flow.Issue{
				Path: configPointer(n.ID, prop), Code: IssueUnresolvedRedaction, Severity: flow.IssueError,
				Message: "\"<redacted>\" keeps the current value of a hidden setting, but the current version has no such setting on a node with this ID and type: set a value instead",
			})
		}
	}
	return issues
}

// cloneJSONValue deep-copies a decoded JSON value.
func cloneJSONValue(v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return v
	}
	return out
}

// sameJSON compares two values by their JSON.
func sameJSON(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ja) == string(jb)
}

// unsetValue reports a value that sets nothing (absent, null or "").
func unsetValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == ""
}

// secretOnly reports whether a Sensitive value holds no literal: every
// string in it is empty or exactly a {{secret.NAME}} reference, and it holds
// no number or boolean.
func secretOnly(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == "" || flow.IsSecretReference(x)
	case map[string]any:
		for _, e := range x {
			if !secretOnly(e) {
				return false
			}
		}
		return true
	case []any:
		for _, e := range x {
			if !secretOnly(e) {
				return false
			}
		}
		return true
	}
	return false
}

// sensitiveLiteralIssues reports the Sensitive properties of next where the
// agent wrote (wrote) anything but {{secret.NAME}} references, whether or
// not it equals the base's value: exempting an equal value would confirm a
// guess of the admin's credential. The message never quotes the value.
func (a *Authoring) sensitiveLiteralIssues(next *flow.Flow, wrote map[propKey][]any) []flow.Issue {
	var issues []flow.Issue
	for _, k := range sortedPropKeys(wrote) {
		n := next.GetNode(k.node)
		if n == nil {
			continue
		}
		schema := a.schemaOf(n.Type)
		if schema == nil || !schema.Properties[k.prop].Sensitive {
			continue
		}
		for _, v := range wrote[k] {
			if !secretOnly(v) {
				issues = append(issues, sensitiveLiteral(configPointer(k.node, k.prop)))
				break
			}
		}
	}
	return issues
}

func sortedPropKeys[T any](m map[propKey]T) []propKey {
	keys := make([]propKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].node != keys[j].node {
			return keys[i].node < keys[j].node
		}
		return keys[i].prop < keys[j].prop
	})
	return keys
}

func sensitiveLiteral(path string) flow.Issue {
	return flow.Issue{Path: path, Code: IssueSensitiveLiteral, Severity: flow.IssueError,
		Message: "a credential setting may only hold {{secret.NAME}} references: ask the admin to store the value as a secret and reference it"}
}

// secretRefRe matches a {{secret.NAME}} reference and captures NAME.
var secretRefRe = regexp.MustCompile(`\{\{\s*secret\.([^}\s]+)\s*\}\}`)

// referencedSecrets returns the secret names referenced in the strings of v,
// sorted and once each.
func referencedSecrets(v any) []string {
	set := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			for _, m := range secretRefRe.FindAllStringSubmatch(x, -1) {
				set[m[1]] = true
			}
		case map[string]any:
			for k, e := range x {
				walk(k)
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(v)
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// unknownSecretIssues warns about the secret references in the values
// written (wrote, by node and property of next) to secrets that are not
// defined: the admin may define them before approving. Only names are shown,
// and only of what was written: the names in settings agents cannot read
// are not revealed.
func (a *Authoring) unknownSecretIssues(next *flow.Flow, wrote map[propKey][]any) []flow.Issue {
	var issues []flow.Issue
	for _, k := range sortedPropKeys(wrote) {
		if next.GetNode(k.node) == nil {
			continue
		}
		for _, name := range referencedSecrets(wrote[k]) {
			if _, ok := a.secrets.Get(name); ok {
				continue
			}
			issues = append(issues, flow.Issue{
				Path: configPointer(k.node, k.prop), Code: IssueUnknownSecret, Severity: flow.IssueWarning,
				Message: fmt.Sprintf("secret %q is not defined: the admin must define it before the flow can use it", capText(name, maxViewIDBytes)),
			})
		}
	}
	return issues
}

// changedValues is what the editor's view of a stored proposal counts as
// written: the config values of next that differ from base's at the same
// node and type.
func changedValues(base, next *flow.Flow) map[propKey][]any {
	out := map[propKey][]any{}
	for i := range next.Nodes {
		n := &next.Nodes[i]
		b := baseNode(base, n)
		for prop, v := range n.Config {
			if b != nil && sameJSON(v, b.Config[prop]) {
				continue
			}
			out[propKey{n.ID, prop}] = []any{v}
		}
	}
	return out
}

// forbiddenChange is the refusal of a change the author scope may not make.
func forbiddenChange(path, what string) *ToolError {
	return toolError(ErrPermissionDenied, fmt.Sprintf("%s: agents may not %s", capText(path, 2*maxViewIDBytes), what))
}

// authorRestrictions refuses (forbidden, naming the path) what agents with
// the author scope may not change: trigger-tool and action-reply nodes
// (added, changed or removed: they define the tools agents call; moving one
// changes nothing), a webhook trigger's path or Sensitive settings (its
// token: any value written there, equal to the base's or not, since agents
// may not read them; only what kept marks stays) or turning one on, a
// RiskSwitch property turned on on an enabled node (enabling a node counts
// as adding it; a disabled node counts as absent), and the legacy
// Variables.
func (a *Authoring) authorRestrictions(base, next *flow.Flow, kept map[propKey]bool) *ToolError {
	isTool := func(t string) bool { return t == toolTriggerType || t == toolReplyType }
	toolWhat := "add, change or remove trigger-tool or action-reply nodes (they define the tools agents call)"
	if base != nil {
		for i := range base.Nodes {
			b := &base.Nodes[i]
			if !isTool(b.Type) {
				continue
			}
			n := next.GetNode(b.ID)
			if n == nil || n.Type != b.Type {
				return forbiddenChange(nodePointer(b.ID), toolWhat)
			}
		}
	}
	for i := range next.Nodes {
		n := &next.Nodes[i]
		b := baseNode(base, n)
		if isTool(n.Type) {
			if b == nil || b.Enabled != n.Enabled || !sameJSON(normConfig(b.Config), normConfig(n.Config)) {
				return forbiddenChange(nodePointer(n.ID), toolWhat)
			}
			continue
		}
		schema := a.schemaOf(n.Type)
		if n.Type == webhookType {
			webhookWhat := "add a webhook trigger or change its path or token (they decide who may start the flow)"
			if b == nil {
				return forbiddenChange(configPointer(n.ID, webhookPathProp), webhookWhat)
			}
			if !b.Enabled && n.Enabled {
				return forbiddenChange(nodePointer(n.ID), "turn on a webhook trigger (it opens its path to requests)")
			}
			props := []string{webhookPathProp}
			if schema != nil {
				for _, name := range sortedSchemaProps(schema) {
					if schema.Properties[name].Sensitive {
						props = append(props, name)
					}
				}
			}
			for _, prop := range props {
				bv, inBase := b.Config[prop]
				nv, inNext := n.Config[prop]
				switch {
				case !inNext:
					if !unsetValue(bv) {
						return forbiddenChange(configPointer(n.ID, prop), webhookWhat) // removed
					}
				case kept[propKey{n.ID, prop}]:
					// the base's value, which the agent did not write
				case !inBase && unsetValue(nv):
					// nothing written over nothing
				default:
					return forbiddenChange(configPointer(n.ID, prop), webhookWhat)
				}
			}
		}
		if schema == nil {
			continue
		}
		for _, prop := range sortedSchemaProps(schema) {
			// Only running nodes count: a disabled node is absent.
			if !schema.Properties[prop].RiskSwitch || !n.Enabled || n.Config[prop] != true {
				continue
			}
			if b == nil || !b.Enabled || b.Config[prop] != true {
				return forbiddenChange(configPointer(n.ID, prop), "turn on a setting that widens what a node may do (the admin decides)")
			}
		}
	}
	var baseVars map[string]any
	if base != nil {
		baseVars = base.Variables
	}
	if len(baseVars) != len(next.Variables) || (len(baseVars) > 0 && !sameJSON(baseVars, next.Variables)) {
		return forbiddenChange("/variables", "change the legacy flow variables")
	}
	return nil
}

// normConfig treats a nil config as an empty one.
func normConfig(cfg map[string]any) map[string]any {
	if cfg == nil {
		return map[string]any{}
	}
	return cfg
}

func sortedSchemaProps(schema *node.ConfigSchema) []string {
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// lint is flow.LintContext with the service's secret store.
func (a *Authoring) lint(ctx context.Context, f *flow.Flow) []flow.Issue {
	return flow.LintContext(secrets.WithStore(ctx, a.secrets), f, a.registry)
}

// mergeIssues merges Lint's issues of f with the authoring issues in Lint's
// order: flow-level first, then nodes and connections in definition order;
// within one location by path, errors first, code, message.
func mergeIssues(f *flow.Flow, lint, extra []flow.Issue) []flow.Issue {
	out := make([]flow.Issue, 0, len(lint)+len(extra))
	out = append(out, lint...)
	out = append(out, extra...)
	nodeRank := map[string]int{}
	for i := range f.Nodes {
		if _, ok := nodeRank[pointerEscape(f.Nodes[i].ID)]; !ok {
			nodeRank[pointerEscape(f.Nodes[i].ID)] = i
		}
	}
	connRank := map[string]int{}
	for i := range f.Connections {
		if _, ok := connRank[pointerEscape(f.Connections[i].ID)]; !ok {
			connRank[pointerEscape(f.Connections[i].ID)] = i
		}
	}
	rank := func(path string) int {
		if rest, ok := strings.CutPrefix(path, "/nodes/"); ok {
			seg, _, _ := strings.Cut(rest, "/")
			if r, ok := nodeRank[seg]; ok {
				return 1 + r
			}
		}
		if rest, ok := strings.CutPrefix(path, "/connections/"); ok {
			seg, _, _ := strings.Cut(rest, "/")
			if r, ok := connRank[seg]; ok {
				return 1 + len(f.Nodes) + r
			}
		}
		return 0
	}
	sev := func(s flow.IssueSeverity) int {
		if s == flow.IssueError {
			return 0
		}
		return 1
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ra, rb := rank(a.Path), rank(b.Path); ra != rb {
			return ra < rb
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if sa, sb := sev(a.Severity), sev(b.Severity); sa != sb {
			return sa < sb
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
	return out
}
