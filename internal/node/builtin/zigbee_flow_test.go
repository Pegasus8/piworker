package builtin

import (
	"context"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The motion-light recipe end to end: a Zigbee Device trigger feeds a timer
// whose emission (a new run) turns the plug off through Zigbee Set.
func TestZigbeeTimerFlowTurnsThePlugOffAfterTheLastReport(t *testing.T) {
	// A running flow's nodes resolve connections in the process-wide store.
	b, _ := zigbeeEnv(t)
	require.NoError(t, brokers.DefaultStore.Set(brokers.Connection{Name: "pw_test_home", Broker: b.URL()}))
	t.Cleanup(func() { _ = brokers.DefaultStore.Delete("pw_test_home") })
	ctx := context.Background()
	f := flow.NewFlow("plug off")
	add := func(id, typ string, cat flow.NodeCategory, cfg map[string]any) {
		n := flow.NewNode(typ, cat)
		n.ID, n.Config = id, cfg
		f.AddNode(*n)
	}
	add("hum", "trigger-zigbee", flow.NodeCategoryInput, map[string]any{"connection": "pw_test_home", "device": "bedroom_climate", "property": "humidity"})
	add("wait", "process-timer", flow.NodeCategoryProcessing, map[string]any{"duration": 0.3, "durationUnit": "seconds"})
	add("off", "action-zigbee", flow.NodeCategoryOutput, map[string]any{"connection": "pw_test_home", "device": "living/plug", "command": "off"})
	f.AddConnection(*flow.NewConnection("hum", "output", "wait", "input"))
	f.AddConnection(*flow.NewConnection("wait", "output", "off", "input"))

	m := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	require.NoError(t, m.Deploy(ctx, f))
	t.Cleanup(func() { _ = m.Undeploy(f.ID) })
	waitSubscribes(t, b, 2)

	start := time.Now()
	b.Route("zigbee2mqtt/bedroom_climate", `{"humidity":71}`)
	time.Sleep(150 * time.Millisecond)
	b.Route("zigbee2mqtt/bedroom_climate", `{"humidity":73}`) // restarts the timer
	require.Eventually(t, func() bool {
		_, _, pubs, _ := b.Snapshot()
		return len(pubs) == 1
	}, 5*time.Second, 10*time.Millisecond)
	assert.GreaterOrEqual(t, time.Since(start), 440*time.Millisecond, "0.3 s after the second report")
	_, _, pubs, _ := b.Snapshot()
	assert.Equal(t, []string{`zigbee2mqtt/living/plug/set={"state":"OFF"}`}, pubs)

	// The deploy refuses an undefined connection.
	g := flow.NewFlow("other")
	n := flow.NewNode("trigger-zigbee", flow.NodeCategoryInput)
	n.ID, n.Config = "x", map[string]any{"connection": "nope", "device": "plug"}
	g.AddNode(*n)
	err := m.Deploy(brokers.WithStore(context.Background(), brokers.NewStore()), g)
	var verr *flow.ValidationError
	assert.ErrorAs(t, err, &verr)
}
