package flow

import (
	"fmt"
	"sort"
)

// AlertToolPathChanged: what a running tool runs changed. A tool's path is
// the running part of the flow its trigger reaches (through connections and
// flow variables) together with everything that feeds that part; it changed
// when a node of it was added, enabled, retyped or reconfigured, a node left
// it, or a connection into it was added or removed, compared with the
// base's path of the same trigger (a trigger new or enabled here has no
// base path). A published tool is the admin's: wiring a confirm:none tool to
// the action behind a confirm:admin tool would run that action without the
// admin's approval, while the tool's name, grant and fingerprint stay the
// same. One alert per tool.
const AlertToolPathChanged = "tool_path_changed"

// toolPathAlerts adds AlertToolPathChanged for every running trigger-tool of
// g (the proposed flow) whose path differs from its path in base (gb, the
// running part of the base flow; paired maps a node key to its base node of
// the same type).
func toolPathAlerts(g, gb *varGraph, nextOrder []*diffNode, paired map[string]*diffNode, add func(order int, a Alert)) {
	for _, d := range nextOrder {
		if d.live() == nil || d.n.Type != "trigger-tool" || g.running[d.n.ID] != d {
			continue
		}
		why := ""
		b := paired[d.key].live()
		if b == nil || gb.running[b.n.ID] != b {
			why = "the tool is new or was enabled"
		} else {
			why = toolPathChange(g.toolPath(d), gb.toolPath(b), paired)
		}
		if why == "" {
			continue
		}
		add(d.order, Alert{Kind: AlertToolPathChanged, NodeID: d.n.ID,
			Message: fmt.Sprintf("what tool node %s runs changed (a published tool is the admin's: its grant and approvals stay bound to it): %s", d.n.ID, why)})
	}
}

// toolPath is the path of the running tool trigger t: t, the running nodes
// it reaches and every running node that feeds them, with the connections
// into them.
type toolPath struct {
	nodes []*diffNode // walk order, t first
	conns [][4]string // source node, port, target node, port; sorted
}

func (g *varGraph) toolPath(t *diffNode) toolPath {
	seen := map[*diffNode]bool{t: true}
	p := toolPath{nodes: []*diffNode{t}}
	for _, u := range g.closure(g.downstream(t)) {
		if !seen[u] {
			seen[u] = true
			p.nodes = append(p.nodes, u)
		}
	}
	in := map[string]bool{}
	for _, u := range p.nodes {
		in[u.n.ID] = true
	}
	for _, c := range g.conns {
		if in[c.TargetNode] {
			p.conns = append(p.conns, [4]string{c.SourceNode, c.SourcePort, c.TargetNode, c.TargetPort})
		}
	}
	sort.Slice(p.conns, func(i, j int) bool {
		a, b := p.conns[i], p.conns[j]
		for k := range a {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return false
	})
	return p
}

// toolPathChange describes the first difference between a tool's path in
// the proposed flow (next) and in the base (base) ("" when none).
func toolPathChange(next, base toolPath, paired map[string]*diffNode) string {
	inBase := map[*diffNode]bool{}
	for _, b := range base.nodes {
		inBase[b] = true
	}
	kept := map[*diffNode]bool{}
	for _, d := range next.nodes {
		b := paired[d.key]
		if change := nodeChange(d, b.live()); change != "" {
			if b != nil && !b.n.Enabled {
				change = "was enabled"
			}
			return fmt.Sprintf("node %s (%s), on it or feeding it, %s", d.n.ID, d.n.Type, change)
		}
		if !inBase[b] {
			return fmt.Sprintf("node %s (%s) is now on it or feeding it", d.n.ID, d.n.Type)
		}
		kept[b] = true
	}
	for _, b := range base.nodes {
		if !kept[b] {
			return fmt.Sprintf("node %s (%s) is no longer on it or feeding it", b.n.ID, b.n.Type)
		}
	}
	baseConns := map[[4]string]bool{}
	for _, c := range base.conns {
		baseConns[c] = true
	}
	nextConns := map[[4]string]bool{}
	for _, c := range next.conns {
		nextConns[c] = true
		if !baseConns[c] {
			return fmt.Sprintf("a connection from node %s into node %s was added", c[0], c[2])
		}
	}
	for _, c := range base.conns {
		if !nextConns[c] {
			return fmt.Sprintf("a connection from node %s into node %s was removed", c[0], c[2])
		}
	}
	return ""
}
