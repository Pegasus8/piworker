package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Pegasus8/piworker/internal/llm"
)

// The LLM connections (ADR-0038) are kept in llm_connections; the API key
// is stored as given (a {{secret.NAME}} reference or plain text, like the
// broker passwords and the secrets table, ADR-0030) and never returned by
// the API. llm_usage counts each connection's tokens and requests per local
// calendar day, so a daily token limit survives a restart.
//
// Part of migration 2 (migrateLLM): never edit it once released, append a
// migration instead.
const llmConnectionsSchema = `
CREATE TABLE llm_connections (
	name              TEXT PRIMARY KEY,
	provider          TEXT NOT NULL,
	base_url          TEXT NOT NULL DEFAULT '',
	api_key           TEXT NOT NULL DEFAULT '',
	model             TEXT NOT NULL DEFAULT '',
	daily_token_limit INTEGER NOT NULL DEFAULT 0,
	updated_at        DATETIME NOT NULL
);
CREATE TABLE llm_usage (
	day           TEXT NOT NULL,
	connection    TEXT NOT NULL,
	input_tokens  INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	requests      INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (day, connection)
);`

// The SQLite store is the persister of the LLM connections and their usage.
var _ llm.Persister = (*SQLiteStore)(nil)

// LoadLLMConnections returns every LLM connection by name. Implements
// llm.Persister.
func (s *SQLiteStore) LoadLLMConnections() (map[string]llm.Connection, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	rows, err := s.db.Query(`SELECT name, provider, base_url, api_key, model, daily_token_limit FROM llm_connections`)
	if err != nil {
		return nil, fmt.Errorf("failed to load LLM connections: %w", err)
	}
	defer rows.Close()
	out := map[string]llm.Connection{}
	for rows.Next() {
		var c llm.Connection
		var provider string
		if err := rows.Scan(&c.Name, &provider, &c.BaseURL, &c.APIKey, &c.Model, &c.DailyTokenLimit); err != nil {
			return nil, fmt.Errorf("failed to scan LLM connection: %w", err)
		}
		c.Provider = llm.Provider(provider)
		out[c.Name] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating LLM connections: %w", err)
	}
	return out, nil
}

// SaveLLMConnection stores (or replaces) an LLM connection. Implements
// llm.Persister.
func (s *SQLiteStore) SaveLLMConnection(c llm.Connection) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	_, err := s.db.Exec(`
		INSERT INTO llm_connections (name, provider, base_url, api_key, model, daily_token_limit, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET provider = excluded.provider, base_url = excluded.base_url, api_key = excluded.api_key,
			model = excluded.model, daily_token_limit = excluded.daily_token_limit, updated_at = excluded.updated_at
	`, c.Name, string(c.Provider), c.BaseURL, c.APIKey, c.Model, c.DailyTokenLimit, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save LLM connection: %w", err)
	}
	return nil
}

// DeleteLLMConnection removes an LLM connection. Its usage rows stay: a
// connection deleted and created again under the same name the same day
// does not get a fresh daily budget. Implements llm.Persister.
func (s *SQLiteStore) DeleteLLMConnection(name string) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	if _, err := s.db.Exec(`DELETE FROM llm_connections WHERE name = ?`, name); err != nil {
		return fmt.Errorf("failed to delete LLM connection: %w", err)
	}
	return nil
}

// AddLLMUsage adds u to the counters of (day, name), in one statement, so
// concurrent calls never lose an increment. Implements llm.Persister.
func (s *SQLiteStore) AddLLMUsage(day, name string, u llm.Usage) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	_, err := s.db.Exec(`
		INSERT INTO llm_usage (day, connection, input_tokens, output_tokens, requests) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(day, connection) DO UPDATE SET input_tokens = input_tokens + excluded.input_tokens,
			output_tokens = output_tokens + excluded.output_tokens, requests = requests + excluded.requests
	`, day, name, u.InputTokens, u.OutputTokens, u.Requests)
	if err != nil {
		return fmt.Errorf("failed to record LLM usage: %w", err)
	}
	return nil
}

// LLMUsage returns the counters of (day, name): zero when there are none.
// Implements llm.Persister.
func (s *SQLiteStore) LLMUsage(day, name string) (llm.Usage, error) {
	if s.isClosed() {
		return llm.Usage{}, ErrDatabaseClosed
	}
	var u llm.Usage
	err := s.db.QueryRow(`SELECT input_tokens, output_tokens, requests FROM llm_usage WHERE day = ? AND connection = ?`, day, name).
		Scan(&u.InputTokens, &u.OutputTokens, &u.Requests)
	if errors.Is(err, sql.ErrNoRows) {
		return llm.Usage{}, nil
	}
	if err != nil {
		return llm.Usage{}, fmt.Errorf("failed to read LLM usage: %w", err)
	}
	return u, nil
}
