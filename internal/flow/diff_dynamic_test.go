package flow_test

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dynamicFlow is an approved publish whose topic takes message data, with
// the admin's broker password, fed by a manual trigger through a transform,
// and a side branch.
func dynamicFlow(change func(f *flow.Flow)) *flow.Flow {
	f := diffFlow(
		trigger(),
		lintNode("calc", "process-transform", map[string]any{"expression": `{device: "kitchen_light", cmd: "ON"}`}),
		lintNode("pub", "action-mqtt", map[string]any{"broker": "tcp://192.168.1.10:1883",
			"topic": "zigbee2mqtt/{{payload.device}}/set", "payload": "{{payload.cmd}}", "password": "{{secret.mqtt_pw}}"}),
		lintNode("side", "process-transform", map[string]any{"expression": "payload"}),
	)
	f.Connections = []flow.Connection{
		lintConn("c1", "t", "output", "calc", "input"),
		lintConn("c2", "calc", "output", "pub", "input"),
		lintConn("c3", "t", "output", "side", "input"),
	}
	if change != nil {
		change(f)
	}
	return f
}

// A destination that takes message data goes where what feeds it says: a
// change upstream of an approved templated topic (or URL) steers it, with
// the admin's credentials or not, so it is a dynamic_destination alert like
// writing the template (audit finding F9).
func TestDiffFlagsChangesUpstreamOfADynamicDestination(t *testing.T) {
	uncredentialed := func(f *flow.Flow) {
		delete(diffNodeOf(f, "pub").Config, "password")
	}
	cases := []struct {
		name       string
		base, next *flow.Flow
		alert      bool
	}{
		{name: "nothing changed", base: dynamicFlow(nil), next: dynamicFlow(nil)},
		{name: "the message shaped upstream changed", base: dynamicFlow(nil), next: dynamicFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = `{device: "front_door_lock", cmd: "UNLOCK"}`
		}), alert: true},
		{name: "a transform inserted upstream", base: dynamicFlow(nil), next: dynamicFlow(func(f *flow.Flow) {
			f.Nodes = append(f.Nodes, lintNode("inject", "process-transform", map[string]any{"expression": `{device: "garage"}`}))
			f.Nodes[len(f.Nodes)-1].Position = flow.Position{X: 2000, Y: 100}
			f.Connections[1] = lintConn("c2", "calc", "output", "inject", "input")
			f.Connections = append(f.Connections, lintConn("c4", "inject", "output", "pub", "input"))
		}), alert: true},
		{name: "a new input", base: dynamicFlow(nil), next: dynamicFlow(func(f *flow.Flow) {
			f.Connections = append(f.Connections, lintConn("c4", "side", "output", "pub", "input"))
		}), alert: true},
		{name: "without credentials", base: dynamicFlow(uncredentialed), next: dynamicFlow(func(f *flow.Flow) {
			uncredentialed(f)
			diffNodeOf(f, "calc").Config["expression"] = `{device: "front_door_lock"}`
		}), alert: true},
		{name: "a change on another branch", base: dynamicFlow(nil), next: dynamicFlow(func(f *flow.Flow) {
			diffNodeOf(f, "side").Config["expression"] = "payload * 2"
		})},
		// Message content on a fixed destination is not a destination.
		{name: "a static topic", base: dynamicFlow(func(f *flow.Flow) {
			diffNodeOf(f, "pub").Config["topic"] = "zigbee2mqtt/kitchen_light/set"
		}), next: dynamicFlow(func(f *flow.Flow) {
			diffNodeOf(f, "pub").Config["topic"] = "zigbee2mqtt/kitchen_light/set"
			diffNodeOf(f, "calc").Config["expression"] = `{cmd: "OFF"}`
		})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := flow.Diff(tc.base, tc.next, node.DefaultRegistry)
			var got []flow.Alert
			for _, a := range r.Alerts {
				if a.Kind == flow.AlertDynamicDestination {
					got = append(got, a)
				}
			}
			if !tc.alert {
				assert.Empty(t, got, "alerts: %v", r.Alerts)
				return
			}
			require.Len(t, got, 1, "alerts: %v", r.Alerts)
			assert.Equal(t, "pub", got[0].NodeID)
			assert.Equal(t, "config.topic", got[0].Path)
			assert.Nil(t, got[0].Before)
			assert.Nil(t, got[0].After)
			assert.NotContains(t, got[0].Message, "zigbee2mqtt", "no values")
		})
	}
}
