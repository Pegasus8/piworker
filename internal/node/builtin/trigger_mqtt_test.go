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

func subscribeCount(b *mqtttest.Broker) int {
	_, subs, _, _ := b.Snapshot()
	return len(subs)
}

func TestMqttTriggerResubscribesAfterReconnect(t *testing.T) {
	b := mqtttest.New(t)
	tr := newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "sensors/#"})
	out := startMqttTrigger(t, tr)

	require.Eventually(t, func() bool { return subscribeCount(b) == 1 },
		5*time.Second, 20*time.Millisecond)

	// Network flap: the broker forgets the (clean) session, so the client
	// must subscribe again once it reconnects.
	b.DropAll()
	require.Eventually(t, func() bool { return subscribeCount(b) >= 2 },
		10*time.Second, 50*time.Millisecond, "subscription must be restored after reconnect")

	b.PublishToAll("sensors/temp", `{"v":1}`)
	select {
	case msg := <-out:
		assert.Equal(t, "sensors/temp", msg.Payload.(map[string]interface{})["topic"])
	case <-time.After(5 * time.Second):
		t.Fatal("no message received after reconnect")
	}
}

func TestMqttTriggerStopDisconnects(t *testing.T) {
	b := mqtttest.New(t)
	tr := newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "t"})
	startMqttTrigger(t, tr)
	require.Eventually(t, func() bool { return subscribeCount(b) == 1 },
		5*time.Second, 20*time.Millisecond)

	require.NoError(t, tr.Stop())
	require.Eventually(t, func() bool {
		_, _, _, d := b.Snapshot()
		return d == 1 && b.ConnCount() == 0
	}, 5*time.Second, 20*time.Millisecond)
}

// Without a client id of its own (empty, pre-filled by the editor, or the
// constant older flows stored) the trigger shares a pooled connection,
// which gets a unique id (ADR-0025).
func TestMqttTriggerClientID(t *testing.T) {
	for _, v := range []string{"", "piworker-sub"} {
		d := newMqttTrigger(t, map[string]interface{}{"broker": "tcp://x:1883", "topic": "t", "clientId": v})
		assert.Empty(t, d.clientID, "clientId %q", v)
	}
	c := newMqttTrigger(t, map[string]interface{}{"broker": "tcp://x:1883", "topic": "t", "clientId": "mine"})
	assert.Equal(t, "mine", c.clientID)
	assert.Equal(t, "", mqttTriggerConfigSchema().Properties["clientId"].Default)
}

// The runtime starts triggers synchronously and fails the deploy (and boot
// restore) on a Start error, so an unreachable broker must neither fail nor
// noticeably delay Start, and Stop must still exit cleanly.
func TestMqttTriggerUnreachableBrokerStartReturnsNilQuickly(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	tr := newMqttTrigger(t, map[string]interface{}{"broker": "tcp://" + addr, "topic": "t"})
	out := make(chan *types.Message, 1)
	start := time.Now()
	require.NoError(t, tr.Start(context.Background(), out))
	assert.Less(t, time.Since(start), 3*time.Second, "Start must not block the deploy waiting for the broker")

	done := make(chan error, 1)
	go func() { done <- tr.Stop() }()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Stop hung while the client was retrying")
	}
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
	b := mqtttest.Serve(t, ln2)

	require.Eventually(t, func() bool { return subscribeCount(b) == 1 },
		10*time.Second, 50*time.Millisecond)
}
