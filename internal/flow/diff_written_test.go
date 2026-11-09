package flow_test

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
)

// writtenFlow is a trigger and a request whose URL (a setting agents may
// not read) is the admin's, with its secret in the hidden headers.
func writtenFlow() *flow.Flow {
	f := diffFlow(
		trigger(),
		lintNode("call", "action-http", map[string]any{"url": "http://192.168.1.50/lock", "method": "POST",
			"headers": map[string]any{"Authorization": "{{secret.api_key}}"}}),
	)
	f.Connections = []flow.Connection{lintConn("c1", "t", "output", "call", "input")}
	return f
}

// The alerts of what an agent wrote never compare it with what it may not
// read (ADR-0021, audit finding F10): a written hidden setting counts as
// changed even when equal to the base's, and a written host or secret name
// counts as new unless a setting agents may read uses it. What it kept, or
// may read, is compared as before.
func TestDiffWithWrittenSettings(t *testing.T) {
	written := func(props ...string) flow.DiffOptions {
		set := map[string]bool{}
		for _, p := range props {
			set[p] = true
		}
		return flow.DiffOptions{Written: func(id, prop string) bool { return set[id+"."+prop] }}
	}
	probe := func(f *flow.Flow) {
		n := lintNode("probe", "action-http", map[string]any{"url": "http://192.168.1.50/x",
			"headers": map[string]any{"Authorization": "{{secret.api_key}}"}})
		n.Position = flow.Position{X: 900, Y: 100}
		f.Nodes = append(f.Nodes, n)
		f.Connections = append(f.Connections, lintConn("c2", "t", "output", "probe", "input"))
	}
	cases := []struct {
		name   string
		change func(f *flow.Flow)
		opts   flow.DiffOptions
		want   []string
	}{
		{name: "a hidden URL written as it was", change: func(*flow.Flow) {}, opts: written("call.url"),
			want: []string{flow.AlertDestinationChanged, flow.AlertNewHost}},
		{name: "a hidden URL kept", change: func(*flow.Flow) {}, opts: written(), want: []string{}},
		{name: "the same, compared (the editor)", change: func(*flow.Flow) {}, opts: flow.DiffOptions{}, want: []string{}},
		{name: "a visible setting written as it was", change: func(*flow.Flow) {}, opts: written("call.method"), want: []string{}},
		{name: "a hidden host and secret written on a new node", change: probe, opts: written("probe.url", "probe.headers"),
			want: []string{flow.AlertDestinationChanged, flow.AlertNewHost, flow.AlertSecretAdded}},
		{name: "the same, compared (the editor)", change: probe, opts: flow.DiffOptions{},
			want: []string{flow.AlertDestinationChanged, flow.AlertSecretMoved}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := writtenFlow()
			tc.change(next)
			r := flow.DiffWith(writtenFlow(), next, node.DefaultRegistry, tc.opts)
			kinds := map[string]bool{}
			for _, k := range alertKinds(r) {
				kinds[k] = true
			}
			got := []string{}
			for k := range kinds {
				got = append(got, k)
			}
			assert.ElementsMatch(t, tc.want, got, "alerts: %v", r.Alerts)
		})
	}
}
