package flow_test

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readsFlow is an approved flow: a trigger feeds a log and a credentialed
// request whose response is logged, and an admin transform reads a global
// variable into a second log.
func readsFlow(change func(f *flow.Flow)) *flow.Flow {
	f := diffFlow(
		trigger(),
		lintNode("log", "action-log", map[string]any{"message": "tick"}),
		lintNode("call", "action-http", map[string]any{"url": "https://api.example.com/balance",
			"headers": map[string]any{"Authorization": "{{secret.api_key}}"}}),
		lintNode("rlog", "action-log", map[string]any{"message": "done"}),
		lintNode("setpoint", "process-transform", map[string]any{"expression": `vars["setpoint"]`}),
		lintNode("slog", "action-log", map[string]any{"message": "set"}),
	)
	f.Connections = []flow.Connection{
		lintConn("c1", "t", "output", "log", "input"),
		lintConn("c2", "t", "output", "call", "input"),
		lintConn("c3", "call", "output", "rlog", "input"),
		lintConn("c4", "t", "output", "setpoint", "input"),
		lintConn("c5", "setpoint", "output", "slog", "input"),
	}
	if change != nil {
		change(f)
	}
	return f
}

// peek adds a transform fed by from.
func peek(f *flow.Flow, from, expression string) {
	n := lintNode("peek", "process-transform", map[string]any{"expression": expression})
	n.Position = flow.Position{X: 3000, Y: 100}
	f.Nodes = append(f.Nodes, n)
	f.Connections = append(f.Connections, lintConn("cp", from, "output", "peek", "input"))
}

// A node an agent adds or changes runs on the admin's data: what it reads
// can come back to the agent through the runs it can list (an error quoting
// it, which nodes ran). Variables, message metadata (the settings of the
// nodes before it: a URL, an expression) and what a credential or a
// variable read produced are data_read_changed (audit finding F2).
func TestDiffAlertsDataReads(t *testing.T) {
	cases := []struct {
		name  string
		next  *flow.Flow
		nodes []string // the nodes alerted
	}{
		{name: "nothing changed", next: readsFlow(nil)},
		{name: "a side node on the trigger's message", next: readsFlow(func(f *flow.Flow) {
			peek(f, "t", "payload * 2")
		})},
		{name: "a side node reading variables", next: readsFlow(func(f *flow.Flow) {
			peek(f, "t", "duration(toJSON(vars))")
		}), nodes: []string{"peek"}},
		{name: "a side node reading flow variables", next: readsFlow(func(f *flow.Flow) {
			peek(f, "t", `flowVars["count"]`)
		}), nodes: []string{"peek"}},
		{name: "a side node reading metadata", next: readsFlow(func(f *flow.Flow) {
			peek(f, "log", "duration(join(split(meta.expression, \"\"), \".\"))")
		}), nodes: []string{"peek"}},
		{name: "a side node reading its whole environment", next: readsFlow(func(f *flow.Flow) {
			peek(f, "t", "toJSON($env)")
		}), nodes: []string{"peek"}},
		{name: "a node after a credentialed request", next: readsFlow(func(f *flow.Flow) {
			peek(f, "call", "int(toJSON(payload))")
		}), nodes: []string{"peek"}},
		{name: "a node after a variable read", next: readsFlow(func(f *flow.Flow) {
			peek(f, "setpoint", "int(toJSON(payload))")
		}), nodes: []string{"peek"}},
		{name: "a changed node after a credentialed request", next: readsFlow(func(f *flow.Flow) {
			diffNodeOf(f, "rlog").Config["message"] = "{{payload}}"
		}), nodes: []string{"rlog"}},
		{name: "a credentialed response wired into a node", next: readsFlow(func(f *flow.Flow) {
			f.Connections = append(f.Connections, lintConn("c6", "call", "output", "log", "input"))
		}), nodes: []string{"log"}},
		{name: "an approved variable read left alone", next: readsFlow(func(f *flow.Flow) {
			diffNodeOf(f, "log").Config["message"] = "tock"
		})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := flow.Diff(readsFlow(nil), tc.next, node.DefaultRegistry)
			var got []string
			for _, a := range r.Alerts {
				if a.Kind == flow.AlertDataReadChanged {
					got = append(got, a.NodeID)
					assert.Empty(t, a.Path)
					assert.Nil(t, a.Before)
					assert.Nil(t, a.After)
					assert.NotContains(t, a.Message, "setpoint\"]", "no values")
				}
			}
			assert.ElementsMatch(t, tc.nodes, got, "alerts: %v", r.Alerts)
		})
	}

	// Against an empty base every running node is new.
	r := flow.Diff(nil, readsFlow(nil), node.DefaultRegistry)
	var got []string
	for _, a := range r.Alerts {
		if a.Kind == flow.AlertDataReadChanged {
			got = append(got, a.NodeID)
		}
	}
	require.ElementsMatch(t, []string{"rlog", "setpoint", "slog"}, got)
}
