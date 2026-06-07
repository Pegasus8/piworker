// Package vars provides a simple process-wide key/value store for flow variables,
// shared by the set-var and get-var nodes. Values are kept in memory for the
// lifetime of the process.
package vars

import "sync"

// Store is a thread-safe key/value store.
type Store struct {
	mu sync.RWMutex
	m  map[string]interface{}
}

// NewStore creates an empty Store.
func NewStore() *Store {
	return &Store{m: make(map[string]interface{})}
}

// Set stores a value under key.
func (s *Store) Set(key string, value interface{}) {
	s.mu.Lock()
	s.m[key] = value
	s.mu.Unlock()
}

// Get returns the value stored under key and whether it exists.
func (s *Store) Get(key string) (interface{}, bool) {
	s.mu.RLock()
	v, ok := s.m[key]
	s.mu.RUnlock()
	return v, ok
}

// Delete removes a key.
func (s *Store) Delete(key string) {
	s.mu.Lock()
	delete(s.m, key)
	s.mu.Unlock()
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
