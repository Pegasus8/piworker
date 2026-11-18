package flow_test

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// garageFlow publishes two tools: garage_status (confirm none) answers
// through a transform; garage_open (confirm admin) posts to a webhook. A
// manual trigger feeds a log beside them.
func garageFlow(change func(f *flow.Flow)) *flow.Flow {
	schema := map[string]any{"type": "object", "properties": map[string]any{}}
	f := diffFlow(
		lintNode("status", "trigger-tool", map[string]any{"toolName": "garage_status", "description": "Door state.", "inputSchema": schema}),
		lintNode("answer", "process-transform", map[string]any{"expression": `"closed"`}),
		lintNode("sreply", "action-reply", nil),
		lintNode("open", "trigger-tool", map[string]any{"toolName": "garage_open", "description": "Opens the door.",
			"inputSchema": schema, "confirm": "admin"}),
		lintNode("pub", "action-http", map[string]any{"url": "https://ha.example.com/api/webhook/abc", "method": "POST"}),
		lintNode("oreply", "action-reply", nil),
		trigger(),
		lintNode("log", "action-log", map[string]any{"message": "hi"}),
	)
	f.Connections = []flow.Connection{
		lintConn("c1", "status", "output", "answer", "input"),
		lintConn("c2", "answer", "output", "sreply", "input"),
		lintConn("c3", "open", "output", "pub", "input"),
		lintConn("c4", "pub", "output", "oreply", "input"),
		lintConn("c5", "t", "output", "log", "input"),
	}
	if change != nil {
		change(f)
	}
	return f
}

func toolPathAlerts(r flow.DiffReport) map[string]flow.Alert {
	out := map[string]flow.Alert{}
	for _, a := range r.Alerts {
		if a.Kind == flow.AlertToolPathChanged {
			out[a.NodeID] = a
		}
	}
	return out
}

// A published tool is the admin's: what it runs may not change behind its
// name, grant and fingerprint. Wiring a confirm:none tool to the action
// behind a confirm:admin tool would run it without the admin's approval
// (audit finding F1).
func TestDiffAlertsToolPathChanges(t *testing.T) {
	cases := []struct {
		name  string
		next  *flow.Flow
		tools []string // the tool nodes alerted
	}{
		{name: "nothing changed", next: garageFlow(nil)},
		{name: "a node moved", next: garageFlow(func(f *flow.Flow) {
			diffNodeOf(f, "answer").Position = flow.Position{X: 5, Y: 5}
		})},
		{name: "a change on another branch", next: garageFlow(func(f *flow.Flow) {
			diffNodeOf(f, "log").Config["message"] = "bye"
		})},
		{name: "a safe tool wired to the admin-gated action", next: garageFlow(func(f *flow.Flow) {
			f.Connections = append(f.Connections, lintConn("c6", "status", "output", "pub", "input"))
		}), tools: []string{"open", "status"}},
		{name: "a gated tool wired to another action", next: garageFlow(func(f *flow.Flow) {
			f.Nodes = append(f.Nodes, lintNode("lights", "action-http", map[string]any{"url": "https://ha.example.com/api/webhook/abc", "method": "PUT"}))
			f.Connections = append(f.Connections, lintConn("c6", "open", "output", "lights", "input"))
		}), tools: []string{"open"}},
		{name: "what a tool computes", next: garageFlow(func(f *flow.Flow) {
			diffNodeOf(f, "answer").Config["expression"] = `"open"`
		}), tools: []string{"status"}},
		{name: "a node taken off a tool's path", next: garageFlow(func(f *flow.Flow) {
			f.Connections = append(f.Connections[:1], f.Connections[2:]...) // answer no longer replies
		}), tools: []string{"status"}},
		{name: "a branch feeding a tool's path", next: garageFlow(func(f *flow.Flow) {
			f.Connections = append(f.Connections, lintConn("c6", "t", "output", "answer", "input"))
		}), tools: []string{"status"}},
		{name: "a node on a tool's path disabled", next: garageFlow(func(f *flow.Flow) {
			diffNodeOf(f, "answer").Enabled = false
		}), tools: []string{"status"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := flow.Diff(garageFlow(nil), tc.next, node.DefaultRegistry)
			got := toolPathAlerts(r)
			var ids []string
			for id := range got {
				ids = append(ids, id)
			}
			assert.ElementsMatch(t, tc.tools, ids, "alerts: %v", r.Alerts)
			for _, a := range got {
				assert.Empty(t, a.Path)
				assert.Nil(t, a.Before)
				assert.Nil(t, a.After)
				assert.NotContains(t, a.Message, "ha.example.com", "no values")
			}
		})
	}

	// Against an empty base (a flow the admin never approved) every tool
	// is new.
	r := flow.Diff(nil, garageFlow(nil), node.DefaultRegistry)
	require.Len(t, toolPathAlerts(r), 2)
}
