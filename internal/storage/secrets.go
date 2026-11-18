package storage

import (
	"fmt"
	"time"
)

// LoadSecrets returns all stored secrets as a name→value map. Implements
// secrets.Persister.
func (s *SQLiteStore) LoadSecrets() (map[string]string, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	rows, err := s.db.Query(`SELECT name, value FROM secrets`)
	if err != nil {
		return nil, fmt.Errorf("failed to load secrets: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return nil, fmt.Errorf("failed to scan secret: %w", err)
		}
		out[name] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating secrets: %w", err)
	}
	return out, nil
}

// SaveSecret stores (or replaces) a secret value.
func (s *SQLiteStore) SaveSecret(name, value string) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	_, err := s.db.Exec(`
		INSERT INTO secrets (name, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, name, value, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save secret: %w", err)
	}
	return nil
}

// DeleteSecret removes a secret.
func (s *SQLiteStore) DeleteSecret(name string) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	if _, err := s.db.Exec(`DELETE FROM secrets WHERE name = ?`, name); err != nil {
		return fmt.Errorf("failed to delete secret: %w", err)
	}
	return nil
}
