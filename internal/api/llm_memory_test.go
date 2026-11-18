package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func llmMemoryRouter(t *testing.T, memory LLMConversations, knowledge KnowledgeBase) *mux.Router {
	t.Helper()
	router := mux.NewRouter()
	NewLLMMemoryHandler(memory, knowledge, zerolog.Nop()).RegisterRoutes(router)
	return router
}

// The editor path-escapes conversation keys (encodeURIComponent): keys hold
// ':' and '/', and the router matches the decoded path.
func conversationPath(key string) string {
	return "/api/llm/conversations/" + url.PathEscape(key)
}

func TestLLMConversationsListShowAndClear(t *testing.T) {
	memory := llm.NewMemory()
	at := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	require.NoError(t, memory.Append("telegram:42/main", 20,
		llm.Turn{Role: llm.RoleUser, Text: "hi", At: at}, llm.Turn{Role: llm.RoleAssistant, Text: "hello", At: at}))
	require.NoError(t, memory.Append("other", 20, llm.Turn{Role: llm.RoleUser, Text: "x", At: at}))
	router := llmMemoryRouter(t, memory, llm.NewKnowledge())

	rr, env := doJSON(t, router, http.MethodGet, "/api/llm/conversations", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	convs := env["data"].(map[string]any)["conversations"].([]any)
	require.Len(t, convs, 2)
	assert.Equal(t, "other", convs[0].(map[string]any)["key"])
	assert.Equal(t, "telegram:42/main", convs[1].(map[string]any)["key"])
	assert.Equal(t, float64(2), convs[1].(map[string]any)["turns"])
	assert.NotEmpty(t, convs[1].(map[string]any)["updatedAt"])

	rr, env = doJSON(t, router, http.MethodGet, conversationPath("telegram:42/main"), "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, map[string]any{"key": "telegram:42/main", "turns": []any{
		map[string]any{"role": "user", "text": "hi", "at": "2026-11-15T10:00:00Z"},
		map[string]any{"role": "assistant", "text": "hello", "at": "2026-11-15T10:00:00Z"},
	}}, env["data"])

	rr, _ = doJSON(t, router, http.MethodGet, conversationPath("telegram:43"), "")
	assert.Equal(t, http.StatusNotFound, rr.Code)
	rr, _ = doJSON(t, router, http.MethodGet, conversationPath(strings.Repeat("k", llm.MaxConversationKeyLength+1)), "")
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	rr, _ = doJSON(t, router, http.MethodDelete, conversationPath("telegram:42/main"), "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Nil(t, memory.Turns("telegram:42/main"))
	assert.NotNil(t, memory.Turns("other"), "only that conversation")
	rr, _ = doJSON(t, router, http.MethodDelete, conversationPath("telegram:42/main"), "")
	assert.Equal(t, http.StatusNotFound, rr.Code)

	require.NoError(t, memory.Clear("other"))
	rr, env = doJSON(t, router, http.MethodGet, "/api/llm/conversations", "")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, []any{}, env["data"].(map[string]any)["conversations"], "an empty list, never null")
}

// failingMemory fails its writes, as a persistence failure would.
type failingMemory struct{ *llm.Memory }

func (failingMemory) Clear(string) error { return errors.New("disk I/O error") }

func TestLLMConversationClearFailureIs500(t *testing.T) {
	memory := llm.NewMemory()
	require.NoError(t, memory.Append("k", 20, llm.Turn{Role: llm.RoleUser, Text: "x"}))
	router := llmMemoryRouter(t, failingMemory{memory}, llm.NewKnowledge())
	rr, env := doJSON(t, router, http.MethodDelete, conversationPath("k"), "")
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.NotContains(t, env["error"], "disk I/O", "internal errors are logged, not echoed")
}

func knowledgeWithDocs(t *testing.T) *llm.Knowledge {
	t.Helper()
	k := llm.NewKnowledge()
	for i, id := range []string{"manual.pdf#0", "manual.pdf#1", "faq/returns#0"} {
		require.NoError(t, k.Add("docs", llm.Document{ID: id, Text: "text " + id, Model: "nomic-embed-text",
			Metadata:  map[string]any{"source": "manual"},
			Embedding: llm.Embedding{1, float32(i)}, CreatedAt: time.Date(2026, 11, 15, 10, i, 0, 0, time.UTC)}))
	}
	require.NoError(t, k.Add("notes", llm.Document{ID: "n1", Text: "note", Model: "other-embed", Embedding: llm.Embedding{0, 1, 0}}))
	return k
}

func TestKnowledgeCollectionsAndDocuments(t *testing.T) {
	k := knowledgeWithDocs(t)
	router := llmMemoryRouter(t, llm.NewMemory(), k)

	rr, env := doJSON(t, router, http.MethodGet, "/api/llm/knowledge", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	cols := env["data"].(map[string]any)["collections"].([]any)
	require.Len(t, cols, 2)
	docs := cols[0].(map[string]any)
	assert.Equal(t, "docs", docs["name"])
	assert.Equal(t, float64(3), docs["documents"])
	assert.Equal(t, "nomic-embed-text", docs["model"])
	assert.Equal(t, float64(2), docs["dimensions"])
	assert.NotEmpty(t, docs["updatedAt"])

	rr, env = doJSON(t, router, http.MethodGet, "/api/llm/knowledge/docs?offset=1&limit=1", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, map[string]any{"total": float64(3), "documents": []any{map[string]any{
		"id": "manual.pdf#1", "text": "text manual.pdf#1", "metadata": map[string]any{"source": "manual"},
		"model": "nomic-embed-text", "createdAt": "2026-11-15T10:01:00Z"}}}, env["data"])
	assert.NotContains(t, rr.Body.String(), "embedding")

	rr, env = doJSON(t, router, http.MethodGet, "/api/llm/knowledge/docs", "")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Len(t, env["data"].(map[string]any)["documents"], 3, "the first page by default")
	rr, env = doJSON(t, router, http.MethodGet, "/api/llm/knowledge/docs?offset=10", "")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, []any{}, env["data"].(map[string]any)["documents"], "past the end: an empty page")
	rr, _ = doJSON(t, router, http.MethodGet, "/api/llm/knowledge/docs?limit=1000", "")
	assert.Equal(t, http.StatusOK, rr.Code, "a limit over 100 is capped")

	for _, q := range []string{"limit=abc", "limit=-1", "offset=-1", "offset=x"} {
		rr, _ = doJSON(t, router, http.MethodGet, "/api/llm/knowledge/docs?"+q, "")
		assert.Equal(t, http.StatusBadRequest, rr.Code, q)
	}
	rr, _ = doJSON(t, router, http.MethodGet, "/api/llm/knowledge/missing", "")
	assert.Equal(t, http.StatusNotFound, rr.Code)
	rr, _ = doJSON(t, router, http.MethodGet, "/api/llm/knowledge/bad%20name", "")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// Document IDs hold '#' and '/' (chunks are <id>#<n>): the editor
// path-escapes them.
func TestKnowledgeDeleteDocumentAndCollection(t *testing.T) {
	k := knowledgeWithDocs(t)
	router := llmMemoryRouter(t, llm.NewMemory(), k)

	path := "/api/llm/knowledge/docs/documents/" + url.PathEscape("faq/returns#0")
	require.Contains(t, path, "faq%2Freturns%230")
	rr, _ := doJSON(t, router, http.MethodDelete, path, "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	docs, total := k.Documents("docs", 0, 10)
	assert.Equal(t, 2, total)
	for _, d := range docs {
		assert.NotEqual(t, "faq/returns#0", d.ID)
	}
	rr, _ = doJSON(t, router, http.MethodDelete, "/api/llm/knowledge/docs/documents/"+url.PathEscape("manual.pdf#0"), "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	_, total = k.Documents("docs", 0, 10)
	assert.Equal(t, 1, total)

	rr, _ = doJSON(t, router, http.MethodDelete, "/api/llm/knowledge/missing/documents/x", "")
	assert.Equal(t, http.StatusNotFound, rr.Code)
	rr, _ = doJSON(t, router, http.MethodDelete, "/api/llm/knowledge/docs/documents/"+strings.Repeat("i", llm.MaxDocumentIDLength+1), "")
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	rr, _ = doJSON(t, router, http.MethodDelete, "/api/llm/knowledge/docs", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Len(t, k.Collections(), 1)
	assert.Equal(t, "notes", k.Collections()[0].Name)
	rr, _ = doJSON(t, router, http.MethodDelete, "/api/llm/knowledge/docs", "")
	assert.Equal(t, http.StatusNotFound, rr.Code)

	require.NoError(t, k.DeleteCollection("notes"))
	rr, env := doJSON(t, router, http.MethodGet, "/api/llm/knowledge", "")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, []any{}, env["data"].(map[string]any)["collections"], "an empty list, never null")
}

// failingKnowledge fails its writes, as a persistence failure would.
type failingKnowledge struct{ *llm.Knowledge }

func (failingKnowledge) Delete(string, string) error   { return errors.New("disk I/O error") }
func (failingKnowledge) DeleteCollection(string) error { return errors.New("disk I/O error") }

func TestKnowledgeDeleteFailureIs500(t *testing.T) {
	router := llmMemoryRouter(t, llm.NewMemory(), failingKnowledge{knowledgeWithDocs(t)})
	for _, path := range []string{"/api/llm/knowledge/docs", "/api/llm/knowledge/docs/documents/n"} {
		rr, env := doJSON(t, router, http.MethodDelete, path, "")
		assert.Equal(t, http.StatusInternalServerError, rr.Code, path)
		assert.NotContains(t, env["error"], "disk I/O", "internal errors are logged, not echoed")
	}
}
