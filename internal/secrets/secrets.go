// Package secrets provides a process-wide store for named secret values
// (API tokens, SMTP passwords, …) that flow nodes reference as {{secret.NAME}}
// instead of embedding the plaintext in the flow definition. Values are kept in
// memory for fast lookups during template rendering and optionally persisted
// through a Persister (the SQLite store) so they survive restarts.
package secrets

import (
	"fmt"
	"regexp"
	"sort"
	"sync"
)

// nameRe constrains secret names so {{secret.NAME}} parsing is unambiguous.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Persister persists secret mutations and provides the initial load. The SQLite
// store implements it.
type Persister interface {
	LoadSecrets() (map[string]string, error)
	SaveSecret(name, value string) error
	DeleteSecret(name string) error
}

// Store is a thread-safe name→value secret store with optional persistence.
type Store struct {
	mu      sync.RWMutex
	values  map[string]string
	persist Persister
}

// NewStore creates an empty in-memory store (no persistence until Attach).
func NewStore() *Store {
	return &Store{values: make(map[string]string)}
}

// Attach loads existing secrets from the persister and enables write-through.
func (s *Store) Attach(p Persister) error {
	loaded, err := p.LoadSecrets()
	if err != nil {
		return fmt.Errorf("failed to load secrets: %w", err)
	}
	s.mu.Lock()
	s.values = loaded
	s.persist = p
	s.mu.Unlock()
	return nil
}

// ValidName reports whether name is a legal secret name.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// Get returns the value for name and whether it exists.
func (s *Store) Get(name string) (string, bool) {
	s.mu.RLock()
	v, ok := s.values[name]
	s.mu.RUnlock()
	return v, ok
}

// Set stores (or replaces) a secret, writing through to the persister if set.
func (s *Store) Set(name, value string) error {
	if !ValidName(name) {
		return fmt.Errorf("invalid secret name %q (use letters, digits, '_' or '-')", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.persist != nil {
		if err := s.persist.SaveSecret(name, value); err != nil {
			return err
		}
	}
	if s.values == nil {
		s.values = make(map[string]string)
	}
	s.values[name] = value
	return nil
}

// Delete removes a secret only after its persistent deletion succeeds.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.persist != nil {
		if err := s.persist.DeleteSecret(name); err != nil {
			return err
		}
	}
	delete(s.values, name)
	return nil
}

// Names returns the sorted secret names. Values are never exposed in bulk.
func (s *Store) Names() []string {
	s.mu.RLock()
	names := make([]string, 0, len(s.values))
	for k := range s.values {
		names = append(names, k)
	}
	s.mu.RUnlock()
	sort.Strings(names)
	return names
}

// DefaultStore is the process-wide secret store used by the built-in nodes.
var DefaultStore = NewStore()
