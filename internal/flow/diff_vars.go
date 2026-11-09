package flow

import "fmt"

// AlertGlobalVariableWrite: a running set-var writes a global variable (its
// scope is not "flow"; an absent scope means global), which other flows read
// for their URLs, commands and conditions, and what it writes may have
// changed: the set-var was added or enabled, or its scope, key or value
// changed; or something upstream of it changed: a running node that feeds
// it (through connections, or through the flow variables a node feeding it
// reads) was added, enabled or reconfigured, or a connection into that part
// was added.
const AlertGlobalVariableWrite = "global_variable_write"

// globalVariableAlerts adds an AlertGlobalVariableWrite for each running
// set-var of next that writes a global variable when it, or the part of next
// that feeds it, differs from base (by node key: the same node, type,
// enabled state and config). A set-var's value is an expression over the
// message it receives (and the variables it reads), so a change anywhere
// upstream can change what it writes. Only running nodes and the
// connections between them carry messages.
func globalVariableAlerts(next *Flow, baseByKey map[string]*diffNode, nextOrder []*diffNode,
	added []Connection, add func(order int, a Alert)) {
	running := map[string]*diffNode{} // the first running node of each ID
	var writers, flowWriters []*diffNode
	for _, d := range nextOrder {
		if d.live() == nil {
			continue
		}
		if _, ok := running[d.n.ID]; !ok {
			running[d.n.ID] = d
		}
		if d.n.Type == "set-var" {
			if configString(d.n.Config, "scope") == "flow" {
				flowWriters = append(flowWriters, d)
			} else {
				writers = append(writers, d)
			}
		}
	}
	if len(writers) == 0 {
		return
	}
	feeds := map[string][]*diffNode{} // target ID → the running nodes connected into it
	for _, c := range next.Connections {
		if src, dst := running[c.SourceNode], running[c.TargetNode]; src != nil && dst != nil {
			feeds[c.TargetNode] = append(feeds[c.TargetNode], src)
		}
	}
	newInput := map[string]bool{}
	for _, c := range added {
		if running[c.SourceNode] != nil && running[c.TargetNode] != nil {
			newInput[c.TargetNode] = true
		}
	}

	for _, w := range writers {
		why := ""
		if change := nodeChange(w, baseByKey[w.key]); change != "" {
			why = fmt.Sprintf("the node %s", change)
		} else {
			why = upstreamChange(w, feeds, flowWriters, newInput, baseByKey)
		}
		if why == "" {
			continue
		}
		a := Alert{Kind: AlertGlobalVariableWrite, NodeID: w.n.ID,
			Message: fmt.Sprintf("node %s (%s) writes a global variable, which other flows read (for their URLs, commands and conditions), and what it writes may change: %s",
				w.n.ID, w.n.Type, why)}
		if key := w.n.Config["key"]; agentReadable("key", w) && !isUnset(key) {
			a.After = cloneValue(key)
		}
		add(w.order, a)
	}
}

// upstreamChange walks the running part of the flow that feeds the global
// writer w (connections into it, and the flow-variable writers when a node
// of that part reads flow variables) and describes the first difference
// from base it finds ("" when none).
func upstreamChange(w *diffNode, feeds map[string][]*diffNode, flowWriters []*diffNode,
	newInput map[string]bool, baseByKey map[string]*diffNode) string {
	seen := map[*diffNode]bool{w: true}
	queue := []*diffNode{w}
	flowVarsWalked := false
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if newInput[d.n.ID] {
			return fmt.Sprintf("a connection into node %s (%s), which feeds it, was added", d.n.ID, d.n.Type)
		}
		upstream := feeds[d.n.ID]
		if !flowVarsWalked && readsFlowVariables(d.n) {
			// Any flow variable writer of the flow may feed what d reads.
			flowVarsWalked = true
			upstream = append(append([]*diffNode{}, upstream...), flowWriters...)
		}
		for _, u := range upstream {
			if seen[u] {
				continue
			}
			seen[u] = true
			if change := nodeChange(u, baseByKey[u.key]); change != "" {
				return fmt.Sprintf("node %s (%s), which feeds it, %s", u.n.ID, u.n.Type, change)
			}
			queue = append(queue, u)
		}
	}
	return ""
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
