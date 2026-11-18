package storage

import (
	"fmt"
	"time"

	"github.com/Pegasus8/piworker/internal/brokers"
)

// The broker connections (ADR-0026) are kept in broker_connections; the
// password is stored as given (a {{secret.NAME}} reference or plain text,
// like the secrets table) and never returned by the API.
const brokerConnectionsSchema = `
CREATE TABLE IF NOT EXISTS broker_connections (
	name       TEXT PRIMARY KEY,
	broker     TEXT NOT NULL,
	username   TEXT NOT NULL DEFAULT '',
	password   TEXT NOT NULL DEFAULT '',
	base_topic TEXT NOT NULL,
	updated_at DATETIME NOT NULL
);`

// LoadConnections returns every broker connection by name. Implements
// brokers.Persister.
func (s *SQLiteStore) LoadConnections() (map[string]brokers.Connection, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	rows, err := s.db.Query(`SELECT name, broker, username, password, base_topic FROM broker_connections`)
	if err != nil {
		return nil, fmt.Errorf("failed to load broker connections: %w", err)
	}
	defer rows.Close()
	out := map[string]brokers.Connection{}
	for rows.Next() {
		var c brokers.Connection
		if err := rows.Scan(&c.Name, &c.Broker, &c.Username, &c.Password, &c.BaseTopic); err != nil {
			return nil, fmt.Errorf("failed to scan broker connection: %w", err)
		}
		out[c.Name] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating broker connections: %w", err)
	}
	return out, nil
}

// SaveConnection stores (or replaces) a broker connection.
func (s *SQLiteStore) SaveConnection(c brokers.Connection) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	_, err := s.db.Exec(`
		INSERT INTO broker_connections (name, broker, username, password, base_topic, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET broker = excluded.broker, username = excluded.username,
			password = excluded.password, base_topic = excluded.base_topic, updated_at = excluded.updated_at
	`, c.Name, c.Broker, c.Username, c.Password, c.BaseTopic, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save broker connection: %w", err)
	}
	return nil
}

// DeleteConnection removes a broker connection.
func (s *SQLiteStore) DeleteConnection(name string) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	if _, err := s.db.Exec(`DELETE FROM broker_connections WHERE name = ?`, name); err != nil {
		return fmt.Errorf("failed to delete broker connection: %w", err)
	}
	return nil
}
