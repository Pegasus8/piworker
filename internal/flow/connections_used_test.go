package flow_test

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
)

// A running flow lists the broker connections its enabled nodes name
// (node.ConnectionRequirer: the Zigbee nodes and the MQTT nodes with a
// connection), sorted, each once; disabled nodes are not built, so they are
// left out.
func TestRuntimeConnectionsUsed(t *testing.T) {
	store := brokers.NewStore()
	for _, name := range []string{"home", "attic", "garage"} {
		require.NoError(t, store.Set(brokers.Connection{Name: name, Broker: "tcp://127.0.0.1:1"}))
	}
	ctx := brokers.WithStore(context.Background(), store)
	f := flow.NewFlow("conns")
	off := lintNode("off", "action-mqtt", map[string]any{"connection": "garage", "topic": "t"})
	off.Enabled = false
	f.Nodes = []flow.Node{
		lintNode("tick", "trigger-interval", map[string]any{"interval": float64(60000)}),
		lintNode("pub", "action-mqtt", map[string]any{"connection": "home", "topic": "t"}),
		lintNode("pub2", "action-mqtt", map[string]any{"connection": "home", "topic": "u"}),
		lintNode("own", "action-mqtt", map[string]any{"broker": "tcp://127.0.0.1:1", "topic": "t"}),
		lintNode("zb", "action-zigbee", map[string]any{"connection": "attic", "device": "lamp", "payload": "{}"}),
		off,
	}
	for _, id := range []string{"pub", "pub2", "own", "zb", "off"} {
		f.Connections = append(f.Connections, lintConn("c-"+id, "tick", "output", id, "input"))
	}
	m := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	require.NoError(t, m.Deploy(ctx, f))
	t.Cleanup(m.StopAll)
	rt, ok := m.GetRuntime(f.ID)
	require.True(t, ok)
	assert.Equal(t, []string{"attic", "home"}, rt.ConnectionsUsed())
}
