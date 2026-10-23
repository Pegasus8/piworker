package storage

import (
	"encoding/json"
	"fmt"
	"time"
)

// LoadVars returns all persisted flow variables, JSON-decoded. Implements
// vars.Persister.
func (s *SQLiteStore) LoadVars() (map[string]interface{}, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	rows, err := s.db.Query(`SELECT key, value FROM vars`)
	if err != nil {
		return nil, fmt.Errorf("failed to load vars: %w", err)
	}
	defer rows.Close()

	out := make(map[string]interface{})
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, fmt.Errorf("failed to scan var: %w", err)
		}
		var value interface{}
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			// Skip a corrupt entry rather than failing the whole load.
			continue
		}
		out[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating vars: %w", err)
	}
	return out, nil
}

// SaveVar stores (or replaces) a variable, JSON-encoding its value.
func (s *SQLiteStore) SaveVar(key string, value interface{}) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to encode var: %w", err)
	}
	_, err = s.db.Exec(`
		INSERT INTO vars (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, string(raw), time.Now())
	if err != nil {
		return fmt.Errorf("failed to save var: %w", err)
	}
	return nil
}

// DeleteVar removes a variable.
func (s *SQLiteStore) DeleteVar(key string) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	if _, err := s.db.Exec(`DELETE FROM vars WHERE key = ?`, key); err != nil {
		return fmt.Errorf("failed to delete var: %w", err)
	}
	return nil
}
