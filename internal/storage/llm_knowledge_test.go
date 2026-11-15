package storage

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Embeddings are stored as little-endian float32, 4 bytes per dimension,
// and read back bit for bit.
func TestEmbeddingEncoding(t *testing.T) {
	e := llm.Embedding{1, -2.5, 0, float32(math.SmallestNonzeroFloat32)}
	raw := encodeEmbedding(e)
	require.Len(t, raw, 16)
	assert.Equal(t, []byte{0x00, 0x00, 0x80, 0x3f}, raw[0:4], "1.0 little-endian")
	assert.Equal(t, []byte{0x00, 0x00, 0x20, 0xc0}, raw[4:8], "-2.5 little-endian")
	got, err := decodeEmbedding(raw, 4)
	require.NoError(t, err)
	assert.Equal(t, e, got)

	_, err = decodeEmbedding(raw[:15], 4)
	assert.Error(t, err, "not a whole number of float32s")
	_, err = decodeEmbedding(raw, 3)
	assert.Error(t, err, "dimensions do not match")
	_, err = decodeEmbedding(nil, 0)
	assert.Error(t, err, "no embedding")
}

// The SQLite store keeps the knowledge base's documents (text, metadata,
// model, embedding) by collection and ID; an llm.Knowledge attaches to it
// across a restart and searches what it loaded.
func TestLLMDocumentsPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knowledge.db")
	store, err := NewSQLiteStore(path)
	require.NoError(t, err)

	loaded, err := store.LoadLLMDocuments()
	require.NoError(t, err)
	assert.Empty(t, loaded)

	at := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	manual := llm.Document{ID: "manual#0", Text: "The boiler restarts with the red button.", Metadata: map[string]any{"source": "manual.pdf", "page": float64(3)},
		Model: "nomic-embed-text", Embedding: llm.Embedding{0.6, 0.8, 0}, CreatedAt: at}
	require.NoError(t, store.SaveLLMDocument("home", manual))
	manual.Text = "The boiler restarts with the red button (hold 5 s)."
	require.NoError(t, store.SaveLLMDocument("home", manual), "replaces the same ID")
	faq := llm.Document{ID: "faq", Text: "Wi-Fi password is on the router.", Model: "nomic-embed-text", Embedding: llm.Embedding{0, 0, 1}, CreatedAt: at.Add(time.Minute)}
	require.NoError(t, store.SaveLLMDocument("home", faq))
	other := llm.Document{ID: "faq", Text: "Another collection, same ID.", Model: "text-embedding-3-small", Embedding: llm.Embedding{1, 0}, CreatedAt: at}
	require.NoError(t, store.SaveLLMDocument("work", other))

	loaded, err = store.LoadLLMDocuments()
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	require.Len(t, loaded["home"], 2)
	got := loaded["home"][0] // oldest first
	assert.Equal(t, manual.ID, got.ID)
	assert.Equal(t, manual.Text, got.Text)
	assert.Equal(t, manual.Metadata, got.Metadata)
	assert.Equal(t, manual.Model, got.Model)
	assert.Equal(t, manual.Embedding, got.Embedding)
	assert.True(t, manual.CreatedAt.Equal(got.CreatedAt))
	assert.Nil(t, loaded["home"][1].Metadata, "no metadata stays none")
	require.Len(t, loaded["work"], 1)
	assert.Equal(t, other.Embedding, loaded["work"][0].Embedding)

	require.NoError(t, store.DeleteLLMDocument("home", "faq"))
	require.NoError(t, store.DeleteLLMCollection("work"))
	loaded, err = store.LoadLLMDocuments()
	require.NoError(t, err)
	assert.Len(t, loaded, 1)
	assert.Len(t, loaded["home"], 1)
	require.NoError(t, store.Close())

	// Through llm.Knowledge, across a restart.
	store, err = NewSQLiteStore(path)
	require.NoError(t, err)
	defer store.Close()
	k := llm.NewKnowledge()
	require.NoError(t, k.Attach(store))
	hits, err := k.Search("home", "nomic-embed-text", llm.Embedding{0.6, 0.8, 0}, 4, 0)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, "manual#0", hits[0].ID)
	assert.InDelta(t, 1.0, hits[0].Score, 1e-6)

	require.NoError(t, k.Add("home", llm.Document{ID: "new", Text: "x", Model: "nomic-embed-text", Embedding: llm.Embedding{0, 3, 4}}))
	loaded, err = store.LoadLLMDocuments()
	require.NoError(t, err)
	require.Len(t, loaded["home"], 2, "Add writes through")
	require.NoError(t, k.DeleteCollection("home"))
	loaded, err = store.LoadLLMDocuments()
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

// A row whose embedding or metadata cannot be read is left out of the load:
// one damaged document does not stop PiWorker.
func TestLLMDocumentsSkipUnreadableRows(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "bad.db"))
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.SaveLLMDocument("c", llm.Document{ID: "good", Text: "ok", Model: "m", Embedding: llm.Embedding{1, 0}, CreatedAt: time.Now()}))
	_, err = store.DB().Exec(`INSERT INTO llm_documents (collection, id, text, metadata, model, dimensions, embedding, created_at)
		VALUES ('c', 'short', 't', '{}', 'm', 2, x'0000', ?), ('c', 'meta', 't', 'not json', 'm', 2, ?, ?)`,
		time.Now(), encodeEmbedding(llm.Embedding{0, 1}), time.Now())
	require.NoError(t, err)

	loaded, err := store.LoadLLMDocuments()
	require.NoError(t, err)
	require.Len(t, loaded["c"], 1)
	assert.Equal(t, "good", loaded["c"][0].ID)
}
