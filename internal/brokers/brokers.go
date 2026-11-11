// Package brokers holds the named MQTT broker connections (ADR-0026): a
// broker URL, credentials and a Zigbee2MQTT base topic, defined once in the
// editor and referenced by name from the Zigbee nodes. Like the secret store
// it keeps the connections in memory, writes through to a Persister (the
// SQLite store) and is resolved through the node's context, so a sandboxed
// run (dry-run, node test) sees an empty store.
package brokers

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// DefaultBaseTopic is Zigbee2MQTT's default base topic.
const DefaultBaseTopic = "zigbee2mqtt"

// MaxNameLength bounds a connection's name.
const MaxNameLength = 64

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidName reports whether name is a legal connection name: 1 to
// MaxNameLength letters, digits, '_' or '-'.
func ValidName(name string) bool { return len(name) <= MaxNameLength && nameRe.MatchString(name) }

// Connection is a named MQTT broker connection. Username and Password may
// hold {{secret.NAME}} references, resolved when a node connects. Password
// is never returned by the API.
type Connection struct {
	Name      string
	Broker    string // tcp://, mqtt://, ssl://, mqtts://, ws:// or wss://host[:port]
	Username  string
	Password  string
	BaseTopic string // Zigbee2MQTT's base topic
}

// Normalize trims the fields and fills the default base topic.
func (c *Connection) Normalize() {
	c.Name = strings.TrimSpace(c.Name)
	c.Broker = strings.TrimSpace(c.Broker)
	c.Username = strings.TrimSpace(c.Username)
	c.BaseTopic = strings.TrimSpace(c.BaseTopic)
	if c.BaseTopic == "" {
		c.BaseTopic = DefaultBaseTopic
	}
}

var brokerSchemes = map[string]bool{"tcp": true, "mqtt": true, "ssl": true, "tls": true, "mqtts": true, "ws": true, "wss": true}

// Validate checks the connection; its errors never quote the password.
func (c Connection) Validate() error {
	if !ValidName(c.Name) {
		return fmt.Errorf("invalid connection name (1 to %d letters, digits, '_' or '-')", MaxNameLength)
	}
	if c.Broker == "" {
		return fmt.Errorf("broker is required (e.g. tcp://192.168.1.10:1883)")
	}
	u, err := url.Parse(c.Broker)
	if err != nil {
		return fmt.Errorf("broker is not a URL (e.g. tcp://192.168.1.10:1883)")
	}
	if !brokerSchemes[strings.ToLower(u.Scheme)] {
		return fmt.Errorf("broker scheme must be tcp, mqtt, ssl, mqtts, ws or wss")
	}
	if u.Hostname() == "" {
		return fmt.Errorf("broker needs a host")
	}
	if u.User != nil {
		return fmt.Errorf("put the broker's credentials in username and password, not in its URL")
	}
	if (u.Path != "" && u.Path != "/" && !strings.HasPrefix(strings.ToLower(u.Scheme), "ws")) || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("broker URL takes no path or query (only ws/wss take a path)")
	}
	if err := ValidBaseTopic(c.BaseTopic); err != nil {
		return err
	}
	return nil
}

// ValidBaseTopic checks a base topic: levels separated by '/', none empty,
// no wildcards.
func ValidBaseTopic(t string) error {
	if t == "" || len(t) > 256 || strings.ContainsAny(t, "+#\x00") {
		return fmt.Errorf("invalid base topic (e.g. zigbee2mqtt; no wildcards)")
	}
	for _, level := range strings.Split(t, "/") {
		if level == "" {
			return fmt.Errorf("invalid base topic: an empty level")
		}
	}
	return nil
}

// Persister persists connection changes and provides the initial load.
type Persister interface {
	LoadConnections() (map[string]Connection, error)
	SaveConnection(c Connection) error
	DeleteConnection(name string) error
}

// Store is a thread-safe name→connection store with optional persistence.
type Store struct {
	mu      sync.RWMutex
	conns   map[string]Connection
	persist Persister
	// onChange, when set, is called after each successful change with the
	// connection's name (the Zigbee watchers follow it).
	onChange func(name string)
}

// NewStore creates an empty in-memory store.
func NewStore() *Store { return &Store{conns: map[string]Connection{}} }

// Attach loads the persisted connections and enables write-through.
func (s *Store) Attach(p Persister) error {
	loaded, err := p.LoadConnections()
	if err != nil {
		return fmt.Errorf("failed to load broker connections: %w", err)
	}
	s.mu.Lock()
	s.conns = loaded
	s.persist = p
	s.mu.Unlock()
	return nil
}

// OnChange registers fn to be called after each change (one listener).
func (s *Store) OnChange(fn func(name string)) {
	s.mu.Lock()
	s.onChange = fn
	s.mu.Unlock()
}

// Get returns the connection named name.
func (s *Store) Get(name string) (Connection, bool) {
	s.mu.RLock()
	c, ok := s.conns[name]
	s.mu.RUnlock()
	return c, ok
}

// Set validates and stores (or replaces) c, writing through first.
func (s *Store) Set(c Connection) error {
	c.Normalize()
	if err := c.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.persist != nil {
		if err := s.persist.SaveConnection(c); err != nil {
			s.mu.Unlock()
			return err
		}
	}
	s.conns[c.Name] = c
	fn := s.onChange
	s.mu.Unlock()
	if fn != nil {
		fn(c.Name)
	}
	return nil
}

// Delete removes a connection after its persistent deletion succeeds.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	if s.persist != nil {
		if err := s.persist.DeleteConnection(name); err != nil {
			s.mu.Unlock()
			return err
		}
	}
	delete(s.conns, name)
	fn := s.onChange
	s.mu.Unlock()
	if fn != nil {
		fn(name)
	}
	return nil
}

// Names returns the sorted connection names.
func (s *Store) Names() []string {
	s.mu.RLock()
	names := make([]string, 0, len(s.conns))
	for n := range s.conns {
		names = append(names, n)
	}
	s.mu.RUnlock()
	sort.Strings(names)
	return names
}

// List returns the connections sorted by name.
func (s *Store) List() []Connection {
	s.mu.RLock()
	out := make([]Connection, 0, len(s.conns))
	for _, c := range s.conns {
		out = append(out, c)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// DefaultStore is the process-wide store the nodes use.
var DefaultStore = NewStore()

type storeKey struct{}

// WithStore returns a context whose lookups use store instead of
// DefaultStore; a nil store means an empty one (isolation fails closed).
func WithStore(ctx context.Context, store *Store) context.Context {
	if store == nil {
		store = NewStore()
	}
	return context.WithValue(ctx, storeKey{}, store)
}

// FromContext returns the store carried by ctx, or DefaultStore.
func FromContext(ctx context.Context) *Store {
	if ctx != nil {
		if s, ok := ctx.Value(storeKey{}).(*Store); ok && s != nil {
			return s
		}
	}
	return DefaultStore
}
