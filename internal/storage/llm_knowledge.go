package storage

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/rs/zerolog/log"
)

// The knowledge base of the LLM nodes (docs/design/llm.md §1.7) is kept in
// llm_documents: one row per (collection, document ID) with its text, its
// metadata (a JSON object), the embedding model and the embedding itself as
// a BLOB of little-endian float32 values (4 bytes per dimension: a
// 1 536-dimension embedding is 6 KiB; llm.Knowledge stores it normalized to
// unit length). Search is brute force over what llm.Knowledge loaded, so
// the table is only read at start.
//
// Part of migration 2 (migrateLLM): never edit it once released, append a
// migration instead.
const llmKnowledgeSchema = `
CREATE TABLE llm_documents (
	collection TEXT NOT NULL,
	id         TEXT NOT NULL,
	text       TEXT NOT NULL,
	metadata   TEXT NOT NULL DEFAULT '{}',
	model      TEXT NOT NULL,
	dimensions INTEGER NOT NULL,
	embedding  BLOB NOT NULL,
	created_at DATETIME NOT NULL,
	PRIMARY KEY (collection, id)
);`

// The SQLite store is the persister of the knowledge base.
var _ llm.KnowledgePersister = (*SQLiteStore)(nil)

// encodeEmbedding encodes e as little-endian float32 values.
func encodeEmbedding(e llm.Embedding) []byte {
	out := make([]byte, 4*len(e))
	for i, v := range e {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(v))
	}
	return out
}

// decodeEmbedding decodes a stored embedding of dims dimensions.
func decodeEmbedding(raw []byte, dims int) (llm.Embedding, error) {
	if dims <= 0 || len(raw) != 4*dims {
		return nil, fmt.Errorf("the embedding has %d bytes, not %d dimensions of 4 bytes", len(raw), dims)
	}
	out := make(llm.Embedding, dims)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
	}
	return out, nil
}

// LoadLLMDocuments returns every document by collection, oldest first, then
// by ID. A row whose embedding or metadata cannot be decoded is left out
// (and logged): one damaged document must not keep PiWorker from starting.
// Implements llm.KnowledgePersister.
func (s *SQLiteStore) LoadLLMDocuments() (map[string][]llm.Document, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	rows, err := s.db.Query(`SELECT collection, id, text, metadata, model, dimensions, embedding, created_at
		FROM llm_documents ORDER BY collection, created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("failed to load the knowledge base: %w", err)
	}
	defer rows.Close()
	out := map[string][]llm.Document{}
	for rows.Next() {
		var (
			collection, metadata string
			dims                 int
			raw                  []byte
			d                    llm.Document
		)
		if err := rows.Scan(&collection, &d.ID, &d.Text, &metadata, &d.Model, &dims, &raw, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan a knowledge document: %w", err)
		}
		if err := decodeDocument(&d, metadata, raw, dims); err != nil {
			log.Warn().Err(err).Str("collection", collection).Str("document", d.ID).
				Msg("Skipping a knowledge document that cannot be read")
			continue
		}
		out[collection] = append(out[collection], d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating the knowledge base: %w", err)
	}
	return out, nil
}

// decodeDocument fills d's metadata (none for an empty object) and
// embedding from their stored forms.
func decodeDocument(d *llm.Document, metadata string, raw []byte, dims int) error {
	e, err := decodeEmbedding(raw, dims)
	if err != nil {
		return err
	}
	d.Embedding = e
	var meta map[string]any
	if err := json.Unmarshal([]byte(metadata), &meta); err != nil {
		return errors.New("the metadata is not a JSON object")
	}
	if len(meta) > 0 {
		d.Metadata = meta
	}
	return nil
}

// SaveLLMDocument stores (or replaces) a document of collection. Implements
// llm.KnowledgePersister.
func (s *SQLiteStore) SaveLLMDocument(collection string, d llm.Document) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	meta := []byte("{}")
	if len(d.Metadata) > 0 {
		raw, err := json.Marshal(d.Metadata)
		if err != nil {
			return fmt.Errorf("failed to encode the document's metadata: %w", err)
		}
		meta = raw
	}
	created := d.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	_, err := s.db.Exec(`
		INSERT INTO llm_documents (collection, id, text, metadata, model, dimensions, embedding, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(collection, id) DO UPDATE SET text = excluded.text, metadata = excluded.metadata, model = excluded.model,
			dimensions = excluded.dimensions, embedding = excluded.embedding, created_at = excluded.created_at
	`, collection, d.ID, d.Text, string(meta), d.Model, len(d.Embedding), encodeEmbedding(d.Embedding), created)
	if err != nil {
		return fmt.Errorf("failed to save a knowledge document: %w", err)
	}
	return nil
}

// DeleteLLMDocument removes one document (none is not an error).
// Implements llm.KnowledgePersister.
func (s *SQLiteStore) DeleteLLMDocument(collection, id string) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	if _, err := s.db.Exec(`DELETE FROM llm_documents WHERE collection = ? AND id = ?`, collection, id); err != nil {
		return fmt.Errorf("failed to delete a knowledge document: %w", err)
	}
	return nil
}

// DeleteLLMCollection removes every document of a collection. Implements
// llm.KnowledgePersister.
func (s *SQLiteStore) DeleteLLMCollection(collection string) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	if _, err := s.db.Exec(`DELETE FROM llm_documents WHERE collection = ?`, collection); err != nil {
		return fmt.Errorf("failed to delete a knowledge collection: %w", err)
	}
	return nil
}
