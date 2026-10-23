// Package vars provides a process-wide key/value store for flow variables,
// shared by the set-var and get-var nodes. Values are kept in memory for fast
// access and optionally persisted (write-through) so they survive restarts.
package vars

import (
	"context"
	"sync"
)

// Persister persists variable mutations and provides the initial load. The
// SQLite store implements it. Mutations become visible only after persistence succeeds.
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
func (s *Store) Set(key string, value interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value = clone(value)
	if s.persist != nil {
		if err := s.persist.SaveVar(key, value); err != nil {
			return err
		}
	}
	s.m[key] = value
	return nil
}

// Get returns the value stored under key and whether it exists.
func (s *Store) Get(key string) (interface{}, bool) {
	s.mu.RLock()
	v, ok := s.m[key]
	v = clone(v)
	s.mu.RUnlock()
	return v, ok
}

// Delete removes a key, writing through to the persister if attached.
func (s *Store) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.persist != nil {
		if err := s.persist.DeleteVar(key); err != nil {
			return err
		}
	}
	delete(s.m, key)
	return nil
}

// Snapshot returns an isolated copy of all stored values.
func (s *Store) Snapshot() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]interface{}, len(s.m))
	for k, v := range s.m {
		out[k] = clone(v)
	}
	return out
}

// DefaultStore is the process-wide variable store used by the built-in nodes.
var DefaultStore = NewStore()

// clone isolates JSON-shaped composites from callers and concurrent executions.
func clone(v interface{}) interface{} {
	switch value := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(value))
		for k, item := range value {
			out[k] = clone(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(value))
		for i, item := range value {
			out[i] = clone(item)
		}
		return out
	default:
		return v
	}
}

type storeKey struct{}

// WithStore supplies an isolated variable store for a preview execution.
func WithStore(ctx context.Context, store *Store) context.Context {
	return context.WithValue(ctx, storeKey{}, store)
}
func FromContext(ctx context.Context) *Store {
	if store, ok := ctx.Value(storeKey{}).(*Store); ok {
		return store
	}
	return DefaultStore
}
