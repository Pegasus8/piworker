package builtin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMqttAction(t *testing.T, config map[string]interface{}) *MqttAction {
	t.Helper()
	n, err := NewMqttAction(config)
	require.NoError(t, err)
	return n.(*MqttAction)
}

func TestMqttActionParsesConfig(t *testing.T) {
	a := newMqttAction(t, map[string]interface{}{
		"broker": "tcp://localhost:1883", "topic": "sensors/temp",
		"qos": 1, "retained": true, "payload": "{{payload}}",
	})
	assert.Equal(t, "tcp://localhost:1883", a.broker)
	assert.Equal(t, "sensors/temp", a.topic)
	assert.Equal(t, byte(1), a.qos)
	assert.True(t, a.retained)
	// Lazily connected: constructing must not dial the broker.
	assert.Nil(t, a.client)
}

func TestMqttActionRequiresBrokerAndTopic(t *testing.T) {
	_, err := NewMqttAction(map[string]interface{}{"topic": "t"})
	assert.Error(t, err, "missing broker")
	_, err = NewMqttAction(map[string]interface{}{"broker": "tcp://x:1883"})
	assert.Error(t, err, "missing topic")
}

func TestMqttActionClampsQoS(t *testing.T) {
	a := newMqttAction(t, map[string]interface{}{"broker": "tcp://x:1883", "topic": "t", "qos": 9})
	assert.Equal(t, byte(0), a.qos, "out-of-range QoS falls back to 0")
}
