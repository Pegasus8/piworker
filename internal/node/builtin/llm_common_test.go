package builtin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers shared by the tests of every LLM node (process-llm, the
// classifier, the agent, transcription and the knowledge base): a fake
// OpenAI-compatible server and a context whose LLM stores are private to
// the test. Nothing reaches the network.

// fakeLLMModel is the model of the test connection; fakeLLMServed is the
// model the fake server says answered.
const (
	fakeLLMConnection = "test"
	fakeLLMModel      = "test-model"
	fakeLLMServed     = "served-model"
)

// fakeLLM is an OpenAI-compatible server. answer is called for each request
// with its index (0-based), path (e.g. "/v1/chat/completions") and decoded
// JSON body (nil for a body that is not JSON) and returns the status and
// the JSON body to answer.
type fakeLLM struct {
	srv    *httptest.Server
	answer func(n int, path string, body map[string]any) (int, any)

	mu       sync.Mutex
	paths    []string
	requests []map[string]any
}

// newFakeLLM starts a fake server, closed when the test ends.
func newFakeLLM(t *testing.T, answer func(n int, path string, body map[string]any) (int, any)) *fakeLLM {
	t.Helper()
	f := &fakeLLM{answer: answer}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		n := len(f.requests)
		f.paths = append(f.paths, r.URL.Path)
		f.requests = append(f.requests, body)
		f.mu.Unlock()
		status, out := f.answer(n, r.URL.Path, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// Requests returns the decoded bodies of the requests received so far.
func (f *fakeLLM) Requests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.requests...)
}

// Paths returns the paths of the requests received so far.
func (f *fakeLLM) Paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

// connection is the test connection to f (an openai connection with a
// default model and no limit).
func (f *fakeLLM) connection() llm.Connection {
	return llm.Connection{Name: fakeLLMConnection, Provider: llm.ProviderOpenAI, BaseURL: f.srv.URL + "/v1", Model: fakeLLMModel}
}

// llmTestEnv is a context whose LLM connection store, memory and knowledge
// base are the test's own.
type llmTestEnv struct {
	ctx       context.Context
	store     *llm.Store
	memory    *llm.Memory
	knowledge *llm.Knowledge
}

// env returns a context whose store holds f's connection (changed by
// edit, when given) and whose memory and knowledge base are empty.
func (f *fakeLLM) env(t *testing.T, edit ...func(*llm.Connection)) llmTestEnv {
	t.Helper()
	conn := f.connection()
	for _, e := range edit {
		e(&conn)
	}
	store := llm.NewStore()
	require.NoError(t, store.Set(conn))
	mem, kb := llm.NewMemory(), llm.NewKnowledge()
	ctx := llm.WithKnowledge(llm.WithMemory(llm.WithStore(context.Background(), store), mem), kb)
	return llmTestEnv{ctx: ctx, store: store, memory: mem, knowledge: kb}
}

// chatAnswer is a chat completion answering text.
func chatAnswer(text string, promptTokens, completionTokens int) map[string]any {
	return map[string]any{
		"model": fakeLLMServed,
		"choices": []any{map[string]any{
			"index":         0,
			"finish_reason": "stop",
			"message":       map[string]any{"role": "assistant", "content": text},
		}},
		"usage": map[string]any{"prompt_tokens": promptTokens, "completion_tokens": completionTokens},
	}
}

// fakeToolCall is one tool call of toolCallsAnswer.
type fakeToolCall struct{ id, name, arguments string }

// toolCallsAnswer is a chat completion asking for tool calls.
func toolCallsAnswer(calls ...fakeToolCall) map[string]any {
	list := make([]any, 0, len(calls))
	for _, c := range calls {
		list = append(list, map[string]any{
			"id": c.id, "type": "function",
			"function": map[string]any{"name": c.name, "arguments": c.arguments},
		})
	}
	return map[string]any{
		"model": fakeLLMServed,
		"choices": []any{map[string]any{
			"index":         0,
			"finish_reason": "tool_calls",
			"message":       map[string]any{"role": "assistant", "content": nil, "tool_calls": list},
		}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
	}
}

// apiError is an OpenAI error body.
func apiError(message string) map[string]any {
	return map[string]any{"error": map[string]any{"message": message}}
}

// requestMessages returns the "messages" of a chat completion request.
func requestMessages(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["messages"].([]any)
	require.True(t, ok, "messages: %v", body["messages"])
	out := make([]map[string]any, 0, len(raw))
	for _, m := range raw {
		out = append(out, m.(map[string]any))
	}
	return out
}

// llmFactories are the factories of the LLM nodes that call a model, each
// with a minimal valid config naming the test connection.
func llmFactories() map[string]struct {
	factory func(map[string]any) (node.Node, error)
	config  map[string]any
} {
	return map[string]struct {
		factory func(map[string]any) (node.Node, error)
		config  map[string]any
	}{
		"process-llm":          {NewLLMProcess, map[string]any{"connection": fakeLLMConnection}},
		"process-llm-classify": {NewLLMClassifyProcess, map[string]any{"connection": fakeLLMConnection, "categories": []any{"a", "b"}}},
		"process-llm-agent":    {NewLLMAgentProcess, map[string]any{"connection": fakeLLMConnection}},
	}
}

// Every LLM node names its connection to deploy (undefined_llm_connection).
func TestLLMNodesRequireTheirConnection(t *testing.T) {
	for typ, tc := range llmFactories() {
		t.Run(typ, func(t *testing.T) {
			n, err := tc.factory(tc.config)
			require.NoError(t, err)
			req, ok := n.(node.LLMConnectionRequirer)
			require.True(t, ok, "%s implements node.LLMConnectionRequirer", typ)
			assert.Equal(t, map[string]string{"connection": fakeLLMConnection}, req.RequiredLLMConnections())

			cfg := map[string]any{}
			for k, v := range tc.config {
				cfg[k] = v
			}
			delete(cfg, "connection")
			_, err = tc.factory(cfg)
			assert.ErrorContains(t, err, "connection is required")
			cfg["connection"] = "not a name!"
			_, err = tc.factory(cfg)
			assert.ErrorContains(t, err, "connection")
		})
	}
}

// The settings every LLM node shares are checked by its factory.
func TestLLMNodesCheckTheSharedSettings(t *testing.T) {
	bad := []struct {
		name  string
		key   string
		value any
		want  string
	}{
		{"timeout too long", "timeout", 601, "timeout must be between 1 and 600 seconds"},
		{"timeout zero", "timeout", 0, "timeout must be between 1 and 600 seconds"},
		{"timeout not whole", "timeout", 1.5, "timeout must be a whole number of seconds"},
		{"model on two lines", "model", "a\nb", "model"},
		{"model too long", "model", strings.Repeat("m", 257), "model"},
	}
	for typ, tc := range llmFactories() {
		for _, b := range bad {
			t.Run(typ+"/"+b.name, func(t *testing.T) {
				cfg := map[string]any{}
				for k, v := range tc.config {
					cfg[k] = v
				}
				cfg[b.key] = b.value
				_, err := tc.factory(cfg)
				assert.ErrorContains(t, err, b.want)
			})
		}
	}
}

// An LLM node never calls a model in a sandbox (a dry-run or a node test),
// whatever its context carries.
func TestLLMNodesRefuseToRunInASandbox(t *testing.T) {
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) { return 200, chatAnswer("x", 1, 1) })
	env := f.env(t)
	sandboxed := node.WithSandbox(env.ctx, node.NewSandbox(1<<20, 1<<20))
	for typ, tc := range llmFactories() {
		t.Run(typ, func(t *testing.T) {
			n, err := tc.factory(tc.config)
			require.NoError(t, err)
			_, err = n.Process(sandboxed, types.NewMessage("hello", types.DataTypeString))
			assert.ErrorIs(t, err, llm.ErrSandboxed)
		})
	}
	assert.Empty(t, f.Requests(), "nothing was sent")
}

// A connection the context's store does not define fails the message
// (permanently: retrying cannot define it).
func TestLLMNodesFailOnAnUndefinedConnection(t *testing.T) {
	ctx := llm.WithStore(context.Background(), llm.NewStore())
	for typ, tc := range llmFactories() {
		t.Run(typ, func(t *testing.T) {
			n, err := tc.factory(tc.config)
			require.NoError(t, err)
			_, err = n.Process(ctx, types.NewMessage("hello", types.DataTypeString))
			assert.ErrorIs(t, err, llm.ErrUndefined)
			assert.False(t, node.IsRetryable(err))
		})
	}
}

// Neither the node nor the connection names a model: llm.ErrNoModel, before
// anything is sent.
func TestLLMNodesNeedAModel(t *testing.T) {
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) { return 200, chatAnswer("x", 1, 1) })
	env := f.env(t, func(c *llm.Connection) { c.Model = "" })
	for typ, tc := range llmFactories() {
		t.Run(typ, func(t *testing.T) {
			n, err := tc.factory(tc.config)
			require.NoError(t, err)
			_, err = n.Process(env.ctx, types.NewMessage("hello", types.DataTypeString))
			assert.ErrorIs(t, err, llm.ErrNoModel)
		})
	}
	assert.Empty(t, f.Requests())
}

// The node's model wins over the connection's.
func TestLLMNodesUseTheirOwnModelFirst(t *testing.T) {
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) {
		return 200, chatAnswer(`{"category":"a","confidence":1}`, 1, 1)
	})
	env := f.env(t)
	for typ, tc := range llmFactories() {
		t.Run(typ, func(t *testing.T) {
			cfg := map[string]any{"model": "my-model"}
			for k, v := range tc.config {
				cfg[k] = v
			}
			n, err := tc.factory(cfg)
			require.NoError(t, err)
			_, err = n.Process(env.ctx, types.NewMessage("hello", types.DataTypeString))
			require.NoError(t, err)
			reqs := f.Requests()
			assert.Equal(t, "my-model", reqs[len(reqs)-1]["model"])
		})
	}
}

// Provider errors keep llm's classification: an overloaded provider is
// transient (the runtime's opt-in retries apply), a bad request and a spent
// budget are permanent.
func TestLLMNodesKeepTheErrorClassification(t *testing.T) {
	status := 503
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) { return status, apiError("overloaded") })
	env := f.env(t)
	for typ, tc := range llmFactories() {
		t.Run(typ, func(t *testing.T) {
			n, err := tc.factory(tc.config)
			require.NoError(t, err)
			msg := types.NewMessage("hello", types.DataTypeString)

			status = 503
			_, err = n.Process(env.ctx, msg)
			require.Error(t, err)
			assert.True(t, node.IsRetryable(err), "503 is transient: %v", err)

			status = 400
			_, err = n.Process(env.ctx, msg)
			var apiErr *llm.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, 400, apiErr.Status)
			assert.False(t, node.IsRetryable(err))
		})
	}
}

func TestLLMNodesStopAtTheBudget(t *testing.T) {
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) { return 200, chatAnswer("x", 1, 1) })
	env := f.env(t, func(c *llm.Connection) { c.DailyTokenLimit = 10 })
	require.NoError(t, env.store.RecordUsage(fakeLLMConnection, llm.Usage{InputTokens: 10, Requests: 1}))
	for typ, tc := range llmFactories() {
		t.Run(typ, func(t *testing.T) {
			n, err := tc.factory(tc.config)
			require.NoError(t, err)
			_, err = n.Process(env.ctx, types.NewMessage("hello", types.DataTypeString))
			assert.ErrorIs(t, err, llm.ErrBudgetExhausted)
			assert.False(t, node.IsRetryable(err))
		})
	}
	assert.Empty(t, f.Requests())
}

// The timeout bounds the node's whole call; running out of it says so.
func TestLLMNodesTimeOut(t *testing.T) {
	release := make(chan struct{})
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) {
		<-release
		return 200, chatAnswer("late", 1, 1)
	})
	defer close(release)
	env := f.env(t)
	n, err := NewLLMProcess(map[string]any{"connection": fakeLLMConnection, "timeout": 1})
	require.NoError(t, err)
	start := time.Now()
	_, err = n.Process(env.ctx, types.NewMessage("hello", types.DataTypeString))
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.Contains(t, err.Error(), "did not finish within 1 second")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.False(t, node.IsRetryable(err))
}

func TestCheckMessagePath(t *testing.T) {
	for _, ok := range []string{"payload", "payload.image", "payload.items.0", "meta.photo", "meta.a.b", "topic"} {
		assert.NoErrorf(t, checkMessagePath("images", ok), "%s", ok)
	}
	for _, bad := range []string{"image", "payload.", "payload..a", "meta", "meta.", "topic.x", "{{payload}}", "payload.a b", " payload"} {
		assert.Errorf(t, checkMessagePath("images", bad), "%s", bad)
	}
	err := checkMessagePath("images", "image")
	assert.ErrorContains(t, err, "images")
}

func TestParseResultField(t *testing.T) {
	for _, ok := range []string{"", "answer", "llm.answer", "a_b-c"} {
		field, err := parseResultField(node.NewBaseNode(map[string]any{"resultField": ok}))
		assert.NoErrorf(t, err, "%q", ok)
		assert.Equal(t, ok, field)
	}
	for _, bad := range []string{".a", "a.", "a..b", "{{payload.x}}", "a b", strings.Repeat("a", 129)} {
		_, err := parseResultField(node.NewBaseNode(map[string]any{"resultField": bad}))
		assert.Errorf(t, err, "%q", bad)
	}
}

func TestLLMMeta(t *testing.T) {
	m := llmMeta(fakeLLMConnection, "m", llm.Usage{InputTokens: 3, OutputTokens: 4, Requests: 2}, llm.StopEnd)
	assert.Equal(t, map[string]any{
		"connection": fakeLLMConnection, "model": "m", "inputTokens": int64(3), "outputTokens": int64(4),
		"requests": int64(2), "stop": "end",
	}, m)
}

// llmAnswerText is the payload a text answer leaves with.
func llmAnswerText(t *testing.T, outs []*types.Message) string {
	t.Helper()
	require.Len(t, outs, 1)
	s, ok := outs[0].Payload.(string)
	require.True(t, ok, "payload %T", outs[0].Payload)
	return s
}

// Effort is sent as reasoning_effort (OpenAI-compatible) or
// output_config.effort (Anthropic) whatever the model: a model without the
// setting may refuse the request, so the description must not promise
// that such models ignore it.
func TestEffortDescriptionDoesNotPromiseItIsIgnored(t *testing.T) {
	for _, typ := range []string{"process-llm", "process-llm-agent"} {
		info, ok := node.DefaultRegistry.Get(typ)
		require.True(t, ok, typ)
		d := info.Config.Properties["effort"].Description
		assert.NotContains(t, d, "ignore", typ)
		assert.Contains(t, d, "only for models that support it", typ)
		assert.Contains(t, d, "reject", typ)
	}
}
