package mcpserver

import (
	"encoding/json"
	"fmt"
	"slices"
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
// control characters).
func idArg(description string) *argSchema { return pattern(description, idPattern, maxIDLength) }

// opNodeIDPattern is the syntax of a node id add_node may choose (flow.ApplyOps).
const opNodeIDPattern = `^[A-Za-z0-9_-]{1,64}$`

func positionArg() *argSchema {
	return object("The node's position on the editor's canvas (finite, within ±1,000,000); laid out automatically when absent.",
		prop("x", number("Horizontal position.")), prop("y", number("Vertical position."))).require("x", "y")
}

func portArg() *argSchema {
	return object("",
		prop("id", text("", 1, 256)),
		prop("name", text("", 0, 256)),
		prop("dataType", text("", 0, 32)),
		prop("multiple", boolean("")),
		prop("required", boolean("")),
	).require("id")
}

// maxInt32 bounds the integers of a sent flow.
const maxInt32 = 1<<31 - 1

// flowArg is the schema of a flow an agent sends: the definition (id and
// timestamps are the server's), plus the informational fields of the read
// views (pw_get_flow, pw_get_draft) and of an exported flow, which are
// accepted so that a flow read can be sent back, and dropped.
func flowArg(maxNodes, maxConnections int) *argSchema {
	node := object("A node. name and risk (as the read views show them) are ignored; inputs and outputs default to the type's ports.",
		prop("id", idArg("The node's id, unique in the flow.")),
		prop("type", idArg("The node type, from pw_list_node_types (e.g. trigger-interval, action-http).")),
		prop("enabled", boolean("Whether the node runs (default true).")),
		prop("position", positionArg()),
		prop("config", freeObject(`The node's settings, as its type's config schema describes them. "<redacted>" (as the read views show `+
			`hidden settings) keeps the current value of that setting of the same node; a sensitive setting takes only a `+
			`{{secret.NAME}} reference.`)),
		prop("category", enum("", "input", "processing", "output")),
		prop("inputs", array("", portArg(), maxPortsPerNode)),
		prop("outputs", array("", portArg(), maxPortsPerNode)),
		prop("name", text("", 0, 256)),
		prop("risk", text("", 0, 32)),
		prop("configOmitted", boolean("Must not be true: a view that left this node's settings out cannot be sent back as the node.")),
	).require("id", "type")
	conn := object("A connection from an output port to an input port.",
		prop("id", idArg("The connection's id, unique in the flow.")),
		prop("sourceNode", idArg("The source node's id.")),
		prop("sourcePort", idArg("The source node's output port.")),
		prop("targetNode", idArg("The target node's id.")),
		prop("targetPort", idArg("The target node's input port.")),
	).require("id", "sourceNode", "sourcePort", "targetNode", "targetPort")
	return object("A whole flow: its nodes and connections. id, state, version, createdAt, updatedAt, tools, totalNodes and "+
		"totalConnections (as the read views and exported flows show them) are ignored: the flow is named by flowId, or is new, "+
		"and its base version is baseVersion.",
		prop("name", text(fmt.Sprintf("The flow's name (at most %d characters).", service.MaxDraftNameChars), 0, service.MaxDraftNameChars)),
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
		prop("id", pattern("The new node's id (1 to 64 letters, digits, '-' or '_'; generated when absent).", opNodeIDPattern, 64)),
		prop("type", idArg("The node type, from pw_list_node_types.")),
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
// left something out is not the flow.
func flowIssues(f map[string]any, at string) []toolschema.Issue {
	var issues []toolschema.Issue
	if f["truncated"] == true {
		issues = append(issues, toolschema.Issue{Pointer: at + "/truncated",
			Message: "the view left nodes, connections or settings out: send the whole flow, or change it with ops"})
	}
	nodes, _ := f["nodes"].([]any)
	for i, raw := range nodes {
		if n, _ := raw.(map[string]any); n["configOmitted"] == true {
			issues = append(issues, toolschema.Issue{Pointer: fmt.Sprintf("%s/nodes/%d/configOmitted", at, i),
				Message: "the view left this node's settings out: send them, or change the node with an update_node op"})
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
// tool's own checks.
func checked(s *argSchema, more func(args map[string]any) []toolschema.Issue) func(map[string]any) []toolschema.Issue {
	return func(args map[string]any) []toolschema.Issue {
		if issues := s.check(args); len(issues) > 0 {
			return issues
		}
		if more == nil {
			return nil
		}
		return more(args)
	}
}

// flowChecks are the checks of a request carrying flow and ops.
func flowChecks(args map[string]any) []toolschema.Issue {
	var issues []toolschema.Issue
	if f, ok := args["flow"].(map[string]any); ok {
		issues = append(issues, flowIssues(f, "/flow")...)
	}
	if ops, ok := args["ops"].([]any); ok {
		issues = append(issues, opIssues(ops)...)
	}
	return issues
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
