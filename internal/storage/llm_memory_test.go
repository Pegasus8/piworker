package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The SQLite store keeps each conversation's turns (text only) under its
// key, and an llm.Memory attaches to it across a restart.
func TestLLMConversationsPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := NewSQLiteStore(path)
	require.NoError(t, err)

	loaded, err := store.LoadLLMConversations()
	require.NoError(t, err)
	assert.Empty(t, loaded)

	at := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	first := []llm.Turn{{Role: llm.RoleUser, Text: "hola", At: at}}
	require.NoError(t, store.SaveLLMConversation("telegram:42", first))
	both := append(first, llm.Turn{Role: llm.RoleAssistant, Text: "¡Hola! ¿En qué te ayudo?", At: at.Add(time.Second)})
	require.NoError(t, store.SaveLLMConversation("telegram:42", both), "upsert")
	require.NoError(t, store.SaveLLMConversation("other", []llm.Turn{{Role: llm.RoleUser, Text: "x", At: at}}))

	loaded, err = store.LoadLLMConversations()
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	require.Len(t, loaded["telegram:42"], 2)
	for i, turn := range loaded["telegram:42"] {
		assert.Equal(t, both[i].Role, turn.Role)
		assert.Equal(t, both[i].Text, turn.Text)
		assert.True(t, both[i].At.Equal(turn.At), "time kept: %v != %v", both[i].At, turn.At)
	}

	require.NoError(t, store.DeleteLLMConversation("other"))
	require.NoError(t, store.DeleteLLMConversation("missing"), "deleting nothing is not an error")
	require.NoError(t, store.Close())

	// Through llm.Memory, across a restart: Append writes through.
	store, err = NewSQLiteStore(path)
	require.NoError(t, err)
	defer store.Close()
	m := llm.NewMemory()
	require.NoError(t, m.Attach(store))
	assert.Len(t, m.Turns("telegram:42"), 2)
	assert.Nil(t, m.Turns("other"))
	require.NoError(t, m.Append("telegram:42", 2, llm.Turn{Role: llm.RoleUser, Text: "adiós"}))
	loaded, err = store.LoadLLMConversations()
	require.NoError(t, err)
	require.Len(t, loaded["telegram:42"], 2, "kept the last 2")
	assert.Equal(t, "adiós", loaded["telegram:42"][1].Text)
	require.NoError(t, m.Clear("telegram:42"))
	loaded, err = store.LoadLLMConversations()
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

// A row whose turns cannot be read (a damaged or hand-edited database) is
// left out of the load: one bad conversation does not stop PiWorker.
func TestLLMConversationsSkipUnreadableRows(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "bad.db"))
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.SaveLLMConversation("good", []llm.Turn{{Role: llm.RoleUser, Text: "ok", At: time.Now()}}))
	_, err = store.DB().Exec(`INSERT INTO llm_conversations (key, turns, updated_at) VALUES ('bad', 'not json', ?)`, time.Now())
	require.NoError(t, err)

	loaded, err := store.LoadLLMConversations()
	require.NoError(t, err)
	assert.Len(t, loaded, 1)
	assert.Contains(t, loaded, "good")
}
