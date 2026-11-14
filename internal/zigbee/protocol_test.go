package zigbee

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTopics(t *testing.T) {
	assert.Equal(t, "zigbee2mqtt/living/plug", DeviceTopic("zigbee2mqtt", "living/plug"))
	assert.Equal(t, "zigbee2mqtt/living/plug/set", SetTopic("zigbee2mqtt", "living/plug"))
	assert.Equal(t, "zigbee2mqtt/living/plug/get", GetTopic("zigbee2mqtt", "living/plug"))
	assert.Equal(t, "zigbee2mqtt/living/plug/availability", AvailabilityTopic("zigbee2mqtt", "living/plug"))
	assert.Equal(t, "zigbee2mqtt/bridge/request/permit_join", BridgeTopic("zigbee2mqtt", "request/permit_join"))
}

func TestValidDeviceName(t *testing.T) {
	for _, ok := range []string{"living/plug", "bedroom_climate", "0xa4c1380000000003", "Sala de estar"} {
		assert.NoError(t, ValidDeviceName(ok), ok)
	}
	for _, bad := range []string{"", "bridge", "bridge/request/permit_join", "Bridge/x", "plug/set", "plug/get",
		"plug/availability", "a/+/b", "a/#", "a//b", "/a", "a/", strings.Repeat("x", 300), "a\x00b"} {
		assert.Error(t, ValidDeviceName(bad), "%q", bad)
	}
}

func TestParseAvailability(t *testing.T) {
	for payload, want := range map[string]bool{`{"state":"online"}`: true, `{"state":"offline"}`: false, "online": true, "offline": false} {
		online, ok := ParseAvailability([]byte(payload))
		require.True(t, ok, payload)
		assert.Equal(t, want, online, payload)
	}
	for _, bad := range []string{"", "maybe", `{"state":1}`, `[]`} {
		_, ok := ParseAvailability([]byte(bad))
		assert.False(t, ok, bad)
	}
}

func TestParseDevices(t *testing.T) {
	data, err := os.ReadFile("testdata/bridge_devices.json")
	require.NoError(t, err)
	devices, err := ParseDevices(data)
	require.NoError(t, err)
	require.Len(t, devices, 3, "the coordinator is left out")

	plug := devices[0]
	assert.Equal(t, "living/plug", plug.FriendlyName)
	assert.Equal(t, "S60ZBTPF", plug.Model)
	assert.Equal(t, "SONOFF", plug.Vendor)
	assert.Equal(t, "Router", plug.Type)
	assert.True(t, plug.Supported)
	state := plug.Feature("state")
	require.NotNil(t, state, "features of composite exposes are flattened")
	assert.True(t, state.Settable())
	assert.True(t, state.Gettable())
	assert.Equal(t, "ON", state.ValueOn)
	assert.Equal(t, "TOGGLE", state.ValueToggle)
	power := plug.Feature("power")
	require.NotNil(t, power)
	assert.Equal(t, "W", power.Unit)
	assert.False(t, power.Settable())
	assert.True(t, power.Published())
	assert.Equal(t, []string{"off", "on", "toggle", "previous"}, plug.Feature("power_on_behavior").Values)
	max := 4000.0
	assert.Equal(t, &max, plug.Feature("overload_protection").Max)

	climate := devices[1]
	assert.Equal(t, "Battery", climate.PowerSource)
	assert.Equal(t, []string{"temperature", "humidity", "battery", "temperature_calibration", "linkquality"}, climate.Properties())
	assert.Equal(t, []string{"temperature_calibration"}, climate.SettableProperties())

	unknown := devices[2]
	assert.False(t, unknown.Supported)
	assert.Empty(t, unknown.Features)

	_, err = ParseDevices([]byte(`{"not":"a list"}`))
	assert.Error(t, err)
}

func TestParseBridgeInfo(t *testing.T) {
	info, err := ParseBridgeInfo([]byte(`{"version":"2.1.0","permit_join":true,"permit_join_end":1731340000000,"coordinator":{"type":"EmberZNet","meta":{}}}`))
	require.NoError(t, err)
	assert.Equal(t, "2.1.0", info.Version)
	assert.True(t, info.PermitJoin)
	require.NotNil(t, info.PermitJoinEnd)
	assert.Equal(t, int64(1731340000000), *info.PermitJoinEnd)
	assert.Equal(t, "EmberZNet", info.Coordinator)

	// Zigbee2MQTT 1.x: permit_join_timeout in seconds.
	info, err = ParseBridgeInfo([]byte(`{"version":"1.40.0","permit_join":false,"coordinator":{"type":"zStack3x0"}}`))
	require.NoError(t, err)
	assert.False(t, info.PermitJoin)
	assert.Nil(t, info.PermitJoinEnd)
}
