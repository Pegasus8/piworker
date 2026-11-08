package builtin

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/types"
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

func startMqttTrigger(t *testing.T, tr *MqttTrigger) chan *types.Message {
	t.Helper()
	out := make(chan *types.Message, 16)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, tr.Start(ctx, out))
	t.Cleanup(func() { _ = tr.Stop() })
	return out
}

func subscribeCount(b *fakeBroker) int {
	_, subs, _, _ := b.snapshot()
	return len(subs)
}

func TestMqttTriggerResubscribesAfterReconnect(t *testing.T) {
	b := newFakeBroker(t)
	tr := newMqttTrigger(t, map[string]interface{}{"broker": b.url(), "topic": "sensors/#"})
	out := startMqttTrigger(t, tr)

	require.Eventually(t, func() bool { return subscribeCount(b) == 1 },
		5*time.Second, 20*time.Millisecond)

	// Network flap: the broker forgets the (clean) session, so the client
	// must subscribe again once it reconnects.
	b.dropAll()
	require.Eventually(t, func() bool { return subscribeCount(b) >= 2 },
		10*time.Second, 50*time.Millisecond, "subscription must be restored after reconnect")

	b.publishToAll("sensors/temp", `{"v":1}`)
	select {
	case msg := <-out:
		assert.Equal(t, "sensors/temp", msg.Payload.(map[string]interface{})["topic"])
	case <-time.After(5 * time.Second):
		t.Fatal("no message received after reconnect")
	}
}

func TestMqttTriggerStopDisconnects(t *testing.T) {
	b := newFakeBroker(t)
	tr := newMqttTrigger(t, map[string]interface{}{"broker": b.url(), "topic": "t"})
	startMqttTrigger(t, tr)
	require.Eventually(t, func() bool { return subscribeCount(b) == 1 },
		5*time.Second, 20*time.Millisecond)

	require.NoError(t, tr.Stop())
	require.Eventually(t, func() bool {
		_, _, _, d := b.snapshot()
		return d == 1 && b.connCount() == 0
	}, 5*time.Second, 20*time.Millisecond)
}

func TestMqttTriggerDefaultClientIDsAreUnique(t *testing.T) {
	a := newMqttTrigger(t, map[string]interface{}{"broker": "tcp://x:1883", "topic": "t"})
	b := newMqttTrigger(t, map[string]interface{}{"broker": "tcp://x:1883", "topic": "t"})
	assert.True(t, strings.HasPrefix(a.clientID, "piworker-sub-"), a.clientID)
	assert.NotEqual(t, a.clientID, b.clientID,
		"two triggers on one broker with the same client id would kick each other off forever")
	c := newMqttTrigger(t, map[string]interface{}{"broker": "tcp://x:1883", "topic": "t", "clientId": "mine"})
	assert.Equal(t, "mine", c.clientID)

	// The editor pre-fills schema defaults, and older flows stored the old
	// constant default; both must still get a unique id.
	for _, v := range []string{"", "piworker-sub"} {
		d := newMqttTrigger(t, map[string]interface{}{"broker": "tcp://x:1883", "topic": "t", "clientId": v})
		assert.True(t, strings.HasPrefix(d.clientID, "piworker-sub-"), "clientId %q -> %q", v, d.clientID)
	}
	assert.Equal(t, "", mqttTriggerConfigSchema().Properties["clientId"].Default)
}

// A Pi often boots before Wi-Fi is up: Start must not fail the deploy because
// the broker is unreachable; it keeps retrying and subscribes once connected.
func TestMqttTriggerStartsBeforeBrokerIsReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	tr := newMqttTrigger(t, map[string]interface{}{"broker": "tcp://" + addr, "topic": "t"})
	tr.initialConnectWait = 200 * time.Millisecond
	tr.connectRetryInterval = 100 * time.Millisecond
	startMqttTrigger(t, tr)

	ln2, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	b := &fakeBroker{t: t, ln: ln2, conns: map[net.Conn]bool{}}
	go b.serve()
	t.Cleanup(b.close)

	require.Eventually(t, func() bool { return subscribeCount(b) == 1 },
		10*time.Second, 50*time.Millisecond)
}
