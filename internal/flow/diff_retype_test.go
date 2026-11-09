package flow_test

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
)

// retypeFlow is an approved external flow: an MQTT subscriber (broker,
// topic and a password secret) feeding an HTTP POST, a manual trigger
// feeding a log, and an RSS feed feeding the log.
func retypeFlow() *flow.Flow {
	f := diffFlow(
		lintNode("door", "trigger-mqtt", map[string]any{
			"broker": "tcp://127.0.0.1:1", "topic": "home/door/state", "password": "{{secret.mqtt_pw}}",
		}),
		lintNode("call", "action-http", map[string]any{"url": "https://api.example.com/door", "method": "POST"}),
		lintNode("start", "trigger-manual", nil),
		lintNode("log", "action-log", map[string]any{"message": "hi"}),
		lintNode("feed", "trigger-rss", map[string]any{"url": "https://news.example.org/rss", "intervalSec": float64(600)}),
	)
	f.Connections = []flow.Connection{
		lintConn("c1", "door", "output", "call", "input"),
		lintConn("c2", "start", "output", "log", "input"),
		lintConn("c3", "feed", "output", "log", "input"),
	}
	return f
}

// retype replaces node id of f with a node of another type (same position).
func retype(f *flow.Flow, id, typ string, cfg map[string]any) {
	n := diffNodeOf(f, id)
	pos := n.Position
	*n = lintNode(id, typ, cfg)
	n.Position = pos
}

// A node retyped under the same ID is a node removed and another added: an
// MQTT subscriber turned into a retained publisher with the same broker,
// topic and password sends where the admin approved only reading, so it
// raises what adding the publisher raises (audit finding F8).
func TestDiffAlertsTreatARetypedNodeAsRemovedAndAdded(t *testing.T) {
	cases := map[string]struct {
		change func(f *flow.Flow)
		want   []string
	}{
		"subscriber turned publisher": {func(f *flow.Flow) {
			retype(f, "door", "action-mqtt", map[string]any{
				"broker": "tcp://127.0.0.1:1", "topic": "home/door/state", "password": "{{secret.mqtt_pw}}",
				"payload": `{"state":"closed"}`, "retained": true,
			})
			f.Connections = append(f.Connections, lintConn("c4", "start", "output", "door", "input"))
		}, []string{flow.AlertDestinationChanged, flow.AlertSecretMoved}},
		// The feed URL kept, its interval left as an undeclared key.
		"feed turned request": {func(f *flow.Flow) {
			retype(f, "feed", "action-http", map[string]any{
				"url": "https://news.example.org/rss", "method": "POST", "intervalSec": float64(600),
			})
			f.Connections = append(f.Connections, lintConn("c4", "start", "output", "feed", "input"))
		}, []string{flow.AlertDestinationChanged, flow.AlertRateChanged}},
		// A manual trigger turned into an interval trigger (the default
		// interval, nothing configured) fires on its own.
		"manual turned interval": {func(f *flow.Flow) {
			retype(f, "start", "trigger-interval", nil)
		}, []string{flow.AlertRateChanged}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			next := retypeFlow()
			tc.change(next)
			r := flow.Diff(retypeFlow(), next, node.DefaultRegistry)
			for _, kind := range tc.want {
				assert.Contains(t, alertKinds(r), kind)
			}
		})
	}
}
