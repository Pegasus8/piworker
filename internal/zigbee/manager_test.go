package zigbee

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/mqtttest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func managerEnv(t *testing.T) (*mqtttest.Broker, *brokers.Store, *Manager) {
	t.Helper()
	b := mqtttest.New(t)
	store := brokers.NewStore()
	require.NoError(t, store.Set(brokers.Connection{Name: "home", Broker: b.URL()}))
	m := NewManager(store)
	m.Start()
	t.Cleanup(m.Stop)
	require.Eventually(t, func() bool { _, subs, _, _ := b.Snapshot(); return len(subs) == 1 }, 5*time.Second, 10*time.Millisecond)
	_, subs, _, _ := b.Snapshot()
	require.Equal(t, []string{"zigbee2mqtt/#"}, subs)
	return b, store, m
}

// The manager learns the bridge, the devices and each device's last state
// and availability from what Zigbee2MQTT publishes.
func TestManagerTracksTheNetwork(t *testing.T) {
	b, _, m := managerEnv(t)
	devices, err := os.ReadFile("testdata/bridge_devices.json")
	require.NoError(t, err)
	b.Route("zigbee2mqtt/bridge/state", `{"state":"online"}`)
	b.Route("zigbee2mqtt/bridge/info", `{"version":"2.1.0","permit_join":false,"coordinator":{"type":"EmberZNet"}}`)
	b.Route("zigbee2mqtt/bridge/devices", string(devices))
	b.Route("zigbee2mqtt/living/plug", `{"state":"ON","power":12.5}`)
	b.Route("zigbee2mqtt/living/plug/availability", `{"state":"online"}`)
	b.Route("zigbee2mqtt/bedroom_climate", `{"temperature":21.3,"humidity":55,"battery":87}`)
	b.Route("zigbee2mqtt/living/plug/set", `{"state":"OFF"}`) // a command, not a report

	require.Eventually(t, func() bool {
		n, ok := m.Network("home")
		return ok && len(n.Devices) == 3 && n.Devices[1].State != nil && n.Devices[0].State != nil
	}, 5*time.Second, 10*time.Millisecond)
	n, _ := m.Network("home")
	assert.True(t, n.Connected)
	assert.True(t, n.Bridge.Online)
	assert.Equal(t, "2.1.0", n.Bridge.Info.Version)
	plug := n.Devices[0]
	assert.Equal(t, "living/plug", plug.FriendlyName)
	assert.Equal(t, "ON", plug.State["state"])
	require.NotNil(t, plug.Online)
	assert.True(t, *plug.Online)
	assert.NotNil(t, plug.LastSeen)
	climate := n.Devices[1]
	assert.Equal(t, 87.0, climate.State["battery"])
	assert.Nil(t, climate.Online, "no availability report")

	state, ok := m.DeviceState("home", "bedroom_climate")
	require.True(t, ok)
	assert.Equal(t, 21.3, state["temperature"])
	_, ok = m.DeviceState("home", "unknown")
	assert.False(t, ok)
	_, ok = m.Network("other")
	assert.False(t, ok)
}

// Permit join publishes Zigbee2MQTT's request, bounded to 254 seconds.
func TestManagerPermitJoin(t *testing.T) {
	b, _, m := managerEnv(t)
	require.Eventually(t, func() bool { n, _ := m.Network("home"); return n.Connected }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, m.PermitJoin("home", 120))
	require.NoError(t, m.PermitJoin("home", 0))
	_, _, pubs, _ := b.Snapshot()
	assert.Equal(t, []string{`zigbee2mqtt/bridge/request/permit_join={"time":120,"value":true}`,
		`zigbee2mqtt/bridge/request/permit_join={"time":0,"value":false}`}, pubs)
	assert.Error(t, m.PermitJoin("home", 255))
	assert.Error(t, m.PermitJoin("other", 10))
}

// A changed or deleted connection is followed: its watcher restarts or
// stops.
func TestManagerFollowsConnectionChanges(t *testing.T) {
	b, store, m := managerEnv(t)
	require.NoError(t, store.Set(brokers.Connection{Name: "home", Broker: b.URL(), BaseTopic: "z2m"}))
	require.Eventually(t, func() bool {
		_, subs, _, _ := b.Snapshot()
		return len(subs) == 2 && subs[1] == "z2m/#"
	}, 5*time.Second, 10*time.Millisecond)
	b.Route("z2m/bridge/state", "online")
	require.Eventually(t, func() bool { n, _ := m.Network("home"); return n.Bridge.Online }, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, store.Delete("home"))
	require.Eventually(t, func() bool { return b.ConnCount() == 0 }, 5*time.Second, 10*time.Millisecond)
	_, ok := m.Network("home")
	assert.False(t, ok)
}

// Oversized or malformed reports are dropped, and the number of devices
// tracked is bounded.
func TestManagerBounds(t *testing.T) {
	b, _, m := managerEnv(t)
	b.Route("zigbee2mqtt/big", `{"x":"`+strings.Repeat("a", maxStateBytes)+`"}`)
	b.Route("zigbee2mqtt/notjson", `hello`)
	b.Route("zigbee2mqtt/ok", `{"a":1}`)
	require.Eventually(t, func() bool { _, ok := m.DeviceState("home", "ok"); return ok }, 5*time.Second, 10*time.Millisecond)
	_, ok := m.DeviceState("home", "big")
	assert.False(t, ok)
	_, ok = m.DeviceState("home", "notjson")
	assert.False(t, ok)
}
