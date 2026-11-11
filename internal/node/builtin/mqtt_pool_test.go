package builtin

import (
	"context"
	"fmt"
	"github.com/Pegasus8/piworker/internal/mqtttest"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// receiveOne waits for one message on out and returns its MQTT topic.
func receiveOne(t *testing.T, out chan *types.Message, what string) string {
	t.Helper()
	select {
	case msg := <-out:
		return msg.Payload.(map[string]interface{})["topic"].(string)
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: no message", what)
		return ""
	}
}

// receiveNone checks that nothing (more) arrives on out for a while.
func receiveNone(t *testing.T, out chan *types.Message, what string) {
	t.Helper()
	select {
	case msg := <-out:
		t.Fatalf("%s: unexpected message %v", what, msg.Payload)
	case <-time.After(300 * time.Millisecond):
	}
}

func waitSubscribes(t *testing.T, b *mqtttest.Broker, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return subscribeCount(b) == n }, 5*time.Second, 20*time.Millisecond)
}

// Triggers on one broker with the same credentials and no client id of
// their own share one connection; each still gets only its own topic.
func TestMqttTriggersShareOneConnection(t *testing.T) {
	b := mqtttest.New(t)
	outA := startMqttTrigger(t, newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/kitchen"}))
	outB := startMqttTrigger(t, newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/plug"}))
	outC := startMqttTrigger(t, newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/plug/availability"}))
	waitSubscribes(t, b, 3)
	assert.Equal(t, 1, b.ConnCount(), "one connection for the three nodes")

	b.Route("z/kitchen", `{"temperature":21.5}`)
	assert.Equal(t, "z/kitchen", receiveOne(t, outA, "kitchen"))
	b.Route("z/plug/availability", `{"state":"online"}`)
	assert.Equal(t, "z/plug/availability", receiveOne(t, outC, "availability"))
	receiveNone(t, outA, "kitchen")
	receiveNone(t, outB, "plug")
}

// Brokers send one copy per matching subscription of a connection, so
// filters that overlap must not share one: each node gets each message once.
func TestMqttOverlappingFiltersUseSeparateConnections(t *testing.T) {
	b := mqtttest.New(t)
	outAll := startMqttTrigger(t, newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/#"}))
	outOne := startMqttTrigger(t, newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/+/set"}))
	outOther := startMqttTrigger(t, newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "other"}))
	waitSubscribes(t, b, 3)
	assert.Equal(t, 2, b.ConnCount(), "z/# and z/+/set overlap; other joins one of them")
	ids, _, _, _ := b.Snapshot()
	require.Len(t, ids, 2)
	assert.NotEqual(t, ids[0], ids[1], "two connections with one id would knock each other off")
	assert.True(t, strings.HasPrefix(ids[0], "piworker-"), ids[0])

	b.Route("z/lamp/set", `{"state":"ON"}`)
	assert.Equal(t, "z/lamp/set", receiveOne(t, outAll, "z/#"))
	assert.Equal(t, "z/lamp/set", receiveOne(t, outOne, "z/+/set"))
	receiveNone(t, outAll, "z/# duplicate")
	receiveNone(t, outOne, "z/+/set duplicate")
	receiveNone(t, outOther, "other")
}

// Two nodes on the same filter share its subscription.
func TestMqttSameFilterSharesOneSubscription(t *testing.T) {
	b := mqtttest.New(t)
	outA := startMqttTrigger(t, newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/door"}))
	outB := startMqttTrigger(t, newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/door"}))
	waitSubscribes(t, b, 1)
	assert.Equal(t, 1, b.ConnCount())

	b.Route("z/door", `{"contact":false}`)
	assert.Equal(t, "z/door", receiveOne(t, outA, "A"))
	assert.Equal(t, "z/door", receiveOne(t, outB, "B"))
	receiveNone(t, outA, "A duplicate")
	receiveNone(t, outB, "B duplicate")
}

// Stopping one node unsubscribes its filter and keeps the connection for
// the others; the last one disconnects.
func TestMqttSharedConnectionOutlivesOneNode(t *testing.T) {
	b := mqtttest.New(t)
	a := newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/a"})
	c := newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/c"})
	outA := startMqttTrigger(t, a)
	outC := startMqttTrigger(t, c)
	waitSubscribes(t, b, 2)

	require.NoError(t, a.Stop())
	require.Eventually(t, func() bool { return len(b.Unsubscribed()) == 1 }, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, []string{"z/a"}, b.Unsubscribed())
	assert.Equal(t, 1, b.ConnCount(), "c still uses the connection")
	_, _, _, d := b.Snapshot()
	assert.Zero(t, d)

	b.Route("z/c", `1`)
	assert.Equal(t, "z/c", receiveOne(t, outC, "c"))
	b.Route("z/a", `1`)
	receiveNone(t, outA, "a after Stop")

	require.NoError(t, c.Stop())
	require.Eventually(t, func() bool {
		_, _, _, d := b.Snapshot()
		return d == 1 && b.ConnCount() == 0
	}, 5*time.Second, 20*time.Millisecond, "the last node disconnects")
	require.NoError(t, c.Stop(), "Stop is idempotent")
}

// After a network flap the shared connection subscribes every filter again.
func TestMqttSharedConnectionResubscribesAll(t *testing.T) {
	b := mqtttest.New(t)
	outA := startMqttTrigger(t, newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/a"}))
	outB := startMqttTrigger(t, newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/b"}))
	waitSubscribes(t, b, 2)

	b.DropAll()
	require.Eventually(t, func() bool { return subscribeCount(b) >= 4 && b.ConnCount() == 1 },
		10*time.Second, 50*time.Millisecond, "both filters restored on the new connection")

	b.Route("z/a", `1`)
	b.Route("z/b", `1`)
	assert.Equal(t, "z/a", receiveOne(t, outA, "a"))
	assert.Equal(t, "z/b", receiveOne(t, outB, "b"))
}

// Publishers use the same shared connection as subscribers.
func TestMqttActionSharesTheTriggersConnection(t *testing.T) {
	b := mqtttest.New(t)
	tr := newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/plug"})
	startMqttTrigger(t, tr)
	waitSubscribes(t, b, 1)

	a := newMqttAction(t, map[string]interface{}{"broker": b.URL(), "topic": "z/plug/set", "payload": `{"state":"ON"}`, "qos": 1})
	_, err := a.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
	require.NoError(t, err)
	_, _, pubs, _ := b.Snapshot()
	assert.Equal(t, []string{`z/plug/set={"state":"ON"}`}, pubs)
	assert.Equal(t, 1, b.ConnCount())

	require.NoError(t, a.Close())
	assert.Equal(t, 1, b.ConnCount(), "the trigger keeps the connection")
	require.NoError(t, tr.Stop())
	require.Eventually(t, func() bool { return b.ConnCount() == 0 }, 5*time.Second, 20*time.Millisecond)
}

// A node with its own client id (broker ACLs, persistent sessions) keeps a
// connection under that id; different credentials never share.
func TestMqttOwnClientIDAndCredentialsGetTheirOwnConnection(t *testing.T) {
	b := mqtttest.New(t)
	startMqttTrigger(t, newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/a"}))
	startMqttTrigger(t, newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/b", "clientId": "mine"}))
	startMqttTrigger(t, newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/c", "username": "other"}))
	waitSubscribes(t, b, 3)
	assert.Equal(t, 3, b.ConnCount())
	ids, _, _, _ := b.Snapshot()
	assert.Contains(t, ids, "mine")
}

// A node running in a sandbox (dry-run, node test: ADR-0020) never shares a
// connection with the nodes outside it, even with the same credentials.
func TestMqttSandboxNeverSharesAConnection(t *testing.T) {
	b := mqtttest.New(t)
	startMqttTrigger(t, newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/a"}))
	waitSubscribes(t, b, 1)

	a := newMqttAction(t, map[string]interface{}{"broker": b.URL(), "topic": "z/a/set", "payload": "x"})
	t.Cleanup(func() { _ = a.Close() })
	ctx := node.WithSandbox(context.Background(), node.NewSandbox(1<<20, 1<<20))
	_, err := a.Process(ctx, types.NewMessage("x", types.DataTypeString))
	require.NoError(t, err)
	assert.Equal(t, 2, b.ConnCount())
}

// A node leaving a filter while another joins it on the same connection
// leaves the broker subscribed: the UNSUBSCRIBE never lands after the
// SUBSCRIBE.
func TestMqttLeaveAndJoinTheSameFilterConcurrently(t *testing.T) {
	b := mqtttest.New(t)
	startMqttTrigger(t, newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "keep/alive"}))
	waitSubscribes(t, b, 1)
	leaving := newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/a"})
	startMqttTrigger(t, leaving)
	waitSubscribes(t, b, 2)

	for i := range 10 {
		joining := newMqttTrigger(t, map[string]interface{}{"broker": b.URL(), "topic": "z/a"})
		var out chan *types.Message
		done := make(chan struct{})
		go func() { defer close(done); _ = leaving.Stop() }()
		out = startMqttTrigger(t, joining)
		<-done
		// Every SUBSCRIBE and UNSUBSCRIBE has been answered once the
		// connection's operations are done.
		c := joining.sub.Conn()
		c.Settle()
		require.Eventually(t, c.Connected, 5*time.Second, 10*time.Millisecond)
		time.Sleep(50 * time.Millisecond) // a SUBSCRIBE goroutine still on its way
		c.Settle()
		b.Route("z/a", `1`)
		assert.Equal(t, "z/a", receiveOne(t, out, fmt.Sprintf("round %d", i)))
		leaving = joining
	}
	assert.Equal(t, 1, b.ConnCount())
}
