package llm

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/secrets"
)

// Bounds on a connection's free-text settings (they are the admin's, but
// they end up in requests, logs and the editor).
const (
	maxBaseURLLength = 2048
	maxAPIKeyLength  = 4096
)

// MaxModelLength bounds a model name, wherever one is set (a connection's
// default, a node's model, the flow assistant's request).
const MaxModelLength = 256

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidName reports whether name is a legal connection (or knowledge
// collection) name: 1 to MaxNameLength letters, digits, '_' or '-'.
func ValidName(name string) bool { return len(name) <= MaxNameLength && nameRe.MatchString(name) }

// Normalize trims the fields, lower-cases the provider and drops a trailing
// '/' from the base URL.
func (c *Connection) Normalize() {
	c.Name = strings.TrimSpace(c.Name)
	c.Provider = Provider(strings.ToLower(strings.TrimSpace(string(c.Provider))))
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.Model = strings.TrimSpace(c.Model)
}

// Validate checks the connection. Its errors never quote the API key, nor
// the base URL (which could carry credentials the admin pasted by mistake).
func (c Connection) Validate() error {
	if !ValidName(c.Name) {
		return fmt.Errorf("invalid connection name (1 to %d letters, digits, '_' or '-')", MaxNameLength)
	}
	switch c.Provider {
	case ProviderAnthropic, ProviderOpenAI:
	default:
		return fmt.Errorf("provider must be %q or %q", ProviderAnthropic, ProviderOpenAI)
	}
	if c.BaseURL != "" {
		if err := validBaseURL(c.BaseURL); err != nil {
			return err
		}
	}
	if len(c.APIKey) > maxAPIKeyLength {
		return fmt.Errorf("the API key is too long (at most %d bytes)", maxAPIKeyLength)
	}
	if c.Provider == ProviderAnthropic && c.APIKey == "" {
		return fmt.Errorf("an Anthropic connection needs an API key")
	}
	if len(c.Model) > MaxModelLength || strings.ContainsAny(c.Model, "\x00\r\n") {
		return fmt.Errorf("invalid model name (at most %d characters, one line)", MaxModelLength)
	}
	if c.DailyTokenLimit < 0 {
		return fmt.Errorf("the daily token limit cannot be negative (0: no limit)")
	}
	return nil
}

func validBaseURL(raw string) error {
	const example = "e.g. http://192.168.1.20:11434/v1"
	if len(raw) > maxBaseURLLength {
		return fmt.Errorf("the base URL is too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("the base URL is not a URL (%s)", example)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("the base URL must start with http:// or https:// (%s)", example)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("the base URL needs a host (%s)", example)
	}
	if u.User != nil {
		return fmt.Errorf("put the API key in its own field, not in the base URL")
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("the base URL takes no query or fragment (%s)", example)
	}
	return nil
}

// Endpoint returns the base URL requests go to: BaseURL, or the provider's
// default.
func (c Connection) Endpoint() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	if c.Provider == ProviderAnthropic {
		return DefaultAnthropicURL
	}
	return DefaultOpenAIURL
}

// resolvedKey expands the API key's {{secret.NAME}} references through the
// secret store of ctx, strictly: an undefined secret is an error naming the
// secret (never a value), so a typo is not sent as an empty key.
func (c Connection) resolvedKey(ctx context.Context) (string, error) {
	if !strings.Contains(c.APIKey, "{{") {
		return c.APIKey, nil
	}
	store := secrets.FromContext(ctx)
	for _, name := range secrets.References(c.APIKey) {
		if _, ok := store.Get(name); !ok {
			return "", fmt.Errorf("secret %q is not defined (referenced by the API key of LLM connection %q)", name, c.Name)
		}
	}
	return strings.TrimSpace(secrets.Expand(ctx, c.APIKey)), nil
}

// Persister persists connection changes and the daily usage, and provides
// the initial load. The SQLite store implements it.
type Persister interface {
	LoadLLMConnections() (map[string]Connection, error)
	SaveLLMConnection(c Connection) error
	DeleteLLMConnection(name string) error
	// AddLLMUsage adds u to the counters of (day, name).
	AddLLMUsage(day, name string, u Usage) error
	// LLMUsage returns the counters of (day, name).
	LLMUsage(day, name string) (Usage, error)
}

// dayLayout is the key of a usage day (the local calendar day).
const dayLayout = "2006-01-02"

// Store is a thread-safe name→connection store with optional persistence,
// and the daily usage of each connection.
type Store struct {
	mu      sync.RWMutex
	conns   map[string]Connection
	persist Persister

	// usage holds today's counters per connection; an entry is loaded from
	// the persister the first time the day is asked for. umu is separate
	// from mu so a slow read of the usage does not block lookups.
	umu      sync.Mutex
	usageDay string
	usage    map[string]*usageEntry

	// now is the clock of the usage days (tests replace it).
	now func() time.Time
}

type usageEntry struct {
	u      Usage
	loaded bool
}

// NewStore creates an empty in-memory store.
func NewStore() *Store {
	return &Store{conns: map[string]Connection{}, usage: map[string]*usageEntry{}, now: time.Now}
}

// Attach loads the persisted connections and enables write-through (of the
// connections and of the usage).
func (s *Store) Attach(p Persister) error {
	loaded, err := p.LoadLLMConnections()
	if err != nil {
		return fmt.Errorf("failed to load LLM connections: %w", err)
	}
	if loaded == nil {
		loaded = map[string]Connection{}
	}
	s.mu.Lock()
	s.conns = loaded
	s.persist = p
	s.mu.Unlock()
	s.umu.Lock()
	s.usage = map[string]*usageEntry{}
	s.umu.Unlock()
	return nil
}

func (s *Store) persister() Persister {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.persist
}

// Get returns the connection named name.
func (s *Store) Get(name string) (Connection, bool) {
	s.mu.RLock()
	c, ok := s.conns[name]
	s.mu.RUnlock()
	return c, ok
}

// Set normalizes, validates and stores (or replaces) c, writing through
// first.
func (s *Store) Set(c Connection) error {
	c.Normalize()
	if err := c.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.persist != nil {
		if err := s.persist.SaveLLMConnection(c); err != nil {
			return err
		}
	}
	s.conns[c.Name] = c
	return nil
}

// Delete removes a connection after its persistent deletion succeeds.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.persist != nil {
		if err := s.persist.DeleteLLMConnection(name); err != nil {
			return err
		}
	}
	delete(s.conns, name)
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

// usageLocked returns today's entry of name, loading it from the persister
// the first time (s.umu held). On a load failure the entry stays unloaded
// (its in-memory counters are not used) and the error is returned.
func (s *Store) usageLocked(day, name string, p Persister) (*usageEntry, error) {
	if s.usageDay != day {
		// A new day starts afresh; yesterday's entries are dropped.
		s.usageDay = day
		s.usage = map[string]*usageEntry{}
	}
	e := s.usage[name]
	if e == nil {
		e = &usageEntry{}
		s.usage[name] = e
	}
	if !e.loaded {
		if p != nil {
			u, err := p.LLMUsage(day, name)
			if err != nil {
				return e, err
			}
			e.u = u
		}
		e.loaded = true
	}
	return e, nil
}

// RecordUsage adds u to today's counters of the connection named name (the
// local calendar day), in memory and through the persister. The call it
// counts already happened, so the caller logs an error instead of failing.
func (s *Store) RecordUsage(name string, u Usage) error {
	if u == (Usage{}) {
		return nil
	}
	p := s.persister()
	s.umu.Lock()
	defer s.umu.Unlock()
	day := s.now().Format(dayLayout)
	e, loadErr := s.usageLocked(day, name, p)
	var saveErr error
	if p != nil {
		saveErr = p.AddLLMUsage(day, name, u)
	}
	// An unloaded entry is not counted in memory: its load (later) reads
	// the persisted counters, which already hold u when the save worked.
	if e.loaded {
		e.u = e.u.Add(u)
	}
	if saveErr != nil {
		return fmt.Errorf("failed to record the usage of LLM connection %q: %w", name, saveErr)
	}
	if loadErr != nil {
		return fmt.Errorf("failed to read the usage of LLM connection %q: %w", name, loadErr)
	}
	return nil
}

// UsageToday returns today's counters of the connection named name (zero
// when they cannot be read).
func (s *Store) UsageToday(name string) Usage {
	p := s.persister()
	s.umu.Lock()
	defer s.umu.Unlock()
	e, err := s.usageLocked(s.now().Format(dayLayout), name, p)
	if err != nil {
		return Usage{}
	}
	return e.u
}

// CheckBudget fails with ErrBudgetExhausted when c has a daily token limit
// and today's input plus output tokens reached it. It fails closed: when
// today's usage cannot be read, a limited connection is refused.
func (s *Store) CheckBudget(c Connection) error {
	if c.DailyTokenLimit <= 0 {
		return nil
	}
	p := s.persister()
	s.umu.Lock()
	defer s.umu.Unlock()
	e, err := s.usageLocked(s.now().Format(dayLayout), c.Name, p)
	if err != nil {
		return fmt.Errorf("cannot read today's usage of LLM connection %q: %w", c.Name, err)
	}
	if e.u.Total() >= c.DailyTokenLimit {
		return fmt.Errorf("LLM connection %q: %w (%d tokens per day)", c.Name, ErrBudgetExhausted, c.DailyTokenLimit)
	}
	return nil
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

// Lookup resolves the connection name through ctx (FromContext), the way
// every LLM node does.
func Lookup(ctx context.Context, name string) (Connection, error) {
	c, ok := FromContext(ctx).Get(name)
	if !ok {
		return Connection{}, fmt.Errorf("LLM connection %q is not defined: %w", name, ErrUndefined)
	}
	return c, nil
}

// SecretReference is a {{secret.NAME}} reference in a connection's API key.
type SecretReference struct {
	Name    string
	Setting string // "apiKey"
}

// SecretReferences returns the secrets c's API key references, in order of
// appearance, each once (as NewClient resolves them).
func SecretReferences(c Connection) []SecretReference {
	var out []SecretReference
	for _, name := range secrets.References(c.APIKey) {
		out = append(out, SecretReference{Name: name, Setting: "apiKey"})
	}
	return out
}
