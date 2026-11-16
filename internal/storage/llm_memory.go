package storage

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/rs/zerolog/log"
)

// The conversation memory of the LLM nodes (docs/design/llm.md §1.6) is
// kept in llm_conversations: one row per conversation key, its text turns
// as a JSON array (llm.Memory caps them: 200 turns of at most 32 KiB, 5 000
// conversations), rewritten whole on every change.
//
// Part of migration 2 (migrateLLM): never edit it once released, append a
// migration instead.
const llmMemorySchema = `
CREATE TABLE llm_conversations (
	key        TEXT PRIMARY KEY,
	turns      TEXT NOT NULL,
	updated_at DATETIME NOT NULL
);`

// The SQLite store is the persister of the conversation memory.
var _ llm.MemoryPersister = (*SQLiteStore)(nil)

// LoadLLMConversations returns every conversation's turns by key. A row
// whose turns cannot be decoded is left out (and logged): one damaged
// conversation must not keep PiWorker from starting. Implements
// llm.MemoryPersister.
func (s *SQLiteStore) LoadLLMConversations() (map[string][]llm.Turn, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	rows, err := s.db.Query(`SELECT key, turns FROM llm_conversations`)
	if err != nil {
		return nil, fmt.Errorf("failed to load LLM conversations: %w", err)
	}
	defer rows.Close()
	out := map[string][]llm.Turn{}
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, fmt.Errorf("failed to scan LLM conversation: %w", err)
		}
		var turns []llm.Turn
		if err := json.Unmarshal([]byte(raw), &turns); err != nil {
			// The key is the admin's template output, not a secret; the
			// turns are not logged.
			log.Warn().Err(err).Str("key", key).Msg("Skipping an LLM conversation whose stored turns cannot be read")
			continue
		}
		out[key] = turns
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating LLM conversations: %w", err)
	}
	return out, nil
}

// SaveLLMConversation stores (or replaces) the turns of a conversation.
// Implements llm.MemoryPersister.
func (s *SQLiteStore) SaveLLMConversation(key string, turns []llm.Turn) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	if turns == nil {
		turns = []llm.Turn{}
	}
	raw, err := json.Marshal(turns)
	if err != nil {
		return fmt.Errorf("failed to encode LLM conversation: %w", err)
	}
	_, err = s.db.Exec(`
		INSERT INTO llm_conversations (key, turns, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET turns = excluded.turns, updated_at = excluded.updated_at
	`, key, string(raw), time.Now())
	if err != nil {
		return fmt.Errorf("failed to save LLM conversation: %w", err)
	}
	return nil
}

// DeleteLLMConversation removes a conversation (none is not an error).
// Implements llm.MemoryPersister.
func (s *SQLiteStore) DeleteLLMConversation(key string) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	if _, err := s.db.Exec(`DELETE FROM llm_conversations WHERE key = ?`, key); err != nil {
		return fmt.Errorf("failed to delete LLM conversation: %w", err)
	}
	return nil
}
