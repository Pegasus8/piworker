package builtin

import (
	"context"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newZigbeeAction(t *testing.T, cfg map[string]interface{}) *ZigbeeAction {
	t.Helper()
	n, err := NewZigbeeAction(cfg)
	require.NoError(t, err)
	a := n.(*ZigbeeAction)
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func lastPublish(t *testing.T, b interface {
	Snapshot() ([]string, []string, []string, int)
}) string {
	t.Helper()
	_, _, pubs, _ := b.Snapshot()
	require.NotEmpty(t, pubs)
	return pubs[len(pubs)-1]
}

func TestZigbeeActionCommands(t *testing.T) {
	b, ctx := zigbeeEnv(t)
	msg := types.NewMessage(map[string]interface{}{"device": "living/plug", "level": 120, "on": true}, types.DataTypeObject)
	for _, c := range []struct {
		cfg  map[string]interface{}
		want string
	}{
		{map[string]interface{}{"command": "on"}, `zigbee2mqtt/living/plug/set={"state":"ON"}`},
		{map[string]interface{}{"command": "off"}, `zigbee2mqtt/living/plug/set={"state":"OFF"}`},
		{map[string]interface{}{"command": "toggle"}, `zigbee2mqtt/living/plug/set={"state":"TOGGLE"}`},
		{map[string]interface{}{"command": "set", "property": "brightness", "value": "{{payload.level}}"}, `zigbee2mqtt/living/plug/set={"brightness":120}`},
		{map[string]interface{}{"command": "set", "property": "child_lock", "value": "{{payload.on}}"}, `zigbee2mqtt/living/plug/set={"child_lock":true}`},
		{map[string]interface{}{"command": "set", "property": "power_on_behavior", "value": "previous"}, `zigbee2mqtt/living/plug/set={"power_on_behavior":"previous"}`},
		{map[string]interface{}{"command": "raw", "payload": `{"state":"ON","countdown":{{payload.level}}}`}, `zigbee2mqtt/living/plug/set={"state":"ON","countdown":120}`},
	} {
		cfg := map[string]interface{}{"connection": "home", "device": "{{payload.device}}"}
		for k, v := range c.cfg {
			cfg[k] = v
		}
		a := newZigbeeAction(t, cfg)
		out, err := a.Process(ctx, msg)
		require.NoError(t, err, c.want)
		require.Len(t, out, 1)
		assert.Equal(t, "output", out[0].SourcePort)
		assert.Equal(t, c.want, lastPublish(t, b))
		assert.Equal(t, "living/plug", out[0].Meta["zigbeeDevice"])
	}
	assert.Equal(t, 1, b.ConnCount(), "one shared connection")
}

func TestZigbeeActionConfig(t *testing.T) {
	for _, cfg := range []map[string]interface{}{
		{"device": "plug", "command": "on"},
		{"connection": "home", "command": "on"},
		{"connection": "home", "device": "bridge/request/permit_join", "command": "raw", "payload": "{}"},
		{"connection": "home", "device": "plug", "command": "dance"},
		{"connection": "home", "device": "plug", "command": "set"},
		{"connection": "home", "device": "plug", "command": "set", "property": "a b"},
		{"connection": "home", "device": "plug", "command": "raw"},
		{"connection": "home", "device": "plug", "command": "raw", "payload": "[1]"},
	} {
		_, err := NewZigbeeAction(cfg)
		assert.Error(t, err, "%v", cfg)
	}
	a := newZigbeeAction(t, map[string]interface{}{"connection": "home", "device": "plug"})
	assert.Equal(t, "toggle", a.command, "toggle by default")
	assert.Equal(t, map[string]string{"connection": "home"}, node.ConnectionRequirer(a).RequiredConnections())
}

// A device name from the message is checked after rendering: the bridge,
// wildcards and command suffixes are refused, permanently.
func TestZigbeeActionRefusesRenderedBridgeAndWildcards(t *testing.T) {
	b, ctx := zigbeeEnv(t)
	a := newZigbeeAction(t, map[string]interface{}{"connection": "home", "device": "{{payload}}", "command": "on"})
	for _, d := range []string{"bridge/request/permit_join", "a/#", "", "plug/set"} {
		_, err := a.Process(ctx, types.NewMessage(d, types.DataTypeString))
		require.Error(t, err, d)
		assert.False(t, node.IsRetryable(err), d)
	}
	_, _, pubs, _ := b.Snapshot()
	assert.Empty(t, pubs)

	raw := newZigbeeAction(t, map[string]interface{}{"connection": "home", "device": "plug", "command": "raw", "payload": "{{payload}}"})
	_, err := raw.Process(ctx, types.NewMessage("not json", types.DataTypeString))
	require.Error(t, err)
	assert.False(t, node.IsRetryable(err))
}

// An unreachable broker is a transient error bounded by the message's
// deadline.
func TestZigbeeActionUnreachableIsTransient(t *testing.T) {
	b, ctx := zigbeeEnv(t)
	b.Close()
	a := newZigbeeAction(t, map[string]interface{}{"connection": "home", "device": "plug", "command": "on"})
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	_, err := a.Process(ctx, types.NewMessage(nil, types.DataTypeAny))
	require.Error(t, err)
	assert.True(t, node.IsRetryable(err))
}
