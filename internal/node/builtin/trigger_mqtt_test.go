package builtin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMqttTrigger(t *testing.T, config map[string]interface{}) *MqttTrigger {
	t.Helper()
	n, err := NewMqttTrigger(config)
	require.NoError(t, err)
	return n.(*MqttTrigger)
}

func TestMqttTriggerParsesConfig(t *testing.T) {
	tr := newMqttTrigger(t, map[string]interface{}{
		"broker": "tcp://localhost:1883", "topic": "sensors/#", "qos": 2,
	})
	assert.Equal(t, "tcp://localhost:1883", tr.broker)
	assert.Equal(t, "sensors/#", tr.topic)
	assert.Equal(t, byte(2), tr.qos)
}

func TestMqttTriggerRequiresBrokerAndTopic(t *testing.T) {
	_, err := NewMqttTrigger(map[string]interface{}{"topic": "t"})
	assert.Error(t, err)
	_, err = NewMqttTrigger(map[string]interface{}{"broker": "tcp://x:1883"})
	assert.Error(t, err)
}

func TestMqttMessagePayloadParsesJSON(t *testing.T) {
	p := mqttMessagePayload("sensors/temp", []byte(`{"value":21.5}`))
	assert.Equal(t, "sensors/temp", p["topic"])
	body, ok := p["payload"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 21.5, body["value"])
}

func TestMqttMessagePayloadFallsBackToString(t *testing.T) {
	p := mqttMessagePayload("sensors/temp", []byte("not json"))
	assert.Equal(t, "not json", p["payload"])
}
