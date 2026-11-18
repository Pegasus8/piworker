package mcpserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/toolschema"
)

// Bounds of the flow-shaped tools' arguments, as JSON: the largest draft
// (service.MaxDraftFlowBytes) plus room for the request around it; a
// dry-run's flow, payload, mocks and flow variables; a node test's config
// and input (each at most flow.DryRunMaxInputBytes). All within the request
// body limit.
const (
	maxFlowArgsBytes     = service.MaxDraftFlowBytes + 64<<10
	maxDryRunArgsBytes   = service.MaxDraftFlowBytes + 4*flow.DryRunMaxInputBytes
	maxTestNodeArgsBytes = 2*flow.DryRunMaxInputBytes + 16<<10
)

// maxPortsPerNode bounds the ports a node of a sent flow may list.
const maxPortsPerNode = 32

// idArg is an id as the read tools take them (1 to 256 characters without
// control characters): a reference to something stored, whatever id an
// older version let it have.
func idArg(description string) *argSchema { return pattern(description, idPattern, maxIDLength) }

// Syntax of the ids a sent flow gives its nodes, connections and ports, and
// of node type ids: what the editor (node_<time>_<n>, node_<uuid>,
// edge_<source>_<target>_<time>), the server (uuids) and add_node generate,
// and dotted or colon-separated ids. Nothing that hides in an issue path
// (/nodes/{id}/config/…), breaks the editor's selectors or reads as other
// text (slashes, quotes, spaces, control or format characters such as bidi
// overrides). A stored flow whose ids are outside it is changed with ops,
// which name nodes by any id (idArg).
const (
	nodeIDPattern       = `^[A-Za-z0-9_.:-]{1,128}$`
	connectionIDPattern = `^[A-Za-z0-9_.:-]{1,256}$`
	portIDPattern       = `^[A-Za-z0-9_.:-]{1,64}$`
	typeIDPattern       = `^[A-Za-z0-9_.:-]{1,128}$`
)

func nodeIDArg(description string) *argSchema { return pattern(description, nodeIDPattern, 128) }
func portIDArg(description string) *argSchema { return pattern(description, portIDPattern, 64) }
func typeArg(description string) *argSchema   { return pattern(description, typeIDPattern, 128) }

// maxFlowNameChars bounds a sent flow's name as the read views bound the
// names they show (256 bytes), so a flow read can be sent back; a draft's
// own name rule (service.MaxDraftNameChars) is the service's.
const maxFlowNameChars = 256

// viewTruncatedValue marks a value the read views (service,
// reads_redact.go) cut: a long string ends with "…<truncated>", an object or
// list too large is replaced by "<truncated>".
const viewTruncatedValue = "<truncated>"

func positionArg() *argSchema {
	return object("The node's position on the editor's canvas (finite, within ±1,000,000); laid out automatically when absent.",
		prop("x", number("Horizontal position.")), prop("y", number("Vertical position."))).require("x", "y")
}

func portArg() *argSchema {
	return object("",
		prop("id", portIDArg("")),
		prop("name", text("", 0, 256)),
		prop("dataType", text("", 0, 32)),
		prop("multiple", boolean("")),
		prop("required", boolean("")),
	).require("id")
}

// maxInt32 bounds the integers of a sent flow.
const maxInt32 = 1<<31 - 1

// flowUse is what a sent flow is for, which decides what "<redacted>" and
// its version mean.
type flowUse int

const (
	// flowForDraft (pw_save_draft): with flowId, "<redacted>" keeps the
	// stored value and the flow's version is its base.
	flowForDraft flowUse = iota
	// flowForValidate (pw_validate_flow): with flowId, "<redacted>" keeps
	// the stored value.
	flowForValidate
	// flowInline (pw_dry_run): no stored flow, so no value to keep.
	flowInline
)

// flowArg is the schema of a flow an agent sends: the definition (id and
// timestamps are the server's), plus the informational fields of the read
// views (pw_get_flow, pw_get_draft) and of an exported flow, which are
// accepted so that a flow read can be sent back, and dropped.
func flowArg(maxNodes, maxConnections int, use flowUse) *argSchema {
	const hidden = ` Values a view cut ("…<truncated>", "<truncated>") or partly hid ("<redacted>" inside a value) are ` +
		`refused: they are not the stored values.`
	config := `The node's settings, as its type's config schema describes them. With flowId, "<redacted>" as a whole ` +
		`setting (as the read views show hidden settings) keeps the current value of that setting of the same node; a new ` +
		`flow has none to keep. A sensitive setting takes only a {{secret.NAME}} reference.` + hidden
	ignored := "id, state, version, createdAt, updatedAt, tools, totalNodes and totalConnections (as the read views and " +
		"exported flows show them) are ignored: the flow is named by flowId, or is new."
	switch use {
	case flowInline:
		config = `The node's settings, as its type's config schema describes them. A flow sent inline has no stored values: ` +
			`"<redacted>" is refused (dry-run a stored flow by flowId, or your draft by draftId, to keep its hidden settings). ` +
			`A sensitive setting takes only a {{secret.NAME}} reference.` + hidden
	case flowForDraft:
		ignored = "With flowId, version (as the read views show it) is the version the flow was read at: the base when " +
			"baseVersion is absent, and it must equal baseVersion otherwise. id, state, createdAt, updatedAt, tools, " +
			"totalNodes and totalConnections (as the read views and exported flows show them) are ignored."
	}
	node := object("A node. name and risk (as the read views show them) are ignored; inputs and outputs default to the type's ports.",
		prop("id", nodeIDArg("The node's id, unique in the flow (letters, digits, '_', '-', '.' or ':'; at most 128).")),
		prop("type", typeArg("The node type, from pw_list_node_types (e.g. trigger-interval, action-http).")),
		prop("enabled", boolean("Whether the node runs (default true).")),
		prop("position", positionArg()),
		prop("config", freeObject(config)),
		prop("category", enum("", "input", "processing", "output")),
		prop("inputs", array("", portArg(), maxPortsPerNode)),
		prop("outputs", array("", portArg(), maxPortsPerNode)),
		prop("name", text("", 0, 256)),
		prop("risk", text("", 0, 32)),
		prop("configOmitted", boolean("Must not be true: a view that left this node's settings out cannot be sent back as the node.")),
	).require("id", "type")
	conn := object("A connection from an output port to an input port.",
		prop("id", pattern("The connection's id, unique in the flow (letters, digits, '_', '-', '.' or ':'; at most 256).",
			connectionIDPattern, 256)),
		prop("sourceNode", nodeIDArg("The source node's id.")),
		prop("sourcePort", portIDArg("The source node's output port.")),
		prop("targetNode", nodeIDArg("The target node's id.")),
		prop("targetPort", portIDArg("The target node's input port.")),
	).require("id", "sourceNode", "sourcePort", "targetNode", "targetPort")
	return object("A whole flow: its nodes and connections. "+ignored,
		prop("name", text(fmt.Sprintf("The flow's name (at most %d characters; a draft's at most %d).", maxFlowNameChars,
			service.MaxDraftNameChars), 0, maxFlowNameChars)),
		prop("description", text("", 0, 4096)),
		prop("nodes", array(fmt.Sprintf("The flow's nodes (at most %d).", maxNodes), node, maxNodes)),
		prop("connections", array(fmt.Sprintf("The flow's connections (at most %d).", maxConnections), conn, maxConnections)),
		prop("variables", freeObject("Legacy flow variables (agents may not change them).")),
		prop("id", idArg("")),
		prop("state", text("", 0, 32)),
		prop("version", integer("", 0, maxInt32)),
		prop("createdAt", text("", 0, 64)),
		prop("updatedAt", text("", 0, 64)),
		prop("tools", array("", anyValue(""), 100)),
		prop("totalNodes", integer("", 0, maxInt32)),
		prop("totalConnections", integer("", 0, maxInt32)),
		prop("truncated", boolean("Must not be true: a view that left nodes, connections or settings out cannot be sent back as the flow.")),
	).require("nodes")
}

// opsArg is the schema of edit operations (flow.Op); opIssues checks each
// op's own fields.
func opsArg() *argSchema {
	opNode := object("The node to add.",
		prop("id", pattern("The new node's id (1 to 64 letters, digits, '-' or '_'; generated when absent).", flow.OpNodeIDPattern, 64)),
		prop("type", typeArg("The node type, from pw_list_node_types.")),
		prop("config", freeObject("The node's settings.")),
		prop("enabled", boolean("Whether the node runs (default true).")),
		prop("position", positionArg()),
	).require("type")
	op := object("One edit: add_node{node}, update_node{nodeId, config?, enabled?, position?} (config is a JSON merge patch: "+
		"null deletes a setting), remove_node{nodeId} (with its connections), connect{sourceNode, sourcePort, targetNode, "+
		"targetPort}, disconnect{connectionId} or disconnect with the four endpoint fields.",
		prop("op", enum("The operation.", flow.OpAddNode, flow.OpUpdateNode, flow.OpRemoveNode, flow.OpConnect, flow.OpDisconnect)),
		prop("node", opNode),
		prop("nodeId", idArg("The node to update or remove.")),
		prop("config", freeObject("update_node: a JSON merge patch of the node's settings (null deletes one).")),
		prop("enabled", boolean("update_node: whether the node runs.")),
		prop("position", positionArg()),
		prop("sourceNode", idArg("connect, disconnect: the source node.")),
		prop("sourcePort", idArg("connect, disconnect: the source node's output port.")),
		prop("targetNode", idArg("connect, disconnect: the target node.")),
		prop("targetPort", idArg("connect, disconnect: the target node's input port.")),
		prop("connectionId", idArg("disconnect: the connection to remove.")),
	).require("op")
	return array(fmt.Sprintf("Edits applied in order to the flow's current version (at most %d).", flow.MaxOps), op, flow.MaxOps)
}

// opFields are the fields each op takes besides "op" (disconnect: either
// form, checked apart).
var opFields = map[string]struct{ required, optional []string }{
	flow.OpAddNode:    {required: []string{"node"}},
	flow.OpUpdateNode: {required: []string{"nodeId"}, optional: []string{"config", "enabled", "position"}},
	flow.OpRemoveNode: {required: []string{"nodeId"}},
	flow.OpConnect:    {required: []string{"sourceNode", "sourcePort", "targetNode", "targetPort"}},
	flow.OpDisconnect: {optional: []string{"connectionId", "sourceNode", "sourcePort", "targetNode", "targetPort"}},
}

var endpointFields = []string{"sourceNode", "sourcePort", "targetNode", "targetPort"}

// opIssues checks the fields of ops that passed the schema: each op takes
// its own (a field another op takes is refused, not ignored).
func opIssues(ops []any) []toolschema.Issue {
	var issues []toolschema.Issue
	for i, raw := range ops {
		op, _ := raw.(map[string]any)
		kind, _ := op["op"].(string)
		fields, ok := opFields[kind]
		if !ok {
			continue
		}
		at := fmt.Sprintf("/ops/%d", i)
		allowed := map[string]bool{"op": true}
		for _, name := range append(append([]string{}, fields.required...), fields.optional...) {
			allowed[name] = true
		}
		for _, name := range sortedKeys(op) {
			if !allowed[name] {
				issues = append(issues, toolschema.Issue{Pointer: at + "/" + escapePointer(name), Message: "not used by " + kind})
			}
		}
		for _, name := range fields.required {
			if _, ok := op[name]; !ok {
				issues = append(issues, toolschema.Issue{Pointer: at + "/" + name, Message: "missing required property"})
			}
		}
		// Settings an op sets are what a view showed too: nothing it cut or
		// partly hid ("<redacted>" as a whole setting keeps the stored
		// value, as in a whole flow).
		cfg, cfgAt := objectArg(op, "config"), at+"/config"
		if kind == flow.OpAddNode {
			cfg, cfgAt = objectArg(objectArg(op, "node"), "config"), at+"/node/config"
		}
		for _, key := range sortedKeys(cfg) {
			issues = viewValueIssues(issues, cfg[key], cfgAt+"/"+escapePointer(shorten(key, 64)), true, flowForDraft)
		}
		switch kind {
		case flow.OpUpdateNode:
			if op["config"] == nil && op["enabled"] == nil && op["position"] == nil {
				issues = append(issues, toolschema.Issue{Pointer: at, Message: "update_node changes nothing: give config, enabled or position"})
			}
		case flow.OpDisconnect:
			_, byID := op["connectionId"]
			endpoints := 0
			for _, name := range endpointFields {
				if _, ok := op[name]; ok {
					endpoints++
				}
			}
			switch {
			case byID && endpoints > 0:
				issues = append(issues, toolschema.Issue{Pointer: at, Message: "disconnect takes connectionId or the four endpoint fields, not both"})
			case !byID && endpoints < len(endpointFields):
				issues = append(issues, toolschema.Issue{Pointer: at, Message: "disconnect needs connectionId, or sourceNode, sourcePort, targetNode and targetPort"})
			}
		}
	}
	return issues
}

// flowIssues checks what the schema cannot say of a sent flow: a view that
// left something out, cut a value or hid part of one is not the flow; an
// inline flow has no stored value for "<redacted>" to keep.
func flowIssues(f map[string]any, at string, use flowUse) []toolschema.Issue {
	var issues []toolschema.Issue
	if f["truncated"] == true {
		issues = append(issues, toolschema.Issue{Pointer: at + "/truncated",
			Message: "the view left nodes, connections or settings out: send the whole flow, or change it with ops"})
	}
	for _, name := range []string{"name", "description"} {
		if s, _ := f[name].(string); strings.Contains(s, service.RedactedValue) {
			issues = append(issues, toolschema.Issue{Pointer: at + "/" + name, Message: partlyHidden})
		}
	}
	nodes, _ := f["nodes"].([]any)
	for i, raw := range nodes {
		n, _ := raw.(map[string]any)
		ptr := fmt.Sprintf("%s/nodes/%d", at, i)
		if n["configOmitted"] == true {
			issues = append(issues, toolschema.Issue{Pointer: ptr + "/configOmitted",
				Message: "the view left this node's settings out: send them, or change the node with an update_node op"})
		}
		cfg, _ := n["config"].(map[string]any)
		for _, key := range sortedKeys(cfg) {
			issues = viewValueIssues(issues, cfg[key], ptr+"/config/"+escapePointer(shorten(key, 64)), true, use)
		}
	}
	return issues
}

const (
	partlyHidden = `a view hid part of this ("<redacted>" inside it): send the whole value, or "<redacted>" as the ` +
		`whole setting to keep the stored one`
	viewCut = `a view cut this value: send it whole, or change the flow with ops, which keep the settings they do not touch`
)

// viewValueIssues appends the problems of a setting's value v at ptr (top:
// the setting itself, not a part of it): a value a view cut, or hid in
// part, and in an inline flow "<redacted>", which has nothing to keep.
func viewValueIssues(issues []toolschema.Issue, v any, ptr string, top bool, use flowUse) []toolschema.Issue {
	switch x := v.(type) {
	case string:
		switch {
		case top && x == service.RedactedValue:
			if use == flowInline {
				issues = append(issues, toolschema.Issue{Pointer: ptr, Message: `a flow sent inline has no stored value for ` +
					`"<redacted>" to keep: give the value, or dry-run the stored flow by flowId or your draft by draftId`})
			}
		case x == viewTruncatedValue || strings.HasSuffix(x, "…"+viewTruncatedValue):
			issues = append(issues, toolschema.Issue{Pointer: ptr, Message: viewCut})
		case strings.Contains(x, service.RedactedValue):
			issues = append(issues, toolschema.Issue{Pointer: ptr, Message: partlyHidden})
		}
	case map[string]any:
		for _, k := range sortedKeys(x) {
			at := ptr + "/" + escapePointer(shorten(k, 64))
			if strings.Contains(k, service.RedactedValue) || strings.Contains(k, viewTruncatedValue) {
				issues = append(issues, toolschema.Issue{Pointer: at, Message: partlyHidden})
				continue
			}
			issues = viewValueIssues(issues, x[k], at, false, use)
		}
	case []any:
		for i, e := range x {
			issues = viewValueIssues(issues, e, ptr+"/"+strconv.Itoa(i), false, use)
		}
	}
	return issues
}

// exactlyOne requires exactly one of names in args (an issue at the root).
func exactlyOne(args map[string]any, names ...string) []toolschema.Issue {
	n := 0
	for _, name := range names {
		if _, ok := args[name]; ok {
			n++
		}
	}
	if n == 1 {
		return nil
	}
	return []toolschema.Issue{{Pointer: "", Message: "give exactly one of " + strings.Join(names, ", ")}}
}

// requiredWith requires name whenever other is given.
func requiredWith(args map[string]any, name, other string) []toolschema.Issue {
	_, has := args[name]
	if _, given := args[other]; given && !has {
		return []toolschema.Issue{{Pointer: "/" + name, Message: "required with " + other}}
	}
	return nil
}

// checked validates args against s and then, when they conform, runs the
// tool's own checks; either way at most maxIssues problems are listed, and
// a last one says more were left out.
func checked(s *argSchema, more func(args map[string]any) []toolschema.Issue) func(map[string]any) []toolschema.Issue {
	return func(args map[string]any) []toolschema.Issue {
		if issues := s.check(args); len(issues) > 0 {
			return issues
		}
		if more == nil {
			return nil
		}
		return capIssues(more(args))
	}
}

// capIssues keeps the first maxIssues problems, as argChecker does.
func capIssues(issues []toolschema.Issue) []toolschema.Issue {
	if len(issues) <= maxIssues {
		return issues
	}
	return append(issues[:maxIssues:maxIssues], toolschema.Issue{Message: fmt.Sprintf("too many problems: only the first %d are listed", maxIssues)})
}

// flowChecks are the checks of a request carrying flow and ops.
func flowChecks(args map[string]any, use flowUse) []toolschema.Issue {
	var issues []toolschema.Issue
	if f, ok := args["flow"].(map[string]any); ok {
		issues = append(issues, flowIssues(f, "/flow", use)...)
	}
	if ops, ok := args["ops"].([]any); ok {
		issues = append(issues, opIssues(ops)...)
	}
	return issues
}

// baseVersionIssues checks pw_save_draft's base: with flowId, baseVersion
// or the sent flow's version (as the read views show it), equal when both
// are given.
func baseVersionIssues(args map[string]any) []toolschema.Issue {
	if _, ok := args["flowId"]; !ok {
		return nil
	}
	base, hasBase := args["baseVersion"].(float64)
	read, _ := objectArg(args, "flow")["version"].(float64)
	switch {
	case !hasBase && read < 1:
		return []toolschema.Issue{{Pointer: "/baseVersion", Message: "required with flowId: the version pw_get_flow shows " +
			"(or send the flow with its version)"}}
	case hasBase && read >= 1 && read != base:
		return []toolschema.Issue{{Pointer: "/flow/version", Message: "must equal baseVersion: the flow was read at another version"}}
	}
	return nil
}

// draftBaseVersion is pw_save_draft's checked base: baseVersion, or the
// sent flow's version when flowId names a flow.
func draftBaseVersion(args map[string]any) int {
	if base, ok := args["baseVersion"].(float64); ok {
		return int(base)
	}
	if _, ok := args["flowId"]; !ok {
		return 0
	}
	read, _ := objectArg(args, "flow")["version"].(float64)
	return int(read)
}

// Fields of a sent flow and its nodes that are only informational: dropped
// before the service sees the flow.
var (
	ignoredFlowFields = []string{"id", "state", "version", "createdAt", "updatedAt", "tools", "totalNodes", "totalConnections", "truncated"}
	ignoredNodeFields = []string{"name", "risk", "configOmitted"}
)

// decodeFlow converts a validated flow argument to the flow the service
// gets: its definition, without the informational fields (no id, state,
// version or times), enabled true unless false.
func decodeFlow(v any) (*flow.Flow, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, nil
	}
	def := make(map[string]any, len(m))
	for k, e := range m {
		def[k] = e
	}
	for _, k := range ignoredFlowFields {
		delete(def, k)
	}
	if nodes, ok := m["nodes"].([]any); ok {
		kept := make([]any, len(nodes))
		for i, raw := range nodes {
			n, _ := raw.(map[string]any)
			c := make(map[string]any, len(n))
			for k, e := range n {
				c[k] = e
			}
			for _, k := range ignoredNodeFields {
				delete(c, k)
			}
			kept[i] = c
		}
		def["nodes"] = kept
	}
	var f flow.Flow
	if err := remarshal(def, &f); err != nil {
		return nil, fmt.Errorf("decode the flow: %w", err)
	}
	return &f, nil
}

// decodeOps converts validated ops to flow.Op.
func decodeOps(v any) ([]flow.Op, error) {
	if v == nil {
		return nil, nil
	}
	var ops []flow.Op
	if err := remarshal(v, &ops); err != nil {
		return nil, fmt.Errorf("decode the ops: %w", err)
	}
	return ops, nil
}

// remarshal decodes the JSON of v into out. v was decoded and validated
// strictly (case-sensitive names, no duplicates, closed objects), so the
// lenient decoder reads exactly what was checked.
func remarshal(v, out any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func objectArg(args map[string]any, name string) map[string]any {
	m, _ := args[name].(map[string]any)
	return m
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
