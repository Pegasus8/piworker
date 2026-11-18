package builtin

import (
	"context"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/mqtttest"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The MQTT nodes take either a named broker connection (ADR-0029) or a
// broker URL with its own credentials, never both, and a connection's
// credentials are the connection's: the node's own are refused.
func TestMqttNodesTakeAConnectionOrABroker(t *testing.T) {
	for _, typ := range []string{"trigger-mqtt", "action-mqtt"} {
		n, err := node.DefaultRegistry.Create(typ, map[string]any{"connection": "home", "topic": "t"})
		require.NoError(t, err, typ)
		req, ok := n.(node.ConnectionRequirer)
		require.True(t, ok, "%s names its connection", typ)
		assert.Equal(t, map[string]string{"connection": "home"}, req.RequiredConnections(), typ)
		assert.Empty(t, n.(node.SecretReferencer).SecretReferences(), "%s: the connection's credentials are its own", typ)

		n, err = node.DefaultRegistry.Create(typ, map[string]any{"broker": "tcp://localhost:1883", "topic": "t"})
		require.NoError(t, err, typ)
		assert.Empty(t, n.(node.ConnectionRequirer).RequiredConnections(), "%s with a broker names no connection", typ)

		_, err = node.DefaultRegistry.Create(typ, map[string]any{"connection": "home", "broker": "tcp://localhost:1883", "topic": "t"})
		assert.ErrorContains(t, err, "either", typ)
		_, err = node.DefaultRegistry.Create(typ, map[string]any{"topic": "t"})
		assert.ErrorContains(t, err, "connection", typ)
		_, err = node.DefaultRegistry.Create(typ, map[string]any{"connection": "bad name", "topic": "t"})
		assert.ErrorContains(t, err, "connection", typ)
		_, err = node.DefaultRegistry.Create(typ, map[string]any{"connection": "home", "topic": "t", "password": "{{secret.x}}"})
		assert.ErrorContains(t, err, "connection", typ)
	}
}

// mqttConnEnv is a fake broker and a context whose broker store holds the
// connection "home" to it.
func mqttConnEnv(t *testing.T) (*mqtttest.Broker, context.Context) {
	t.Helper()
	b := mqtttest.New(t)
	store := brokers.NewStore()
	require.NoError(t, store.Set(brokers.Connection{Name: "home", Broker: b.URL()}))
	ctx, cancel := context.WithCancel(brokers.WithStore(context.Background(), store))
	t.Cleanup(cancel)
	return b, ctx
}

// A subscriber with a connection subscribes on that connection's broker,
// resolved through its context when it starts.
func TestMqttTriggerUsesItsConnection(t *testing.T) {
	b, ctx := mqttConnEnv(t)
	n, err := NewMqttTrigger(map[string]any{"connection": "home", "topic": "sensors/#"})
	require.NoError(t, err)
	tr := n.(*MqttTrigger)
	out := make(chan *types.Message, 4)
	require.NoError(t, tr.Start(ctx, out))
	t.Cleanup(func() { _ = tr.Stop() })
	require.Eventually(t, func() bool { return subscribeCount(b) == 1 }, 5*time.Second, 20*time.Millisecond)

	b.PublishToAll("sensors/t", `{"v":1}`)
	select {
	case msg := <-out:
		assert.Equal(t, "sensors/t", msg.Payload.(map[string]any)["topic"])
	case <-time.After(5 * time.Second):
		t.Fatal("no message")
	}
}

// A subscriber whose connection is not defined fails to start (deploy
// refuses it earlier: undefined_connection).
func TestMqttTriggerWithAnUndefinedConnectionDoesNotStart(t *testing.T) {
	_, ctx := mqttConnEnv(t)
	n, err := NewMqttTrigger(map[string]any{"connection": "elsewhere", "topic": "t"})
	require.NoError(t, err)
	err = n.(*MqttTrigger).Start(ctx, make(chan *types.Message, 1))
	assert.ErrorContains(t, err, `"elsewhere" is not defined`)
}

// A publisher with a connection publishes on that connection's broker.
func TestMqttActionUsesItsConnection(t *testing.T) {
	b, ctx := mqttConnEnv(t)
	n, err := NewMqttAction(map[string]any{"connection": "home", "topic": "lights/set", "payload": "on"})
	require.NoError(t, err)
	a := n.(*MqttAction)
	t.Cleanup(func() { _ = a.Close() })
	_, err = a.Process(ctx, types.NewMessage(nil, types.DataTypeAny))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, _, pubs, _ := b.Snapshot()
		return len(pubs) == 1 && pubs[0] == "lights/set=on"
	}, 5*time.Second, 20*time.Millisecond)

	n, err = NewMqttAction(map[string]any{"connection": "elsewhere", "topic": "t"})
	require.NoError(t, err)
	_, err = n.(*MqttAction).Process(ctx, types.NewMessage(nil, types.DataTypeAny))
	assert.ErrorContains(t, err, `"elsewhere" is not defined`)
}
