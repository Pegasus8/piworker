package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A node test of an LLM node with a memoryKey runs as a fresh conversation:
// it neither reads nor writes the conversations of the process-wide memory
// (the variables preview never writes either).
func TestTestNodeLeavesTheConversationsAlone(t *testing.T) {
	var mu sync.Mutex
	var sent [][]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []any `json:"messages"`
		}
		_ = json.Unmarshal(raw, &req)
		mu.Lock()
		sent = append(sent, req.Messages)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"fake","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"Hello!"}}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`))
	}))
	t.Cleanup(srv.Close)
	require.NoError(t, llm.DefaultStore.Set(llm.Connection{Name: "pw_test_node_test", Provider: llm.ProviderOpenAI, BaseURL: srv.URL, Model: "fake"}))
	t.Cleanup(func() { _ = llm.DefaultStore.Delete("pw_test_node_test") })
	require.NoError(t, llm.DefaultMemory.Append("pw_test:node-test", 0, llm.Turn{Role: llm.RoleUser, Text: "an earlier question"},
		llm.Turn{Role: llm.RoleAssistant, Text: "an earlier answer"}))
	t.Cleanup(func() { _ = llm.DefaultMemory.Clear("pw_test:node-test") })

	h := NewNodeTypesHandler(node.DefaultRegistry, zerolog.Nop())
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	rr := postTestNode(t, router, "process-llm", map[string]any{
		"config":  map[string]any{"connection": "pw_test_node_test", "memoryKey": "pw_test:node-test"},
		"payload": "Say hello",
	})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), "Hello!")
	assert.NotContains(t, rr.Body.String(), `"error"`)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, sent, 1)
	assert.Len(t, sent[0], 1, "only the test's prompt is sent: no earlier turn")
	assert.Len(t, llm.DefaultMemory.Turns("pw_test:node-test"), 2, "the stored conversation is unchanged")
}

// LLM node types get a longer bound than the rest (a model can take a
// minute), and the answer's write deadline outlasts it.
func TestTestNodeTimeoutOfLLMTypes(t *testing.T) {
	for _, typ := range []string{"process-llm", "process-llm-classify", "process-llm-agent", "process-transcribe", "process-knowledge-search"} {
		info, ok := node.DefaultRegistry.Get(typ)
		require.True(t, ok, typ)
		assert.Equalf(t, llmTestNodeTimeout, testNodeTimeoutFor(info), "%s", typ)
	}
	for _, typ := range []string{"process-transform", "process-template", "process-delay"} {
		info, ok := node.DefaultRegistry.Get(typ)
		require.True(t, ok, typ)
		assert.Equalf(t, testNodeTimeout, testNodeTimeoutFor(info), "%s", typ)
	}
	assert.Greater(t, llmTestNodeTimeout, testNodeTimeout)
	assert.Greater(t, testNodeWriteDeadline, llmTestNodeTimeout)
}
