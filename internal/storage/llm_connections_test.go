package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The SQLite store keeps the LLM connections (the API key as given: a
// {{secret.NAME}} reference or plain text, like the broker passwords), and
// an llm.Store attaches to it across a restart.
func TestLLMConnectionsPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm.db")
	store, err := NewSQLiteStore(path)
	require.NoError(t, err)

	loaded, err := store.LoadLLMConnections()
	require.NoError(t, err)
	assert.Empty(t, loaded)

	claude := llm.Connection{Name: "claude", Provider: llm.ProviderAnthropic, APIKey: "{{secret.anthropic}}", Model: "claude-opus-4-1", DailyTokenLimit: 200000}
	require.NoError(t, store.SaveLLMConnection(claude))
	claude.Model = "claude-sonnet-4-5"
	require.NoError(t, store.SaveLLMConnection(claude), "upsert")
	ollama := llm.Connection{Name: "ollama", Provider: llm.ProviderOpenAI, BaseURL: "http://192.168.1.20:11434/v1"}
	require.NoError(t, store.SaveLLMConnection(ollama))
	require.NoError(t, store.Close())

	store, err = NewSQLiteStore(path)
	require.NoError(t, err)
	defer store.Close()
	s := llm.NewStore()
	require.NoError(t, s.Attach(store))
	got, ok := s.Get("claude")
	require.True(t, ok)
	assert.Equal(t, claude, got)
	got, ok = s.Get("ollama")
	require.True(t, ok)
	assert.Equal(t, ollama, got)
	assert.Equal(t, []string{"claude", "ollama"}, s.Names())

	// The store writes through.
	require.NoError(t, s.Delete("claude"))
	loaded, err = store.LoadLLMConnections()
	require.NoError(t, err)
	assert.Equal(t, map[string]llm.Connection{"ollama": ollama}, loaded)
}

// Usage adds up per (day, connection); a day or connection without rows is
// zero. An attached llm.Store reads today's counters back after a restart,
// so a daily limit survives one.
func TestLLMUsagePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.db")
	store, err := NewSQLiteStore(path)
	require.NoError(t, err)

	u, err := store.LLMUsage("2026-11-15", "claude")
	require.NoError(t, err)
	assert.Equal(t, llm.Usage{}, u)

	require.NoError(t, store.AddLLMUsage("2026-11-15", "claude", llm.Usage{InputTokens: 100, OutputTokens: 20, Requests: 1}))
	require.NoError(t, store.AddLLMUsage("2026-11-15", "claude", llm.Usage{InputTokens: 5, OutputTokens: 7, Requests: 1}))
	require.NoError(t, store.AddLLMUsage("2026-11-14", "claude", llm.Usage{InputTokens: 1, Requests: 1}))
	require.NoError(t, store.AddLLMUsage("2026-11-15", "ollama", llm.Usage{Requests: 1}))

	u, err = store.LLMUsage("2026-11-15", "claude")
	require.NoError(t, err)
	assert.Equal(t, llm.Usage{InputTokens: 105, OutputTokens: 27, Requests: 2}, u)
	u, err = store.LLMUsage("2026-11-14", "claude")
	require.NoError(t, err)
	assert.Equal(t, llm.Usage{InputTokens: 1, Requests: 1}, u)
	u, err = store.LLMUsage("2026-11-15", "ollama")
	require.NoError(t, err)
	assert.Equal(t, llm.Usage{Requests: 1}, u)

	// Through the llm.Store, across a restart.
	conn := llm.Connection{Name: "budget", Provider: llm.ProviderOpenAI, DailyTokenLimit: 50}
	s := llm.NewStore()
	require.NoError(t, s.Attach(store))
	require.NoError(t, s.Set(conn))
	require.NoError(t, s.RecordUsage("budget", llm.Usage{InputTokens: 40, OutputTokens: 10, Requests: 1}))
	require.NoError(t, store.Close())

	store, err = NewSQLiteStore(path)
	require.NoError(t, err)
	defer store.Close()
	s = llm.NewStore()
	require.NoError(t, s.Attach(store))
	assert.Equal(t, llm.Usage{InputTokens: 40, OutputTokens: 10, Requests: 1}, s.UsageToday("budget"))
	assert.ErrorIs(t, s.CheckBudget(conn), llm.ErrBudgetExhausted)
	u, err = store.LLMUsage(time.Now().Format("2006-01-02"), "budget")
	require.NoError(t, err)
	assert.Equal(t, int64(50), u.Total())
}

// Every LLM persister method refuses a closed store instead of panicking.
func TestLLMPersistersRefuseAClosedStore(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "closed.db"))
	require.NoError(t, err)
	require.NoError(t, store.Close())

	_, err = store.LoadLLMConnections()
	assert.ErrorIs(t, err, ErrDatabaseClosed)
	assert.ErrorIs(t, store.SaveLLMConnection(llm.Connection{Name: "x"}), ErrDatabaseClosed)
	assert.ErrorIs(t, store.DeleteLLMConnection("x"), ErrDatabaseClosed)
	assert.ErrorIs(t, store.AddLLMUsage("2026-11-15", "x", llm.Usage{Requests: 1}), ErrDatabaseClosed)
	_, err = store.LLMUsage("2026-11-15", "x")
	assert.ErrorIs(t, err, ErrDatabaseClosed)

	_, err = store.LoadLLMConversations()
	assert.ErrorIs(t, err, ErrDatabaseClosed)
	assert.ErrorIs(t, store.SaveLLMConversation("k", nil), ErrDatabaseClosed)
	assert.ErrorIs(t, store.DeleteLLMConversation("k"), ErrDatabaseClosed)

	_, err = store.LoadLLMDocuments()
	assert.ErrorIs(t, err, ErrDatabaseClosed)
	assert.ErrorIs(t, store.SaveLLMDocument("c", llm.Document{ID: "d"}), ErrDatabaseClosed)
	assert.ErrorIs(t, store.DeleteLLMDocument("c", "d"), ErrDatabaseClosed)
	assert.ErrorIs(t, store.DeleteLLMCollection("c"), ErrDatabaseClosed)
}
