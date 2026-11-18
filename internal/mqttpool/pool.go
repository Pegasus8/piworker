// Package mqttpool shares MQTT broker connections between the nodes and
// services of the process (ADR-0025).
package mqttpool

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
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Nodes share their broker connections (ADR-0025). Nodes on one broker
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

// Bounded timeouts suitable for a flaky Wi-Fi link.
const (
	// ConnectTimeout bounds a connection attempt and an operation's wait.
	ConnectTimeout = 10 * time.Second
	// RetryInterval is the default wait between connection attempts.
	RetryInterval        = 10 * time.Second
	keepAlive            = 30 * time.Second
	pingTimeout          = 10 * time.Second
	writeTimeout         = 10 * time.Second
	maxReconnectInterval = time.Minute
)

// DefaultClientID returns a client id unique to one connection. MQTT
// brokers disconnect the older session when a second client connects with
// the same id, so a shared constant makes two connections to one broker
// knock each other off in a reconnect loop.
func DefaultClientID(prefix string) string {
	return prefix + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
}

// ClientOptions builds client options with bounded timeouts.
func ClientOptions(broker, clientID, username, password string) *mqtt.ClientOptions {
	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(clientID).
		SetConnectTimeout(ConnectTimeout).
		SetKeepAlive(keepAlive).
		SetPingTimeout(pingTimeout).
		SetWriteTimeout(writeTimeout).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(maxReconnectInterval)
	if username != "" {
		opts.SetUsername(username)
	}
	if password != "" {
		opts.SetPassword(password)
	}
	return opts
}

// Endpoint is what a node connects with: the broker, the resolved
// credentials, its own client id ("" for a pooled connection) and the
// sandbox it runs in, if any (nodes in a sandbox, ADR-0020, never share a
// connection with nodes outside it).
type Endpoint struct {
	Broker, Username, Password, ClientID string
	Sandbox                              *node.Sandbox
}

// connKey identifies the connections an endpoint may use.
type connKey struct {
	broker, username, passwordSum, clientID string
	sandbox                                 *node.Sandbox
}

func (e Endpoint) key() connKey {
	sum := sha256.Sum256([]byte(e.Password))
	return connKey{broker: e.Broker, username: e.Username, passwordSum: hex.EncodeToString(sum[:]),
		clientID: e.ClientID, sandbox: e.Sandbox}
}

var (
	// poolMu guards pool and every pooled connection's refs, subs,
	// connected flag and up channel.
	poolMu sync.Mutex
	pool   = map[connKey][]*Conn{}
)

// Conn is one broker connection shared by the nodes holding a reference.
type Conn struct {
	key    connKey
	client mqtt.Client
	// opMu orders the broker (un)subscriptions: each one sends what the
	// state says when it runs (syncFilter), so an UNSUBSCRIBE of a filter a
	// node left never lands after the SUBSCRIBE of a node that joined it.
	opMu sync.Mutex

	refs      int
	subs      map[string]*subscription // by filter
	connected bool                     // between OnConnect and ConnectionLost
	up        chan struct{}            // closed while connected
}

// subscription is one filter subscribed on a connection and the nodes receiving it.
type subscription struct {
	qos      byte
	handlers map[*Sub]func(mqtt.Message)
}

// Sub is a node's subscription; release ends it.
type Sub struct {
	conn   *Conn
	filter string
	once   sync.Once
}

// Subscribe registers handler for filter on a connection for ep and
// returns the subscription; fresh tells whether the connection was opened
// for it (the caller may wait briefly for it with WaitUp). The broker
// subscription is made now when connected, else on connect.
func Subscribe(ep Endpoint, filter string, qos byte, handler func(mqtt.Message), retry time.Duration) (h *Sub, fresh bool) {
	key := ep.key()
	poolMu.Lock()
	var conn *Conn
	for _, c := range pool[key] {
		if key.clientID != "" || c.accepts(filter, qos) {
			conn = c
			break
		}
	}
	if conn == nil {
		conn = newConn(ep, key, retry)
		pool[key] = append(pool[key], conn)
		fresh = true
	}
	conn.refs++
	h = &Sub{conn: conn, filter: filter}
	sub := conn.subs[filter]
	subscribeNow := false
	if sub == nil {
		sub = &subscription{qos: qos, handlers: map[*Sub]func(mqtt.Message){}}
		conn.subs[filter] = sub
		// Connected: OnConnect has already subscribed what was there, so
		// this filter is ours to subscribe. Otherwise OnConnect will.
		subscribeNow = conn.connected
	}
	sub.handlers[h] = handler
	poolMu.Unlock()

	if fresh {
		conn.client.Connect()
	} else if subscribeNow {
		go conn.syncFilter(filter)
	}
	return h, fresh
}

// Acquire returns a connection for ep to publish on, holding a reference
// until Conn.Release; fresh as for Subscribe.
func Acquire(ep Endpoint, retry time.Duration) (conn *Conn, fresh bool) {
	key := ep.key()
	poolMu.Lock()
	if conns := pool[key]; len(conns) > 0 {
		conn = conns[0]
	} else {
		conn = newConn(ep, key, retry)
		pool[key] = []*Conn{conn}
		fresh = true
	}
	conn.refs++
	poolMu.Unlock()
	if fresh {
		conn.client.Connect()
	}
	return conn, fresh
}

// Release ends the subscription: the filter is unsubscribed when no other
// node receives it, and the connection closed when no node holds it. It is
// idempotent.
func (h *Sub) Release() {
	h.once.Do(func() {
		c := h.conn
		poolMu.Lock()
		unsubscribe := false // nobody receives the filter any more
		if sub := c.subs[h.filter]; sub != nil {
			delete(sub.handlers, h)
			if len(sub.handlers) == 0 {
				delete(c.subs, h.filter)
				unsubscribe = true
			}
		}
		poolMu.Unlock()
		if unsubscribe {
			c.syncFilter(h.filter)
		}
		release(c)
	})
}

// release drops a reference to conn and disconnects it after the last.
func release(c *Conn) {
	poolMu.Lock()
	c.refs--
	last := c.refs == 0
	if last {
		conns := pool[c.key]
		for i, other := range conns {
			if other == c {
				conns = append(conns[:i:i], conns[i+1:]...)
				break
			}
		}
		if len(conns) == 0 {
			delete(pool, c.key)
		} else {
			pool[c.key] = conns
		}
	}
	poolMu.Unlock()
	if last {
		// Also aborts a pending connect retry.
		c.client.Disconnect(250)
	}
}

func newConn(ep Endpoint, key connKey, retry time.Duration) *Conn {
	c := &Conn{key: key, subs: map[string]*subscription{}, up: make(chan struct{})}
	id := ep.ClientID
	if id == "" {
		id = DefaultClientID("piworker")
	}
	opts := ClientOptions(ep.Broker, id, ep.Username, ep.Password).
		SetConnectRetry(true).
		SetConnectRetryInterval(retry).
		SetOnConnectHandler(func(mqtt.Client) { c.onConnect() }).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			poolMu.Lock()
			if c.connected {
				c.connected = false
				c.up = make(chan struct{})
			}
			poolMu.Unlock()
			log.Warn().Err(err).Str("broker", ep.Broker).Msg("mqtt connection lost; reconnecting")
		})
	c.client = mqtt.NewClient(opts)
	return c
}

// onConnect subscribes every filter again: with a clean session the broker
// forgets subscriptions whenever the connection drops. It runs in its own
// goroutine, so waiting here is safe.
func (c *Conn) onConnect() {
	poolMu.Lock()
	filters := make([]string, 0, len(c.subs))
	for f := range c.subs {
		filters = append(filters, f)
	}
	if !c.connected {
		c.connected = true
		close(c.up)
	}
	poolMu.Unlock()
	for _, f := range filters {
		c.syncFilter(f)
	}
}

// syncFilter makes the broker's subscription to filter what the state says
// now: subscribed while a node receives it, unsubscribed otherwise. It does
// nothing while disconnected (onConnect subscribes on connect).
func (c *Conn) syncFilter(filter string) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	poolMu.Lock()
	sub, connected := c.subs[filter], c.connected
	var qos byte
	if sub != nil {
		qos = sub.qos
	}
	poolMu.Unlock()
	if !connected {
		return
	}
	if sub == nil {
		c.client.Unsubscribe(filter).WaitTimeout(time.Second)
		return
	}
	tok := c.client.Subscribe(filter, qos, c.dispatch(filter))
	if !tok.WaitTimeout(ConnectTimeout) {
		log.Warn().Str("topic", filter).Msg("mqtt subscribe timed out")
		return
	}
	if err := tok.Error(); err != nil {
		log.Error().Err(err).Str("topic", filter).Msg("mqtt subscribe failed")
	}
}

// dispatch hands a message on filter to every node receiving it now.
func (c *Conn) dispatch(filter string) mqtt.MessageHandler {
	return func(_ mqtt.Client, m mqtt.Message) {
		poolMu.Lock()
		var handlers []func(mqtt.Message)
		if sub := c.subs[filter]; sub != nil {
			for _, h := range sub.handlers {
				handlers = append(handlers, h)
			}
		}
		poolMu.Unlock()
		for _, h := range handlers {
			h(m)
		}
	}
}

// accepts tells whether filter can join this pooled connection: it is
// already subscribed with the same QoS, or overlaps none of its filters.
func (c *Conn) accepts(filter string, qos byte) bool {
	if sub, ok := c.subs[filter]; ok {
		return sub.qos == qos
	}
	for f := range c.subs {
		if FiltersOverlap(f, filter) {
			return false
		}
	}
	return true
}

// WaitUp waits until the connection is up, ctx is done or timeout passes.
func (c *Conn) WaitUp(ctx context.Context, timeout time.Duration) error {
	poolMu.Lock()
	up := c.up
	poolMu.Unlock()
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

// FiltersOverlap reports whether some topic matches both filters.
// Wildcards at the first level never match topics starting with "$".
func FiltersOverlap(a, b string) bool {
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

// Client is the connection's MQTT client (to publish on).
func (c *Conn) Client() mqtt.Client { return c.client }

// Release drops the caller's reference to the connection (Acquire).
func (c *Conn) Release() { release(c) }

// Conn is the subscription's connection.
func (h *Sub) Conn() *Conn { return h.conn }

// Connected reports whether the connection is up.
func (c *Conn) Connected() bool {
	poolMu.Lock()
	defer poolMu.Unlock()
	return c.connected
}

// Settle waits for the SUBSCRIBE or UNSUBSCRIBE in progress, if any. Tests
// use it before publishing to the broker.
func (c *Conn) Settle() {
	c.opMu.Lock()
	c.opMu.Unlock() //nolint:staticcheck // only waits for the operation in progress
}
