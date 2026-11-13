package flow_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
)

// ConnectionsUsed lists the broker connections the enabled nodes of a flow
// name (node.ConnectionRequirer: the Zigbee nodes and the MQTT nodes with a
// connection), sorted, each once; disabled nodes and nodes that fail to
// build are left out.
func TestConnectionsUsed(t *testing.T) {
	f := flow.NewFlow("conns")
	off := lintNode("off", "action-mqtt", map[string]any{"connection": "garage", "topic": "t"})
	off.Enabled = false
	f.Nodes = []flow.Node{
		lintNode("sub", "trigger-mqtt", map[string]any{"connection": "home", "topic": "t"}),
		lintNode("pub", "action-mqtt", map[string]any{"connection": "home", "topic": "t"}),
		lintNode("own", "action-mqtt", map[string]any{"broker": "tcp://localhost:1883", "topic": "t"}),
		lintNode("zb", "action-zigbee", map[string]any{"connection": "attic", "device": "lamp", "payload": "{}"}),
		lintNode("bad", "action-mqtt", map[string]any{"topic": "t"}),
		off,
	}
	assert.Equal(t, []string{"attic", "home"}, flow.ConnectionsUsed(f, node.DefaultRegistry))
	assert.Empty(t, flow.ConnectionsUsed(nil, node.DefaultRegistry))
}
