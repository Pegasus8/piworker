package flow

import "fmt"

// Global variables are shared state: other flows read them for their URLs,
// commands and conditions, and what a tool's path reads from them goes back
// to the agent that calls it. The alerts below flag changes to what flows
// into a global variable or out of one through a tool.

// AlertGlobalVariableWrite: a running set-var writes a global variable (its
// scope is not "flow"; an absent scope means global), which other flows read
// for their URLs, commands and conditions, and what it writes may have
// changed: the set-var was added or enabled, or its scope, key or value
// changed; or something upstream of it changed: a running node that feeds
// it (through connections, or through the flow variables a node feeding it
// reads) was added, enabled or reconfigured, or a connection into that part
// was added.
const AlertGlobalVariableWrite = "global_variable_write"

// AlertGlobalVariableRead: what a running tool's path (the running part of
// the flow its trigger reaches through connections) reads from global
// variables goes back to the agent that calls it, and that path changed
// and reads global variables: a node on it was added, enabled or
// reconfigured, or a connection into it was added.
const AlertGlobalVariableRead = "global_variable_read"

// varGraph is the running part of a proposed flow, as the global variable
// alerts walk it.
type varGraph struct {
	baseByKey map[string]*diffNode
	running   map[string]*diffNode   // the first running node of each ID
	feeds     map[string][]*diffNode // target ID → the running nodes connected into it
	fedBy     map[string][]*diffNode // source ID → the running nodes it connects to
	newInput  map[string]bool        // target IDs of added connections between running nodes
	// flowWriters are the running set-vars of scope flow.
	flowWriters []*diffNode
}

func newVarGraph(next *Flow, baseByKey map[string]*diffNode, nextOrder []*diffNode, added []Connection) *varGraph {
	g := &varGraph{baseByKey: baseByKey, running: map[string]*diffNode{}, feeds: map[string][]*diffNode{},
		fedBy: map[string][]*diffNode{}, newInput: map[string]bool{}}
	for _, d := range nextOrder {
		if d.live() == nil {
			continue
		}
		if _, ok := g.running[d.n.ID]; !ok {
			g.running[d.n.ID] = d
		}
		if d.n.Type == "set-var" && configString(d.n.Config, "scope") == "flow" {
			g.flowWriters = append(g.flowWriters, d)
		}
	}
	for _, c := range next.Connections {
		if src, dst := g.running[c.SourceNode], g.running[c.TargetNode]; src != nil && dst != nil {
			g.feeds[c.TargetNode] = append(g.feeds[c.TargetNode], src)
			g.fedBy[c.SourceNode] = append(g.fedBy[c.SourceNode], dst)
		}
	}
	for _, c := range added {
		if g.running[c.SourceNode] != nil && g.running[c.TargetNode] != nil {
			g.newInput[c.TargetNode] = true
		}
	}
	return g
}

// globalVariableAlerts adds the alerts of global variable writes and of
// global variable reads through tools.
func globalVariableAlerts(next *Flow, baseByKey map[string]*diffNode, nextOrder []*diffNode,
	added []Connection, add func(order int, a Alert)) {
	g := newVarGraph(next, baseByKey, nextOrder, added)
	for _, d := range nextOrder {
		if d.live() == nil {
			continue
		}
		switch {
		case d.n.Type == "set-var" && configString(d.n.Config, "scope") != "flow":
			g.writeAlert(d, add)
		case d.n.Type == "trigger-tool":
			g.readAlert(d, add)
		}
	}
}

// writeAlert raises AlertGlobalVariableWrite for the global writer w when
// it, or the running part of the flow that feeds it, differs from base (by
// node key: the same node, type, enabled state and config). A set-var's
// value is an expression over the message it receives (and the variables
// it reads), so a change anywhere upstream can change what it writes.
func (g *varGraph) writeAlert(w *diffNode, add func(order int, a Alert)) {
	why := ""
	if change := nodeChange(w, g.baseByKey[w.key]); change != "" {
		why = fmt.Sprintf("the node %s", change)
	} else {
		why = g.upstreamChange(w)
	}
	if why == "" {
		return
	}
	a := Alert{Kind: AlertGlobalVariableWrite, NodeID: w.n.ID,
		Message: fmt.Sprintf("node %s (%s) writes a global variable, which other flows read (for their URLs, commands and conditions), and what it writes may change: %s",
			w.n.ID, w.n.Type, why)}
	if key := w.n.Config["key"]; agentReadable("key", w) && !isUnset(key) {
		a.After = cloneValue(key)
	}
	add(w.order, a)
}

// upstreamChange walks the running part of the flow that feeds the global
// writer w (connections into it, and the flow-variable writers when a node
// of that part reads flow variables) and describes the first difference
// from base it finds ("" when none).
func (g *varGraph) upstreamChange(w *diffNode) string {
	seen := map[*diffNode]bool{w: true}
	queue := []*diffNode{w}
	flowVarsWalked := false
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if g.newInput[d.n.ID] {
			return fmt.Sprintf("a connection into node %s (%s), which feeds it, was added", d.n.ID, d.n.Type)
		}
		upstream := g.feeds[d.n.ID]
		if !flowVarsWalked && readsFlowVariables(d.n) {
			// Any flow variable writer of the flow may feed what d reads.
			flowVarsWalked = true
			upstream = append(append([]*diffNode{}, upstream...), g.flowWriters...)
		}
		for _, u := range upstream {
			if seen[u] {
				continue
			}
			seen[u] = true
			if change := nodeChange(u, g.baseByKey[u.key]); change != "" {
				return fmt.Sprintf("node %s (%s), which feeds it, %s", u.n.ID, u.n.Type, change)
			}
			queue = append(queue, u)
		}
	}
	return ""
}

// readAlert raises AlertGlobalVariableRead for the tool trigger tool when
// the running part of the flow it reaches reads global variables and
// differs from base (a node added, enabled or reconfigured, or a connection
// into it added).
func (g *varGraph) readAlert(tool *diffNode, add func(order int, a Alert)) {
	seen := map[*diffNode]bool{tool: true}
	queue := []*diffNode{tool}
	reads, changed := false, ""
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		for _, u := range g.fedBy[d.n.ID] {
			if seen[u] {
				continue
			}
			seen[u] = true
			reads = reads || readsGlobalVariables(u.n)
			if changed == "" {
				if change := nodeChange(u, g.baseByKey[u.key]); change != "" {
					changed = fmt.Sprintf("node %s (%s) on it %s", u.n.ID, u.n.Type, change)
				} else if g.newInput[u.n.ID] {
					changed = fmt.Sprintf("a connection into node %s (%s) on it was added", u.n.ID, u.n.Type)
				}
			}
			queue = append(queue, u)
		}
	}
	if !reads || changed == "" {
		return
	}
	add(tool.order, Alert{Kind: AlertGlobalVariableRead, NodeID: tool.n.ID,
		Message: fmt.Sprintf("the path of tool node %s reads global variables, which go back to the agent that calls it, and it changed: %s",
			tool.n.ID, changed)})
}

// readsGlobalVariables reports whether a node may read global variables: a
// get-var whose scope is not flow, or any config text naming vars
// (expressions and templates; flowVars and $env included). It errs on the
// side of yes.
func readsGlobalVariables(n *Node) bool {
	if n.Type == "get-var" && configString(n.Config, "scope") != "flow" {
		return true
	}
	return configTextContains(n.Config, "vars", "$env")
}

// nodeChange describes how running node d differs from its base node b (nil:
// none): "is new", "was enabled", "changed" ("" when it is the same).
func nodeChange(d, b *diffNode) string {
	switch {
	case b == nil:
		return "is new"
	case !b.n.Enabled:
		return "was enabled"
	case b.n.Type != d.n.Type:
		return "changed"
	}
	for _, key := range configKeys(b, d) {
		if !sameValue(b.n.Config[key], d.n.Config[key]) {
			return "changed"
		}
	}
	return ""
}
