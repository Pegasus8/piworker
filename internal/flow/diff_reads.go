package flow

import "fmt"

// AlertDataReadChanged: a running node that was added, enabled, retyped or
// reconfigured, or that gained an input, reads what agents may not read:
// variables (global or flow; $env holds them), message metadata (where the
// nodes before it record their settings: an HTTP URL, an expression, a
// topic), or what a node that uses a credential or reads variables
// produced (it runs downstream of one, through connections or flow
// variables). Runs of a flow are visible to agents (pw_list_runs,
// pw_get_run): such a node could hand that data back through an error it
// raises on purpose, or through which nodes run. One alert per node.
const AlertDataReadChanged = "data_read_changed"

// dataReadAlerts adds AlertDataReadChanged for the running nodes of g (the
// proposed flow) that differ from base (g.baseByKey holds the base node of
// the same type) and read protected data.
func dataReadAlerts(g *varGraph, nextOrder []*diffNode, add func(order int, a Alert)) {
	// fedBy maps a running node to the first source upstream of it: a node
	// that uses a credential (its response) or reads variables.
	fedBy := map[*diffNode]*diffNode{}
	for _, s := range nextOrder {
		if s.live() == nil || g.running[s.n.ID] != s || !(s.credentialed() || readsVariables(s.n)) {
			continue
		}
		for _, u := range g.downstream(s) {
			if fedBy[u] == nil {
				fedBy[u] = s
			}
		}
	}
	for _, d := range nextOrder {
		if d.live() == nil || g.running[d.n.ID] != d {
			continue
		}
		change := nodeChange(d, g.baseByKey[d.key])
		if change == "" && g.newInput[d.n.ID] {
			change = "gained an input"
		}
		if change == "" {
			continue
		}
		var msg string
		switch src := fedBy[d]; {
		case readsVariables(d.n) || readsMetadata(d.n):
			msg = fmt.Sprintf("node %s (%s) %s and reads variables or message metadata (the settings of the nodes before it)",
				d.n.ID, d.n.Type, change)
		case src != nil:
			msg = fmt.Sprintf("node %s (%s) %s and receives what node %s (%s), which uses a credential or reads variables, produced",
				d.n.ID, d.n.Type, change, src.n.ID, src.n.Type)
		default:
			continue
		}
		add(d.order, Alert{Kind: AlertDataReadChanged, NodeID: d.n.ID,
			Message: msg + ": what it reads could come back to an agent through the flow's runs"})
	}
}

// readsVariables reports whether a node may read variables of any scope.
func readsVariables(n *Node) bool {
	return readsGlobalVariables(n) || readsFlowVariables(n)
}

// readsMetadata reports whether a node's config text names the message
// metadata (meta, or $env, which holds it). It errs on the side of yes.
func readsMetadata(n *Node) bool {
	return configTextContains(n.Config, "meta", "$env")
}
