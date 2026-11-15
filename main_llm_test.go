package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests here go through the process-wide LLM stores (processDeps names
// them), so the connections they create carry a pw_test_ name and are
// deleted when they end.

// testLLMConnection stores an OpenAI-compatible connection to baseURL in
// llm.DefaultStore under a unique name and deletes it when t ends.
func testLLMConnection(t *testing.T, baseURL, apiKey string) string {
	t.Helper()
	name := "pw_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	require.NoError(t, llm.DefaultStore.Set(llm.Connection{
		Name: name, Provider: llm.ProviderOpenAI, BaseURL: baseURL, APIKey: apiKey, Model: "fake-model",
	}))
	t.Cleanup(func() { _ = llm.DefaultStore.Delete(name) })
	return name
}

// llmRoutes are a safe and an unsafe request of each LLM route group:
// connections, conversations, knowledge and the flow assistant.
var llmRoutes = []struct{ method, path, body string }{
	{"GET", "/api/llm/connections", ""},
	{"PUT", "/api/llm/connections/pw_test_route", `{"provider":"openai","baseUrl":"http://127.0.0.1:1/v1"}`},
	{"GET", "/api/llm/conversations", ""},
	{"DELETE", "/api/llm/conversations/pw_test_route", ""},
	{"GET", "/api/llm/knowledge", ""},
	{"DELETE", "/api/llm/knowledge/pw_test_route", ""},
	{"POST", "/api/llm/assistant/flow", `{}`},
}

// The LLM routes are mounted before the web UI catch-all: they answer the
// API's JSON envelope, not the editor's page.
func TestLLMRoutesAreMounted(t *testing.T) {
	app := newTestApp(t, false)
	t.Cleanup(func() { _ = llm.DefaultStore.Delete("pw_test_route") })
	for _, path := range []string{"/api/llm/connections", "/api/llm/conversations", "/api/llm/knowledge"} {
		w := app.do("GET", path, "", "", nil)
		require.Equalf(t, http.StatusOK, w.Code, "GET %s: %s", path, w.Body.String())
		assert.Containsf(t, w.Header().Get("Content-Type"), "application/json", "GET %s", path)
		assert.Containsf(t, w.Body.String(), `"success":true`, "GET %s", path)
	}

	w := app.do("PUT", "/api/llm/connections/pw_test_route", "application/json", `{"provider":"openai","baseUrl":"http://127.0.0.1:1/v1"}`, editorHeaders)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, ok := llm.DefaultStore.Get("pw_test_route")
	assert.True(t, ok, "the handler writes the process-wide store the nodes read")

	// The assistant refuses a request without a connection (400), which
	// only its handler answers.
	w = app.do("POST", "/api/llm/assistant/flow", "application/json", `{"description":"turn on a lamp"}`, editorHeaders)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"success":false`)

	w = app.do("DELETE", "/api/llm/knowledge/pw_test_nothing_here", "", "", editorHeaders)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"success":false`)
}

// The LLM routes sit behind the same guards as the rest of /api: the
// session, the CSRF request guard on unsafe methods and the Host
// allowlist.
func TestLLMRoutesAreGuardedLikeTheAPI(t *testing.T) {
	app := newTestApp(t, true)
	t.Cleanup(func() { _ = llm.DefaultStore.Delete("pw_test_route") })
	headers := app.login(t)
	for _, rt := range llmRoutes {
		ct := ""
		if rt.body != "" {
			ct = "application/json"
		}
		w := app.do(rt.method, rt.path, ct, rt.body, editorHeaders)
		assert.Equalf(t, http.StatusUnauthorized, w.Code, "%s %s without a session: %s", rt.method, rt.path, w.Body.String())

		if rt.method != "GET" {
			w = app.do(rt.method, rt.path, ct, rt.body, withoutHeader(headers, "X-PiWorker-Request"))
			assert.Equalf(t, http.StatusForbidden, w.Code, "%s %s without the CSRF header: %s", rt.method, rt.path, w.Body.String())
		}
		if rt.body != "" {
			w = app.do(rt.method, rt.path, "text/plain", rt.body, headers)
			assert.Equalf(t, http.StatusUnsupportedMediaType, w.Code, "%s %s with a text/plain body", rt.method, rt.path)
		}

		r := httptest.NewRequest(rt.method, "http://attacker.example"+rt.path, strings.NewReader(rt.body))
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		hw := httptest.NewRecorder()
		app.handler.ServeHTTP(hw, r)
		assert.Equalf(t, http.StatusForbidden, hw.Code, "%s %s from an unknown Host", rt.method, rt.path)
	}
	_, ok := llm.DefaultStore.Get("pw_test_route")
	assert.False(t, ok, "no refused request stored a connection")

	w := app.do("GET", "/api/llm/connections", "", "", headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// LLM connections are among the secret uses (Secrets dialog) and in the
// stores export.
func TestLLMConnectionsInSecretUsesAndStoresExport(t *testing.T) {
	app := newTestApp(t, false)
	name := testLLMConnection(t, "http://127.0.0.1:1/v1", "{{secret.PW_TEST_LLM_KEY}}")

	w := app.do("GET", "/api/secrets?usage=1", "", "", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var secretsList struct {
		Data struct {
			Undefined []struct {
				Name string `json:"name"`
				Uses []struct {
					LLMConnection string `json:"llmConnection"`
					Setting       string `json:"setting"`
				} `json:"uses"`
			} `json:"undefined"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &secretsList))
	var found bool
	for _, u := range secretsList.Data.Undefined {
		if u.Name == "PW_TEST_LLM_KEY" {
			found = true
			require.Len(t, u.Uses, 1)
			assert.Equal(t, name, u.Uses[0].LLMConnection)
			assert.Equal(t, "apiKey", u.Uses[0].Setting)
		}
	}
	assert.True(t, found, "the connection's key reference is a use: %s", w.Body.String())

	w = app.do("POST", "/api/stores/export", "application/json", `{}`, editorHeaders)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var bundle struct {
		LLMConnections []struct {
			Name    string `json:"name"`
			BaseURL string `json:"baseUrl"`
		} `json:"llmConnections"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &bundle))
	var exported bool
	for _, c := range bundle.LLMConnections {
		exported = exported || c.Name == name
	}
	assert.True(t, exported, "the export carries the LLM connections: %s", w.Body.String())
	assert.NotContains(t, w.Body.String(), "PW_TEST_LLM_KEY", "never the key, not even a reference, outside the sealed part")
}

// Deleting an LLM connection a running flow uses is refused through the
// lifecycle path the router wires.
func TestDeletingAnLLMConnectionInUseThroughTheRouter(t *testing.T) {
	app := newTestApp(t, false)
	name := testLLMConnection(t, "http://127.0.0.1:1/v1", "")
	f := flow.NewFlow("Summaries")
	trigger := flow.NewNode("trigger-manual", flow.NodeCategoryInput)
	trigger.ID = "start"
	f.AddNode(*trigger)
	summary := flow.NewNode("process-llm", flow.NodeCategoryProcessing)
	summary.ID, summary.Config = "summary", map[string]any{"connection": name}
	f.AddNode(*summary)
	f.AddConnection(*flow.NewConnection("start", "output", "summary", "input"))
	require.NoError(t, app.store.CreateFlow(f))
	w := app.do("POST", "/api/flows/"+f.ID+"/deploy", "", "", editorHeaders)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = app.do("DELETE", "/api/llm/connections/"+name, "", "", editorHeaders)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), f.ID)
	_, ok := llm.DefaultStore.Get(name)
	assert.True(t, ok)

	require.Equal(t, http.StatusOK, app.do("POST", "/api/flows/"+f.ID+"/stop", "", "", editorHeaders).Code)
	w = app.do("DELETE", "/api/llm/connections/"+name, "", "", editorHeaders)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// fakeChatServer is an OpenAI-compatible chat completions server: the
// first request of each conversation is answered with a call of the tool
// it offers, the next one (which must carry the tool's result) with the
// text final.
type fakeChatServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []map[string]any
}

func newFakeChatServer(t *testing.T, args, final string) *fakeChatServer {
	t.Helper()
	f := &fakeChatServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(raw, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.requests = append(f.requests, req)
		f.mu.Unlock()
		messages, _ := req["messages"].([]any)
		last, _ := messages[len(messages)-1].(map[string]any)
		w.Header().Set("Content-Type", "application/json")
		if last["role"] == "tool" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "fake-model",
				"choices": []any{map[string]any{"finish_reason": "stop",
					"message": map[string]any{"role": "assistant", "content": final}}},
				"usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 5},
			})
			return
		}
		tools, _ := req["tools"].([]any)
		if len(tools) == 0 {
			http.Error(w, "no tool offered", http.StatusBadRequest)
			return
		}
		fn := tools[0].(map[string]any)["function"].(map[string]any)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "fake-model",
			"choices": []any{map[string]any{"finish_reason": "tool_calls",
				"message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{
					"id": "call_1", "type": "function",
					"function": map[string]any{"name": fn["name"], "arguments": args},
				}}}}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 3},
		})
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeChatServer) seen() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.requests...)
}

// An agent node in a deployed flow calls a tool of another flow through the
// tool service the composition root gives the runtime manager: the model
// asks for the garage tool, the garage flow runs and replies, the model
// gets the reply and answers. The agent's flow is itself a tool here, so
// its answer comes back through the editor's "Test tool" route.
func TestAgentNodeCallsAnotherFlowsToolEndToEnd(t *testing.T) {
	app := newTestApp(t, false)
	model := newFakeChatServer(t, `{"door":"left"}`, "The left door moved.")
	conn := testLLMConnection(t, model.URL+"/v1", "")

	garage := saveToolFlow(t, app, nil)
	garageTool := garage.Nodes[0].Config["toolName"].(string)
	w := app.do("POST", "/api/flows/"+garage.ID+"/deploy", "", "", editorHeaders)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	agent := flow.NewNode("process-llm-agent", flow.NodeCategoryProcessing)
	agent.ID = "agent"
	agent.Config = map[string]any{"connection": conn, "prompt": "Open the {{payload.door}} garage door.", "tools": []any{garageTool}, "timeout": 20}
	reply := flow.NewNode("action-reply", flow.NodeCategoryOutput)
	reply.ID = "reply"
	assistant := saveToolFlow(t, app, map[string]any{"timeout": 25}, *agent)
	assistant.Nodes = append(assistant.Nodes, *reply)
	assistant.AddConnection(*flow.NewConnection("agent", "output", "reply", "input"))
	require.NoError(t, app.store.UpdateFlow(assistant))
	w = app.do("POST", "/api/flows/"+assistant.ID+"/deploy", "", "", editorHeaders)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = app.do("POST", "/api/flows/"+assistant.ID+"/call/tool", "application/json", `{"args":{"door":"left"}}`, editorHeaders)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var call struct {
		Data flow.CallResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &call))
	require.Equal(t, flow.CallStatusCompleted, call.Data.Status, w.Body.String())
	assert.JSONEq(t, `"The left door moved."`, string(call.Data.Result))

	requests := model.seen()
	require.Len(t, requests, 2)
	tools := requests[0]["tools"].([]any)
	require.Len(t, tools, 1, "the agent offers the one tool it lists")
	assert.Equal(t, garageTool, tools[0].(map[string]any)["function"].(map[string]any)["name"])
	messages := requests[1]["messages"].([]any)
	result := messages[len(messages)-1].(map[string]any)
	assert.Equal(t, "tool", result["role"])
	assert.Equal(t, "call_1", result["tool_call_id"])
	assert.JSONEq(t, `{"door":"left"}`, result["content"].(string), "the garage flow's reply reaches the model")

	usage := llm.DefaultStore.UsageToday(conn)
	assert.Equal(t, int64(30), usage.InputTokens, "the agent's requests are recorded on the connection")
}

// attachLLM loads the LLM connections, conversations and knowledge base
// from the database and writes their changes through to it.
func TestAttachLLMPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm.db")
	store, err := storage.NewSQLiteStore(path)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	deps := appDeps{store: store, llm: llm.NewStore(), memory: llm.NewMemory(), knowledge: llm.NewKnowledge()}
	require.NoError(t, attachLLM(deps))

	require.NoError(t, deps.llm.Set(llm.Connection{Name: "local", Provider: llm.ProviderOpenAI, BaseURL: "http://127.0.0.1:11434/v1"}))
	require.NoError(t, deps.memory.Append("chat:1", 10, llm.Turn{Role: llm.RoleUser, Text: "hello", At: time.Now()}))
	require.NoError(t, deps.knowledge.Add("manuals", llm.Document{ID: "boiler", Text: "Reset with the red button.", Model: "embed", Embedding: llm.Embedding{1, 0}}))

	again := appDeps{store: store, llm: llm.NewStore(), memory: llm.NewMemory(), knowledge: llm.NewKnowledge()}
	require.NoError(t, attachLLM(again))
	_, ok := again.llm.Get("local")
	assert.True(t, ok)
	assert.Len(t, again.memory.Turns("chat:1"), 1)
	docs, total := again.knowledge.Documents("manuals", 0, 10)
	assert.Equal(t, 1, total)
	require.Len(t, docs, 1)
	assert.Equal(t, "boiler", docs[0].ID)
}
