package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog/log"
)

// MQTT nodes share their broker connections (ADR-0025). Nodes on one broker
// with the same (resolved) credentials and no client id of their own use a
// pooled connection, whatever flow they are in; a home with one trigger per
// Zigbee device then holds a few connections instead of one per node. A
// node with its own client id gets the connection of that id (brokers use
// it for ACLs and persistent sessions; two connections cannot share an id).
//
// Brokers deliver a message once per matching subscription of a connection
// (Mosquitto's default), and the client then hands each copy to every
// matching filter's handler, so subscriptions on one pooled connection never
// overlap: a filter that overlaps one already there goes to another
// connection. Nodes on the very same filter and QoS share its subscription.

// mqttEndpoint is what a node connects with: the broker, the resolved
// credentials, its own client id ("" for a pooled connection) and the
// sandbox it runs in, if any.
type mqttEndpoint struct {
	broker, username, password, clientID string
	sandbox                              *node.Sandbox
}

// mqttEndpointFor is ep in the sandbox of ctx, if any: nodes in a sandbox
// (ADR-0020) never share a connection with nodes outside it.
func mqttEndpointFor(ctx context.Context, broker, username, password, clientID string) mqttEndpoint {
	return mqttEndpoint{broker: broker, username: expandSecrets(ctx, username), password: expandSecrets(ctx, password),
		clientID: clientID, sandbox: node.SandboxFrom(ctx)}
}

// mqttConnKey identifies the connections an endpoint may use.
type mqttConnKey struct {
	broker, username, passwordSum, clientID string
	sandbox                                 *node.Sandbox
}

func (e mqttEndpoint) key() mqttConnKey {
	sum := sha256.Sum256([]byte(e.password))
	return mqttConnKey{broker: e.broker, username: e.username, passwordSum: hex.EncodeToString(sum[:]),
		clientID: e.clientID, sandbox: e.sandbox}
}

var (
	// mqttPoolMu guards mqttPool and every pooled connection's refs, subs,
	// connected flag and up channel.
	mqttPoolMu sync.Mutex
	mqttPool   = map[mqttConnKey][]*mqttConn{}
)

// mqttConn is one broker connection shared by the nodes holding a reference.
type mqttConn struct {
	key    mqttConnKey
	client mqtt.Client
	// opMu orders the broker (un)subscriptions: each one sends what the
	// state says when it runs (syncFilter), so an UNSUBSCRIBE of a filter a
	// node left never lands after the SUBSCRIBE of a node that joined it.
	opMu sync.Mutex

	refs      int
	subs      map[string]*mqttSub // by filter
	connected bool                // between OnConnect and ConnectionLost
	up        chan struct{}       // closed while connected
}

// mqttSub is one filter subscribed on a connection and the nodes receiving it.
type mqttSub struct {
	qos      byte
	handlers map[*mqttSubHandle]func(mqtt.Message)
}

// mqttSubHandle is a node's subscription; release ends it.
type mqttSubHandle struct {
	conn   *mqttConn
	filter string
	once   sync.Once
}

// mqttSubscribe registers handler for filter on a connection for ep and
// returns the subscription; fresh tells whether the connection was opened
// for it (the caller may wait briefly for it with waitUp). The broker
// subscription is made now when connected, else on connect.
func mqttSubscribe(ep mqttEndpoint, filter string, qos byte, handler func(mqtt.Message), retry time.Duration) (h *mqttSubHandle, fresh bool) {
	key := ep.key()
	mqttPoolMu.Lock()
	var conn *mqttConn
	for _, c := range mqttPool[key] {
		if key.clientID != "" || c.accepts(filter, qos) {
			conn = c
			break
		}
	}
	if conn == nil {
		conn = newMqttConn(ep, key, retry)
		mqttPool[key] = append(mqttPool[key], conn)
		fresh = true
	}
	conn.refs++
	h = &mqttSubHandle{conn: conn, filter: filter}
	sub := conn.subs[filter]
	subscribeNow := false
	if sub == nil {
		sub = &mqttSub{qos: qos, handlers: map[*mqttSubHandle]func(mqtt.Message){}}
		conn.subs[filter] = sub
		// Connected: OnConnect has already subscribed what was there, so
		// this filter is ours to subscribe. Otherwise OnConnect will.
		subscribeNow = conn.connected
	}
	sub.handlers[h] = handler
	mqttPoolMu.Unlock()

	if fresh {
		conn.client.Connect()
	} else if subscribeNow {
		go conn.syncFilter(filter)
	}
	return h, fresh
}

// mqttAcquire returns a connection for ep to publish on, holding a reference
// until mqttRelease; fresh as for mqttSubscribe.
func mqttAcquire(ep mqttEndpoint, retry time.Duration) (conn *mqttConn, fresh bool) {
	key := ep.key()
	mqttPoolMu.Lock()
	if conns := mqttPool[key]; len(conns) > 0 {
		conn = conns[0]
	} else {
		conn = newMqttConn(ep, key, retry)
		mqttPool[key] = []*mqttConn{conn}
		fresh = true
	}
	conn.refs++
	mqttPoolMu.Unlock()
	if fresh {
		conn.client.Connect()
	}
	return conn, fresh
}

// release ends the subscription: the filter is unsubscribed when no other
// node receives it, and the connection closed when no node holds it. It is
// idempotent.
func (h *mqttSubHandle) release() {
	h.once.Do(func() {
		c := h.conn
		mqttPoolMu.Lock()
		unsubscribe := false // nobody receives the filter any more
		if sub := c.subs[h.filter]; sub != nil {
			delete(sub.handlers, h)
			if len(sub.handlers) == 0 {
				delete(c.subs, h.filter)
				unsubscribe = true
			}
		}
		mqttPoolMu.Unlock()
		if unsubscribe {
			c.syncFilter(h.filter)
		}
		mqttRelease(c)
	})
}

// mqttRelease drops a reference to conn and disconnects it after the last.
func mqttRelease(c *mqttConn) {
	mqttPoolMu.Lock()
	c.refs--
	last := c.refs == 0
	if last {
		conns := mqttPool[c.key]
		for i, other := range conns {
			if other == c {
				conns = append(conns[:i:i], conns[i+1:]...)
				break
			}
		}
		if len(conns) == 0 {
			delete(mqttPool, c.key)
		} else {
			mqttPool[c.key] = conns
		}
	}
	mqttPoolMu.Unlock()
	if last {
		// Also aborts a pending connect retry.
		c.client.Disconnect(250)
	}
}

func newMqttConn(ep mqttEndpoint, key mqttConnKey, retry time.Duration) *mqttConn {
	c := &mqttConn{key: key, subs: map[string]*mqttSub{}, up: make(chan struct{})}
	id := ep.clientID
	if id == "" {
		id = defaultMqttClientID("piworker")
	}
	opts := newMqttClientOptions(ep.broker, id, ep.username, ep.password).
		SetConnectRetry(true).
		SetConnectRetryInterval(retry).
		SetOnConnectHandler(func(mqtt.Client) { c.onConnect() }).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			mqttPoolMu.Lock()
			if c.connected {
				c.connected = false
				c.up = make(chan struct{})
			}
			mqttPoolMu.Unlock()
			log.Warn().Err(err).Str("broker", ep.broker).Msg("mqtt connection lost; reconnecting")
		})
	c.client = mqtt.NewClient(opts)
	return c
}

// onConnect subscribes every filter again: with a clean session the broker
// forgets subscriptions whenever the connection drops. It runs in its own
// goroutine, so waiting here is safe.
func (c *mqttConn) onConnect() {
	mqttPoolMu.Lock()
	filters := make([]string, 0, len(c.subs))
	for f := range c.subs {
		filters = append(filters, f)
	}
	if !c.connected {
		c.connected = true
		close(c.up)
	}
	mqttPoolMu.Unlock()
	for _, f := range filters {
		c.syncFilter(f)
	}
}

// syncFilter makes the broker's subscription to filter what the state says
// now: subscribed while a node receives it, unsubscribed otherwise. It does
// nothing while disconnected (onConnect subscribes on connect).
func (c *mqttConn) syncFilter(filter string) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	mqttPoolMu.Lock()
	sub, connected := c.subs[filter], c.connected
	var qos byte
	if sub != nil {
		qos = sub.qos
	}
	mqttPoolMu.Unlock()
	if !connected {
		return
	}
	if sub == nil {
		c.client.Unsubscribe(filter).WaitTimeout(time.Second)
		return
	}
	tok := c.client.Subscribe(filter, qos, c.dispatch(filter))
	if !tok.WaitTimeout(mqttConnectTimeout) {
		log.Warn().Str("topic", filter).Msg("mqtt subscribe timed out")
		return
	}
	if err := tok.Error(); err != nil {
		log.Error().Err(err).Str("topic", filter).Msg("mqtt subscribe failed")
	}
}

// dispatch hands a message on filter to every node receiving it now.
func (c *mqttConn) dispatch(filter string) mqtt.MessageHandler {
	return func(_ mqtt.Client, m mqtt.Message) {
		mqttPoolMu.Lock()
		var handlers []func(mqtt.Message)
		if sub := c.subs[filter]; sub != nil {
			for _, h := range sub.handlers {
				handlers = append(handlers, h)
			}
		}
		mqttPoolMu.Unlock()
		for _, h := range handlers {
			h(m)
		}
	}
}

// accepts tells whether filter can join this pooled connection: it is
// already subscribed with the same QoS, or overlaps none of its filters.
func (c *mqttConn) accepts(filter string, qos byte) bool {
	if sub, ok := c.subs[filter]; ok {
		return sub.qos == qos
	}
	for f := range c.subs {
		if mqttFiltersOverlap(f, filter) {
			return false
		}
	}
	return true
}

// waitUp waits until the connection is up, ctx is done or timeout passes.
func (c *mqttConn) waitUp(ctx context.Context, timeout time.Duration) error {
	mqttPoolMu.Lock()
	up := c.up
	mqttPoolMu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-up:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("not connected after %s", timeout)
	}
}

// mqttFiltersOverlap reports whether some topic matches both filters.
// Wildcards at the first level never match topics starting with "$".
func mqttFiltersOverlap(a, b string) bool {
	la, lb := strings.Split(a, "/"), strings.Split(b, "/")
	if strings.HasPrefix(la[0], "$") != strings.HasPrefix(lb[0], "$") {
		return false // a $ topic matches only a filter naming its first level
	}
	for i := 0; ; i++ {
		switch {
		case i == len(la) && i == len(lb):
			return true
		case i == len(la):
			return lb[i] == "#" // "#" also matches its parent level
		case i == len(lb):
			return la[i] == "#"
		case la[i] == "#" || lb[i] == "#":
			return true
		case la[i] != "+" && lb[i] != "+" && la[i] != lb[i]:
			return false
		}
	}
}
