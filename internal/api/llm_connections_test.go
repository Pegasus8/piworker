package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeModels is an OpenAI-compatible server answering GET /models; it
// records the Authorization header of each request.
type fakeModels struct {
	mu     sync.Mutex
	auth   []string
	status int
	body   string
}

func newFakeModels(t *testing.T) (*fakeModels, *httptest.Server) {
	t.Helper()
	f := &fakeModels{status: http.StatusOK, body: `{"data":[{"id":"m2","name":"Model Two"},{"id":"m1"}]}`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		f.mu.Lock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		status, body := f.status, f.body
		f.mu.Unlock()
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeModels) answer(status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.body = status, body
}

func (f *fakeModels) lastAuth() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.auth) == 0 {
		return "<no request>"
	}
	return f.auth[len(f.auth)-1]
}

func llmConnectionsRouter(t *testing.T) (*mux.Router, *llm.Store, *secrets.Store) {
	t.Helper()
	store := llm.NewStore()
	sec := secrets.NewStore()
	router := mux.NewRouter()
	NewLLMConnectionsHandler(store, sec, zerolog.Nop()).RegisterRoutes(router)
	return router, store, sec
}

// The API key is write-only: set or replaced by PUT, kept when the request
// leaves it out, cleared by "", never returned (only hasApiKey). PUT answers
// the stored item.
func TestLLMConnectionsCRUDKeepsTheKeyWriteOnly(t *testing.T) {
	router, store, _ := llmConnectionsRouter(t)

	rr, env := doJSON(t, router, http.MethodPut, "/api/llm/connections/claude",
		`{"provider":"Anthropic","baseUrl":"","apiKey":"sk-ant-very-secret","model":"claude-x","dailyTokenLimit":5000}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.NotContains(t, rr.Body.String(), "sk-ant-very-secret")
	assert.Equal(t, map[string]any{"name": "claude", "provider": "anthropic", "baseUrl": "", "hasApiKey": true,
		"model": "claude-x", "dailyTokenLimit": float64(5000),
		"usageToday": map[string]any{"inputTokens": float64(0), "outputTokens": float64(0), "requests": float64(0)}}, env["data"])
	got, ok := store.Get("claude")
	require.True(t, ok)
	assert.Equal(t, "sk-ant-very-secret", got.APIKey)

	require.NoError(t, store.RecordUsage("claude", llm.Usage{InputTokens: 10, OutputTokens: 4, Requests: 1}))
	rr, env = doJSON(t, router, http.MethodGet, "/api/llm/connections", "")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.NotContains(t, rr.Body.String(), "sk-ant-very-secret")
	assert.Equal(t, []any{map[string]any{"name": "claude", "provider": "anthropic", "baseUrl": "", "hasApiKey": true,
		"model": "claude-x", "dailyTokenLimit": float64(5000),
		"usageToday": map[string]any{"inputTokens": float64(10), "outputTokens": float64(4), "requests": float64(1)}}},
		env["data"].(map[string]any)["connections"])

	// Without apiKey the stored one is kept; "" clears it (an OpenAI-compatible
	// server may need none).
	rr, _ = doJSON(t, router, http.MethodPut, "/api/llm/connections/claude",
		`{"provider":"openai","baseUrl":"http://10.0.0.5:11434/v1/","model":"llama3","dailyTokenLimit":0}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	got, _ = store.Get("claude")
	assert.Equal(t, "sk-ant-very-secret", got.APIKey)
	assert.Equal(t, "http://10.0.0.5:11434/v1", got.BaseURL, "normalized")
	rr, env = doJSON(t, router, http.MethodPut, "/api/llm/connections/claude",
		`{"provider":"openai","baseUrl":"http://10.0.0.5:11434/v1","apiKey":"","model":"llama3"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	got, _ = store.Get("claude")
	assert.Empty(t, got.APIKey)
	assert.Equal(t, false, env["data"].(map[string]any)["hasApiKey"])

	rr, _ = doJSON(t, router, http.MethodDelete, "/api/llm/connections/claude", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Empty(t, store.Names())
	rr, _ = doJSON(t, router, http.MethodDelete, "/api/llm/connections/claude", "")
	assert.Equal(t, http.StatusNotFound, rr.Code)

	rr, env = doJSON(t, router, http.MethodGet, "/api/llm/connections", "")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, []any{}, env["data"].(map[string]any)["connections"], "an empty list, never null")
}

// Invalid settings are refused with 400 and store nothing; no error quotes
// the key or the base URL (which could carry credentials pasted by mistake).
func TestLLMConnectionsRejectInvalidSettings(t *testing.T) {
	router, store, _ := llmConnectionsRouter(t)
	for _, c := range []struct{ path, body string }{
		{"/api/llm/connections/my%20claude", `{"provider":"anthropic","apiKey":"k"}`},
		{"/api/llm/connections/c", `{"provider":"gemini","apiKey":"k"}`},
		{"/api/llm/connections/c", `{"provider":"anthropic"}`},
		{"/api/llm/connections/c", `{"provider":"openai","baseUrl":"ftp://host/v1"}`},
		{"/api/llm/connections/c", `{"provider":"openai","baseUrl":"https://user:hunter2-pass@host/v1"}`},
		{"/api/llm/connections/c", `{"provider":"openai","dailyTokenLimit":-1}`},
		{"/api/llm/connections/c", `{"provider":"openai","dailyTokenLimit":1.5}`},
		{"/api/llm/connections/c", `{"provider":"openai","model":"a\nb","apiKey":"sk-leak-me"}`},
		{"/api/llm/connections/c", `not json`},
	} {
		rr, env := doJSON(t, router, http.MethodPut, c.path, c.body)
		assert.Equal(t, http.StatusBadRequest, rr.Code, c.body)
		assert.Equal(t, false, env["success"])
		assert.NotContains(t, rr.Body.String(), "hunter2-pass")
		assert.NotContains(t, rr.Body.String(), "sk-leak-me")
	}
	assert.Empty(t, store.Names())
}

// Test lists the models with the settings sent, the stored key when none is
// sent, and stores nothing. A failure is {ok: false, error} with the key
// removed from the provider's message.
func TestLLMConnectionsTestListsTheModelsWithTheSettingsSent(t *testing.T) {
	router, store, _ := llmConnectionsRouter(t)
	fake, srv := newFakeModels(t)
	require.NoError(t, store.Set(llm.Connection{Name: "local", Provider: llm.ProviderOpenAI,
		BaseURL: "http://192.0.2.1/v1", APIKey: "stored-key-123"}))

	rr, env := doJSON(t, router, http.MethodPost, "/api/llm/connections/local/test",
		`{"provider":"openai","baseUrl":"`+srv.URL+`/v1","model":"m1"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, map[string]any{"ok": true, "models": []any{
		map[string]any{"id": "m1"}, map[string]any{"id": "m2", "name": "Model Two"}}}, env["data"])
	assert.Equal(t, "Bearer stored-key-123", fake.lastAuth(), "the stored key when none is sent")
	got, _ := store.Get("local")
	assert.Equal(t, "http://192.0.2.1/v1", got.BaseURL, "nothing stored")

	// A new connection with its own key; the provider refuses it and quotes
	// it back: the answer has it removed.
	fake.answer(http.StatusUnauthorized, `{"error":{"message":"Incorrect API key provided: typed-key-456"}}`)
	rr, env = doJSON(t, router, http.MethodPost, "/api/llm/connections/new/test",
		`{"provider":"openai","baseUrl":"`+srv.URL+`/v1","apiKey":"typed-key-456"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "Bearer typed-key-456", fake.lastAuth())
	data := env["data"].(map[string]any)
	assert.Equal(t, false, data["ok"])
	assert.Contains(t, data["error"], "401")
	assert.NotContains(t, rr.Body.String(), "typed-key-456")
	_, exists := store.Get("new")
	assert.False(t, exists, "a test stores nothing")

	rr, _ = doJSON(t, router, http.MethodPost, "/api/llm/connections/new/test", `{"provider":"anthropic"}`)
	assert.Equal(t, http.StatusBadRequest, rr.Code, "settings are validated as for PUT")
}

// A {{secret.NAME}} key is resolved through the handler's secret store; an
// undefined secret fails the test naming the secret, never a value.
func TestLLMConnectionsTestResolvesSecretReferences(t *testing.T) {
	router, _, sec := llmConnectionsRouter(t)
	fake, srv := newFakeModels(t)
	require.NoError(t, sec.Set("OLLAMA_KEY", "from-the-secret-store"))

	rr, env := doJSON(t, router, http.MethodPost, "/api/llm/connections/local/test",
		`{"provider":"openai","baseUrl":"`+srv.URL+`/v1","apiKey":"{{secret.OLLAMA_KEY}}"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, true, env["data"].(map[string]any)["ok"])
	assert.Equal(t, "Bearer from-the-secret-store", fake.lastAuth())

	rr, env = doJSON(t, router, http.MethodPost, "/api/llm/connections/local/test",
		`{"provider":"openai","baseUrl":"`+srv.URL+`/v1","apiKey":"{{secret.MISSING}}"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	data := env["data"].(map[string]any)
	assert.Equal(t, false, data["ok"])
	assert.Contains(t, data["error"], `"MISSING"`)
}

// Models lists the models with the stored settings; an unknown connection
// is 404 and a provider failure 502 with the provider's (redacted) message.
func TestLLMConnectionModels(t *testing.T) {
	router, store, _ := llmConnectionsRouter(t)
	fake, srv := newFakeModels(t)
	require.NoError(t, store.Set(llm.Connection{Name: "local", Provider: llm.ProviderOpenAI, BaseURL: srv.URL + "/v1", APIKey: "kept-key-789"}))

	rr, env := doJSON(t, router, http.MethodGet, "/api/llm/connections/local/models", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, map[string]any{"models": []any{map[string]any{"id": "m1"}, map[string]any{"id": "m2", "name": "Model Two"}}}, env["data"])
	assert.Equal(t, "Bearer kept-key-789", fake.lastAuth())

	rr, _ = doJSON(t, router, http.MethodGet, "/api/llm/connections/nope/models", "")
	assert.Equal(t, http.StatusNotFound, rr.Code)

	fake.answer(http.StatusForbidden, `{"error":{"message":"key kept-key-789 is not allowed"}}`)
	rr, env = doJSON(t, router, http.MethodGet, "/api/llm/connections/local/models", "")
	assert.Equal(t, http.StatusBadGateway, rr.Code, rr.Body.String())
	assert.Contains(t, env["error"], "403")
	assert.NotContains(t, rr.Body.String(), "kept-key-789")

	fake.answer(http.StatusOK, `{"data":[]}`)
	rr, env = doJSON(t, router, http.MethodGet, "/api/llm/connections/local/models", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, map[string]any{"models": []any{}}, env["data"], "an empty list, never null")
}

// llmTrigger is a trigger whose settings name an LLM connection
// (node.LLMConnectionRequirer).
type llmTrigger struct {
	mockTriggerNode
	name string
}

func (n *llmTrigger) RequiredLLMConnections() map[string]string {
	return map[string]string{"connection": n.name}
}

var _ node.LLMConnectionRequirer = (*llmTrigger)(nil)

// An LLM connection a running flow uses cannot be deleted (409, naming the
// flows, decided under the lifecycle lock); once the flow stops it can.
// Deploy checks the connection in the store of its context, the process
// one here, hence the pw_test_ name.
func TestDeletingAnLLMConnectionARunningFlowUsesIsRefused(t *testing.T) {
	server, _ := createTestServer(t)
	t.Cleanup(server.handler.lifecycle.Runtimes().StopAll)
	require.NoError(t, server.handler.registry.Register(node.NodeTypeInfo{
		Type: "mock-llm", Name: "LLM", Category: flow.NodeCategoryInput, Risk: node.RiskExternal,
		Outputs: []flow.Port{{ID: "output", Name: "Output", DataType: flow.DataTypeAny}},
	}, func(cfg map[string]any) (node.Node, error) {
		name, _ := cfg["connection"].(string)
		return &llmTrigger{name: name}, nil
	}))
	conns := llm.DefaultStore
	require.NoError(t, conns.Set(llm.Connection{Name: "pw_test_llm", Provider: llm.ProviderOpenAI}))
	require.NoError(t, conns.Set(llm.Connection{Name: "pw_test_llm_other", Provider: llm.ProviderOpenAI}))
	t.Cleanup(func() { _ = conns.Delete("pw_test_llm"); _ = conns.Delete("pw_test_llm_other") })
	r := mux.NewRouter()
	NewLLMConnectionsHandler(conns, secrets.NewStore(), zerolog.Nop()).
		WithFlows(LLMConnectionUsersOf(server.handler.lifecycle)).RegisterRoutes(r)

	user := flow.NewFlow("assistant flow")
	n := flow.NewNode("mock-llm", flow.NodeCategoryInput)
	n.Config = map[string]any{"connection": "pw_test_llm"}
	n.Outputs = []types.Port{{ID: "output", Name: "Output", DataType: flow.DataTypeAny}}
	user.AddNode(*n)
	require.NoError(t, server.handler.store.CreateFlow(user))
	rr := performRequest(server, http.MethodPost, "/api/flows/"+user.ID+"/deploy", nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	rr, env := doJSON(t, r, http.MethodDelete, "/api/llm/connections/pw_test_llm", "")
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	assert.Contains(t, env["error"], `"assistant flow"`)
	assert.Equal(t, []any{map[string]any{"id": user.ID, "name": "assistant flow"}}, env["data"].(map[string]any)["flows"])
	_, ok := conns.Get("pw_test_llm")
	assert.True(t, ok, "not deleted")

	// Another connection is not in use.
	rr, _ = doJSON(t, r, http.MethodDelete, "/api/llm/connections/pw_test_llm_other", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	require.Equal(t, http.StatusOK, performRequest(server, http.MethodPost, "/api/flows/"+user.ID+"/stop", nil).Code)
	rr, _ = doJSON(t, r, http.MethodDelete, "/api/llm/connections/pw_test_llm", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	_, ok = conns.Get("pw_test_llm")
	assert.False(t, ok)
}

// The lifecycle adapter runs the deletion under the lifecycle lock: it
// waits for a lifecycle change in progress.
func TestLLMConnectionUsersTakesTheLifecycleLock(t *testing.T) {
	server, _ := createTestServer(t)
	users := LLMConnectionUsersOf(server.handler.lifecycle)
	held := server.handler.lifecycle.Hold()
	done := make(chan struct{})
	go func() {
		defer close(done)
		refs, err := users.UnlessRunningFlowsUseLLMConnection("x", func() error { return nil })
		assert.NoError(t, err)
		assert.Empty(t, refs)
	}()
	assert.Never(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond, "ran while the lifecycle lock was held")
	held.Release()
	<-done
}
