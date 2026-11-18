package builtin

import (
	"context"
	"github.com/Pegasus8/piworker/internal/mqtttest"
	"net"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/types"
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
	assert.Equal(t, "tcp://localhost:1883", a.target.broker)
	assert.Equal(t, "sensors/temp", a.topic)
	assert.Equal(t, byte(1), a.qos)
	assert.True(t, a.retained)
	// Lazily connected: constructing must not dial the broker.
	assert.Nil(t, a.conn)
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

// Without a client id of its own (empty, or the constant older flows
// stored) the action shares a pooled connection (ADR-0025).
func TestMqttActionClientID(t *testing.T) {
	for _, v := range []string{"", "  ", "piworker-pub"} {
		a := newMqttAction(t, map[string]interface{}{"broker": "tcp://x:1883", "topic": "t", "clientId": v})
		assert.Empty(t, a.clientID, "clientId %q", v)
	}
	assert.Empty(t, newMqttAction(t, map[string]interface{}{"broker": "tcp://x:1883", "topic": "t"}).clientID)
	assert.Equal(t, "mine", newMqttAction(t, map[string]interface{}{"broker": "tcp://x:1883", "topic": "t", "clientId": "mine"}).clientID)
}

func TestMqttActionPublishesAndCloseDisconnects(t *testing.T) {
	b := mqtttest.New(t)
	a := newMqttAction(t, map[string]interface{}{"broker": b.URL(), "topic": "out/t", "payload": "hi", "qos": 1})

	_, err := a.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
	require.NoError(t, err)
	_, _, pubs, _ := b.Snapshot()
	assert.Equal(t, []string{"out/t=hi"}, pubs)

	require.NoError(t, a.Close())
	require.Eventually(t, func() bool {
		_, _, _, d := b.Snapshot()
		return d == 1 && b.ConnCount() == 0
	}, 5*time.Second, 20*time.Millisecond, "Close must disconnect the publish client")
	require.NoError(t, a.Close(), "Close is idempotent")
}

// A broker that accepts TCP but never answers CONNECT must not pin the node's
// executor past the caller's deadline.
func TestMqttActionPublishRespectsContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		var conns []net.Conn
		defer func() {
			for _, c := range conns {
				_ = c.Close()
			}
		}()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c)
		}
	}()

	a := newMqttAction(t, map[string]interface{}{"broker": "tcp://" + ln.Addr().String(), "topic": "t"})
	t.Cleanup(func() { _ = a.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = a.Process(ctx, types.NewMessage("x", types.DataTypeString))
	assert.Error(t, err)
	assert.Less(t, time.Since(start), 3*time.Second)
}
