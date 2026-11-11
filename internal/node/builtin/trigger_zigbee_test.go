package builtin

import (
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZigbeeTriggerConfig(t *testing.T) {
	_, err := NewZigbeeTrigger(map[string]interface{}{"connection": "home", "device": "plug"})
	require.NoError(t, err)
	for _, cfg := range []map[string]interface{}{
		{"device": "plug"},
		{"connection": "home"},
		{"connection": "home", "device": "bridge/state"},
		{"connection": "home", "device": "plug/#"},
		{"connection": "home", "device": "{{payload.d}}"},
		{"connection": "{{payload.c}}", "device": "plug"},
		{"connection": "home", "device": "plug", "minChange": float64(-1)},
	} {
		_, err := NewZigbeeTrigger(cfg)
		assert.Error(t, err, "%v", cfg)
	}
	n, _ := NewZigbeeTrigger(map[string]interface{}{"connection": "home", "device": "plug"})
	assert.Equal(t, map[string]string{"connection": "home"}, n.(node.ConnectionRequirer).RequiredConnections())
}

// Change mode with a property: fires on the first reading and then only when
// the property changes by at least minChange; other reports are ignored.
func TestZigbeeTriggerPropertyChanges(t *testing.T) {
	b, ctx := zigbeeEnv(t)
	out := startZigbeeTrigger(t, ctx, map[string]interface{}{"connection": "home", "device": "bedroom_climate",
		"property": "temperature", "minChange": 0.5})
	waitSubscribes(t, b, 2)

	b.Route("zigbee2mqtt/bedroom_climate", `{"temperature":21.3,"humidity":55,"battery":90}`)
	m := nextMsg(t, out)
	assert.Equal(t, "output", m.SourcePort)
	p := m.Payload.(map[string]interface{})
	assert.Equal(t, "bedroom_climate", p["device"])
	assert.Equal(t, "temperature", p["property"])
	assert.Equal(t, 21.3, p["value"])
	assert.Nil(t, p["previous"])
	assert.Equal(t, 55.0, p["state"].(map[string]interface{})["humidity"])

	b.Route("zigbee2mqtt/bedroom_climate", `{"temperature":21.5,"humidity":60}`) // +0.2: below minChange
	noMsg(t, out)
	b.Route("zigbee2mqtt/bedroom_climate", `{"temperature":21.8,"humidity":60}`) // +0.5 from the last fired
	p = nextMsg(t, out).Payload.(map[string]interface{})
	assert.Equal(t, 21.8, p["value"])
	assert.Equal(t, 21.3, p["previous"])
	b.Route("zigbee2mqtt/bedroom_climate", `{"humidity":61}`) // no temperature
	noMsg(t, out)
}

// Without a property, change mode fires when the state changes, ignoring
// link quality and timestamps; every mode fires on every report.
func TestZigbeeTriggerStateChangesAndEvery(t *testing.T) {
	b, ctx := zigbeeEnv(t)
	changes := startZigbeeTrigger(t, ctx, map[string]interface{}{"connection": "home", "device": "living/plug"})
	every := startZigbeeTrigger(t, ctx, map[string]interface{}{"connection": "home", "device": "living/plug", "mode": "every"})
	waitSubscribes(t, b, 2) // both share the device's and availability's subscriptions

	b.Route("zigbee2mqtt/living/plug", `{"state":"ON","power":12,"linkquality":80}`)
	assert.Equal(t, "ON", nextMsg(t, changes).Payload.(map[string]interface{})["state"].(map[string]interface{})["state"])
	nextMsg(t, every)
	b.Route("zigbee2mqtt/living/plug", `{"state":"ON","power":12,"linkquality":40,"last_seen":"2026-11-11T10:00:00Z"}`)
	noMsg(t, changes)
	nextMsg(t, every)
	b.Route("zigbee2mqtt/living/plug", `{"state":"OFF","power":0,"linkquality":40}`)
	nextMsg(t, changes)
	nextMsg(t, every)
}

// Button actions are events: each one fires, an empty one never.
func TestZigbeeTriggerActionsAlwaysFire(t *testing.T) {
	b, ctx := zigbeeEnv(t)
	out := startZigbeeTrigger(t, ctx, map[string]interface{}{"connection": "home", "device": "button", "property": "action"})
	waitSubscribes(t, b, 2)
	b.Route("zigbee2mqtt/button", `{"action":"single"}`)
	assert.Equal(t, "single", nextMsg(t, out).Payload.(map[string]interface{})["value"])
	b.Route("zigbee2mqtt/button", `{"action":"single"}`)
	assert.Equal(t, "single", nextMsg(t, out).Payload.(map[string]interface{})["value"])
	b.Route("zigbee2mqtt/button", `{"action":""}`)
	noMsg(t, out)
}

// Availability goes to its own output: the first report only when the
// device is offline, then every change.
func TestZigbeeTriggerAvailability(t *testing.T) {
	b, ctx := zigbeeEnv(t)
	out := startZigbeeTrigger(t, ctx, map[string]interface{}{"connection": "home", "device": "bedroom_climate"})
	waitSubscribes(t, b, 2)
	b.Route("zigbee2mqtt/bedroom_climate/availability", `{"state":"online"}`)
	noMsg(t, out)
	b.Route("zigbee2mqtt/bedroom_climate/availability", `{"state":"offline"}`)
	m := nextMsg(t, out)
	assert.Equal(t, "availability", m.SourcePort)
	assert.Equal(t, map[string]interface{}{"device": "bedroom_climate", "online": false, "state": "offline"}, m.Payload)
	b.Route("zigbee2mqtt/bedroom_climate/availability", "offline")
	noMsg(t, out)
	b.Route("zigbee2mqtt/bedroom_climate/availability", "online")
	assert.Equal(t, true, nextMsg(t, out).Payload.(map[string]interface{})["online"])
}

// A connection the context's store does not define fails Start (deploy
// refuses it before, with undefined_connection).
func TestZigbeeTriggerUndefinedConnection(t *testing.T) {
	_, ctx := zigbeeEnv(t)
	n, err := NewZigbeeTrigger(map[string]interface{}{"connection": "other", "device": "plug"})
	require.NoError(t, err)
	err = n.(*ZigbeeTrigger).Start(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"other"`)
	_ = time.Second
}
