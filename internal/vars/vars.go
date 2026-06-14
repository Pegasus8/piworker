// Package vars provides a process-wide key/value store for flow variables,
// shared by the set-var and get-var nodes. Values are kept in memory for fast
// access and optionally persisted (write-through) so they survive restarts.
package vars

import "sync"

// Persister persists variable mutations and provides the initial load. The
// SQLite store implements it. Persistence is best-effort: the in-memory map is
// always authoritative, so a persist failure never blocks a node.
type Persister interface {
	LoadVars() (map[string]interface{}, error)
	SaveVar(key string, value interface{}) error
	DeleteVar(key string) error
}

// Store is a thread-safe key/value store.
type Store struct {
	mu      sync.RWMutex
	m       map[string]interface{}
	persist Persister
}

// NewStore creates an empty Store.
func NewStore() *Store {
	return &Store{m: make(map[string]interface{})}
}

// Attach loads persisted variables and enables write-through persistence.
func (s *Store) Attach(p Persister) error {
	loaded, err := p.LoadVars()
	if err != nil {
		return err
	}
	s.mu.Lock()
	if loaded != nil {
		s.m = loaded
	}
	s.persist = p
	s.mu.Unlock()
	return nil
}

// Set stores a value under key, writing through to the persister if attached.
func (s *Store) Set(key string, value interface{}) {
	s.mu.Lock()
	s.m[key] = value
	p := s.persist
	s.mu.Unlock()
	if p != nil {
		// Best-effort: the in-memory value is authoritative.
		_ = p.SaveVar(key, value)
	}
}

// Get returns the value stored under key and whether it exists.
func (s *Store) Get(key string) (interface{}, bool) {
	s.mu.RLock()
	v, ok := s.m[key]
	s.mu.RUnlock()
	return v, ok
}

// Delete removes a key, writing through to the persister if attached.
func (s *Store) Delete(key string) {
	s.mu.Lock()
	delete(s.m, key)
	p := s.persist
	s.mu.Unlock()
	if p != nil {
		_ = p.DeleteVar(key)
	}
}

// Snapshot returns a shallow copy of all stored values.
func (s *Store) Snapshot() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]interface{}, len(s.m))
	for k, v := range s.m {
		out[k] = v
	}
	return out
}

// DefaultStore is the process-wide variable store used by the built-in nodes.
var DefaultStore = NewStore()
