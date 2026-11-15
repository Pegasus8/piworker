package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2"

	"github.com/Pegasus8/piworker/internal/node"
)

// sseMessage scripts one streamed Messages API answer.
type sseMessage struct {
	status      int    // non-200: an error answer with errBody
	errBody     string // JSON error body
	model       string
	blocks      []string // content_block_start payloads (each a full content block)
	textDeltas  map[int][]string
	jsonDeltas  map[int][]string
	stopReason  string
	stopDetails string // raw JSON or ""
	inputTokens int
	outTokens   int
	midError    string // an "error" event sent after message_start
	cut         bool   // the stream ends after the content blocks (no message_stop)
}

// fakeAnthropic speaks the Messages API's SSE streaming format and the
// models list.
type fakeAnthropic struct {
	t      *testing.T
	mu     sync.Mutex
	bodies []map[string]any
	header []http.Header
	script []sseMessage
	models []string // pages of /v1/models answers
}

func newFakeAnthropic(t *testing.T) (*fakeAnthropic, *httptest.Server) {
	f := &fakeAnthropic{t: t}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeAnthropic) lastBody() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[len(f.bodies)-1]
}

func (f *fakeAnthropic) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.header = append(f.header, r.Header.Clone())
	if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
		page := `{"data":[],"has_more":false}`
		if len(f.models) > 0 {
			page, f.models = f.models[0], f.models[1:]
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, page)
		return
	}
	if r.URL.Path != "/v1/messages" {
		f.mu.Unlock()
		f.t.Errorf("unexpected path %s", r.URL.Path)
		w.WriteHeader(404)
		return
	}
	var body map[string]any
	data, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(data, &body); err != nil {
		f.t.Errorf("request body: %v", err)
	}
	f.bodies = append(f.bodies, body)
	if len(f.script) == 0 {
		f.mu.Unlock()
		f.t.Errorf("no scripted answer")
		w.WriteHeader(500)
		return
	}
	m := f.script[0]
	f.script = f.script[1:]
	f.mu.Unlock()

	if m.status != 0 && m.status != 200 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(m.status)
		_, _ = io.WriteString(w, m.errBody)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	model := m.model
	if model == "" {
		model = "claude-test"
	}
	event := func(name, data string) {
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)
	}
	event("message_start", fmt.Sprintf(`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":%q,"content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":%d,"output_tokens":1}}}`, model, m.inputTokens))
	if m.midError != "" {
		event("error", m.midError)
		return
	}
	for i, block := range m.blocks {
		event("content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":%s}`, i, block))
		for _, d := range m.textDeltas[i] {
			b, _ := json.Marshal(d)
			event("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"text_delta","text":%s}}`, i, b))
		}
		for _, d := range m.jsonDeltas[i] {
			b, _ := json.Marshal(d)
			event("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`, i, b))
		}
		event("content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, i))
	}
	if m.cut {
		return
	}
	details := "null"
	if m.stopDetails != "" {
		details = m.stopDetails
	}
	event("message_delta", fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q,"stop_sequence":null,"stop_details":%s},"usage":{"output_tokens":%d}}`, m.stopReason, details, m.outTokens))
	event("message_stop", `{"type":"message_stop"}`)
}

func newTestAnthropic(t *testing.T, srv *httptest.Server) Client {
	t.Helper()
	c, err := NewClient(context.Background(), Connection{Name: "claude", Provider: ProviderAnthropic, BaseURL: srv.URL, APIKey: "sk-ant-test"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAnthropicChatText(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-from-env")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	f, srv := newFakeAnthropic(t)
	f.script = []sseMessage{{
		blocks:      []string{`{"type":"text","text":""}`},
		textDeltas:  map[int][]string{0: {"Hello ", "there"}},
		stopReason:  "end_turn",
		inputTokens: 11, outTokens: 4,
	}}
	c := newTestAnthropic(t, srv)
	resp, err := c.Chat(context.Background(), Request{
		Model: "claude-opus-5-5", System: "Be brief.", MaxTokens: 64000, Effort: "high", Cache: true,
		Messages: []Message{
			{Role: RoleUser, Text: "earlier"},
			{Role: RoleAssistant, Text: "answer"},
			{Role: RoleUser, Text: "Look", Images: []Media{{MediaType: "image/png", Data: pngBytes}, {URL: "https://example.com/x.jpg"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "Hello there" || resp.Stop != StopEnd || resp.Model != "claude-test" || resp.Usage != (Usage{11, 4, 1}) {
		t.Fatalf("response %+v", resp)
	}
	if _, ok := resp.Message.Native.(anthropic.MessageParam); !ok || resp.Message.Role != RoleAssistant || resp.Message.Text != "Hello there" {
		t.Fatalf("message %+v", resp.Message)
	}

	h := f.header[0]
	if got := h.Get("X-Api-Key") + h.Get("Authorization"); !strings.Contains(got, "sk-ant-test") || strings.Contains(got, "from-env") {
		t.Fatalf("credentials sent: %q", got)
	}
	body := f.lastBody()
	if body["model"] != "claude-opus-5-5" || body["max_tokens"] != float64(64000) || body["stream"] != true {
		t.Fatalf("body %v", body)
	}
	for _, k := range []string{"temperature", "top_p", "top_k", "thinking"} {
		if _, ok := body[k]; ok {
			t.Errorf("unexpected %s", k)
		}
	}
	if body["cache_control"].(map[string]any)["type"] != "ephemeral" {
		t.Fatalf("cache_control %v", body["cache_control"])
	}
	if oc := body["output_config"].(map[string]any); oc["effort"] != "high" || oc["format"] != nil {
		t.Fatalf("output_config %v", oc)
	}
	system := body["system"].([]any)
	if len(system) != 1 || system[0].(map[string]any)["text"] != "Be brief." {
		t.Fatalf("system %v", system)
	}
	msgs := body["messages"].([]any)
	if len(msgs) != 3 || msgs[1].(map[string]any)["role"] != "assistant" {
		t.Fatalf("messages %v", msgs)
	}
	content := msgs[2].(map[string]any)["content"].([]any)
	if len(content) != 3 {
		t.Fatalf("content %v", content)
	}
	img := content[0].(map[string]any)["source"].(map[string]any)
	if img["type"] != "base64" || img["media_type"] != "image/png" || img["data"] == "" {
		t.Fatalf("base64 image %v", img)
	}
	if src := content[1].(map[string]any)["source"].(map[string]any); src["type"] != "url" || src["url"] != "https://example.com/x.jpg" {
		t.Fatalf("URL image %v", src)
	}
	if content[2].(map[string]any)["text"] != "Look" {
		t.Fatalf("text after the images %v", content[2])
	}
}

func TestAnthropicToolRoundTripReplaysNative(t *testing.T) {
	f, srv := newFakeAnthropic(t)
	f.script = []sseMessage{
		{
			blocks: []string{
				`{"type":"thinking","thinking":"","signature":""}`,
				`{"type":"text","text":"Let me check."}`,
				`{"type":"tool_use","id":"toolu_1","name":"lights","input":{}}`,
			},
			jsonDeltas: map[int][]string{2: {`{"on": `, `true}`}},
			stopReason: "tool_use", inputTokens: 20, outTokens: 9,
		},
		{
			blocks:     []string{`{"type":"text","text":"Done."}`},
			stopReason: "end_turn", inputTokens: 40, outTokens: 2,
		},
	}
	c := newTestAnthropic(t, srv)
	tools := []Tool{{Name: "lights", Description: "Switch the lights", InputSchema: json.RawMessage(`{"type":"object","properties":{"on":{"type":"boolean"}},"required":["on"],"additionalProperties":false}`)}}
	req := Request{Model: "claude-opus-5-5", MaxTokens: 1000, Tools: tools, Cache: true, Messages: []Message{{Role: RoleUser, Text: "lights on"}}}
	resp, err := c.Chat(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Stop != StopToolUse || len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "toolu_1" || resp.ToolCalls[0].Name != "lights" {
		t.Fatalf("tool calls %+v", resp.ToolCalls)
	}
	var input map[string]any
	if err := json.Unmarshal(resp.ToolCalls[0].Input, &input); err != nil || input["on"] != true {
		t.Fatalf("input %s", resp.ToolCalls[0].Input)
	}
	tool := f.lastBody()["tools"].([]any)[0].(map[string]any)
	schema := tool["input_schema"].(map[string]any)
	if tool["name"] != "lights" || tool["description"] != "Switch the lights" || schema["type"] != "object" || schema["additionalProperties"] != false || schema["required"].([]any)[0] != "on" {
		t.Fatalf("tool %v", tool)
	}

	req.Messages = append(req.Messages, resp.Message, Message{Role: RoleUser, ToolResults: []ToolResult{{CallID: "toolu_1", Content: `{"ok":true}`}, {CallID: "toolu_x", Content: "boom", IsError: true}}})
	resp, err = c.Chat(context.Background(), req)
	if err != nil || resp.Text != "Done." {
		t.Fatalf("second answer %+v %v", resp, err)
	}
	msgs := f.lastBody()["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages %v", msgs)
	}
	replayed := msgs[1].(map[string]any)["content"].([]any)
	if len(replayed) != 3 || replayed[0].(map[string]any)["type"] != "thinking" || replayed[2].(map[string]any)["type"] != "tool_use" {
		t.Fatalf("assistant not replayed unchanged: %v", replayed)
	}
	results := msgs[2].(map[string]any)["content"].([]any)
	if len(results) != 2 {
		t.Fatalf("tool results %v", results)
	}
	r0, r1 := results[0].(map[string]any), results[1].(map[string]any)
	if r0["type"] != "tool_result" || r0["tool_use_id"] != "toolu_1" || r1["is_error"] != true {
		t.Fatalf("tool results %v", results)
	}
}

func TestAnthropicStructuredOutput(t *testing.T) {
	f, srv := newFakeAnthropic(t)
	f.script = []sseMessage{
		{blocks: []string{`{"type":"text","text":""}`}, textDeltas: map[int][]string{0: {`{"category":`, `"b","confidence":0.7}`}}, stopReason: "end_turn"},
		{blocks: []string{`{"type":"text","text":"{\"category\":\"nope\",\"confidence\":2}"}`}, stopReason: "end_turn"},
		{blocks: []string{`{"type":"text","text":"{\"category\":"}`}, stopReason: "max_tokens"},
	}
	c := newTestAnthropic(t, srv)
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"category": map[string]any{"enum": []any{"a", "b"}}, "confidence": map[string]any{"type": "number", "maximum": 1}},
		"required":   []any{"category", "confidence"},
	}
	req := Request{Model: "claude-opus-5-5", MaxTokens: 256, Schema: schema, Messages: []Message{{Role: RoleUser, Text: "classify"}}}
	resp, err := c.Chat(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if v := resp.JSON.(map[string]any); v["category"] != "b" {
		t.Fatalf("json %#v", resp.JSON)
	}
	format := f.lastBody()["output_config"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" || format["schema"].(map[string]any)["additionalProperties"] != false {
		t.Fatalf("format %v", format)
	}
	if _, err := c.Chat(context.Background(), req); !errors.Is(err, ErrInvalidAnswer) {
		t.Fatalf("answer outside the schema: %v", err)
	}
	if _, err := c.Chat(context.Background(), req); !errors.Is(err, ErrInvalidAnswer) || !strings.Contains(err.Error(), "maxTokens") {
		t.Fatalf("cut answer: %v", err)
	}
}

func TestAnthropicRefusal(t *testing.T) {
	f, srv := newFakeAnthropic(t)
	f.script = []sseMessage{{
		blocks:      []string{`{"type":"text","text":""}`},
		stopReason:  "refusal",
		stopDetails: `{"type":"refusal","category":"cyber","explanation":"This request was declined."}`,
		inputTokens: 5, outTokens: 0,
	}}
	store := NewStore()
	conn := Connection{Name: "claude", Provider: ProviderAnthropic, BaseURL: srv.URL, APIKey: "k"}
	_ = store.Set(conn)
	ctx := WithStore(context.Background(), store)
	c, err := NewClient(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Chat(ctx, Request{Model: "m", MaxTokens: 10, Messages: []Message{{Role: RoleUser, Text: "x"}}})
	if !errors.Is(err, ErrRefused) || node.IsRetryable(err) || !strings.Contains(err.Error(), "cyber") {
		t.Fatalf("refusal: %v", err)
	}
	if u := store.UsageToday("claude"); u.InputTokens != 5 || u.Requests != 1 {
		t.Fatalf("refusal usage %+v", u)
	}
}

func TestAnthropicErrors(t *testing.T) {
	f, srv := newFakeAnthropic(t)
	f.script = []sseMessage{
		{status: 429, errBody: `{"type":"error","error":{"type":"rate_limit_error","message":"Number of request tokens has exceeded your rate limit"}}`},
		{status: 401, errBody: `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key sk-ant-test"}}`},
		{status: 529, errBody: `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`},
		{midError: `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`},
	}
	c := newTestAnthropic(t, srv)
	req := Request{Model: "m", MaxTokens: 10, Messages: []Message{{Role: RoleUser, Text: "x"}}}

	_, err := c.Chat(context.Background(), req)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 429 || !node.IsRetryable(err) || !strings.Contains(apiErr.Message, "rate limit") {
		t.Fatalf("429: %v", err)
	}
	_, err = c.Chat(context.Background(), req)
	if !errors.As(err, &apiErr) || apiErr.Status != 401 || node.IsRetryable(err) {
		t.Fatalf("401: %v", err)
	}
	if strings.Contains(err.Error(), "sk-ant-test") || !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("401 not redacted: %v", err)
	}
	_, err = c.Chat(context.Background(), req)
	if !errors.As(err, &apiErr) || apiErr.Status != 529 || !node.IsRetryable(err) {
		t.Fatalf("529: %v", err)
	}
	_, err = c.Chat(context.Background(), req)
	if !node.IsRetryable(err) || !errors.As(err, &apiErr) || apiErr.Status != 529 {
		t.Fatalf("overloaded mid-stream: %v", err)
	}
}

func TestAnthropicModelsAndUnsupported(t *testing.T) {
	f, srv := newFakeAnthropic(t)
	f.models = []string{
		`{"data":[{"type":"model","id":"claude-opus-5-5","display_name":"Claude Opus 5.5","created_at":"2026-09-01T00:00:00Z"}],"has_more":true,"first_id":"claude-opus-5-5","last_id":"claude-opus-5-5"}`,
		`{"data":[{"type":"model","id":"claude-haiku-5-5","display_name":"Claude Haiku 5.5","created_at":"2026-08-01T00:00:00Z"}],"has_more":false,"first_id":"claude-haiku-5-5","last_id":"claude-haiku-5-5"}`,
	}
	c := newTestAnthropic(t, srv)
	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != (ModelInfo{ID: "claude-opus-5-5", Name: "Claude Opus 5.5"}) || models[1].ID != "claude-haiku-5-5" {
		t.Fatalf("models %+v", models)
	}
	if _, _, err := c.Embed(context.Background(), "m", []string{"x"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("embed: %v", err)
	}
	if _, err := c.Transcribe(context.Background(), "m", Media{MediaType: "audio/mpeg", Data: []byte("x")}, ""); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("transcribe: %v", err)
	}
}

// Each unsupported feature points to servers that offer that feature:
// Ollama embeds text but does not transcribe audio.
func TestAnthropicUnsupportedFeaturesNameServersThatOfferThem(t *testing.T) {
	_, srv := newFakeAnthropic(t)
	c := newTestAnthropic(t, srv)
	_, _, err := c.Embed(context.Background(), "m", []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "embeddings") || !strings.Contains(err.Error(), "Ollama") {
		t.Fatalf("embed: %v", err)
	}
	_, err = c.Transcribe(context.Background(), "m", Media{MediaType: "audio/mpeg", Data: []byte("x")}, "")
	if err == nil || !strings.Contains(err.Error(), "transcription") || strings.Contains(err.Error(), "Ollama") || strings.Contains(err.Error(), "embeddings") {
		t.Fatalf("transcribe: %v", err)
	}
}

func TestAnthropicRejectsUnsupportedImages(t *testing.T) {
	_, srv := newFakeAnthropic(t)
	c := newTestAnthropic(t, srv)
	_, err := c.Chat(context.Background(), Request{Model: "m", MaxTokens: 10, Messages: []Message{{Role: RoleUser, Text: "x", Images: []Media{{MediaType: "text/plain", Data: []byte("hi")}}}}})
	if err == nil {
		t.Fatal("a non-image accepted as an image")
	}
}

func TestAnthropicStreamEndedEarlyIsTransient(t *testing.T) {
	f, srv := newFakeAnthropic(t)
	f.script = []sseMessage{{
		blocks:      []string{`{"type":"text","text":"partial"}`},
		inputTokens: 9,
		cut:         true,
	}}
	store := NewStore()
	conn := Connection{Name: "claude", Provider: ProviderAnthropic, BaseURL: srv.URL, APIKey: "k"}
	_ = store.Set(conn)
	ctx := WithStore(context.Background(), store)
	c, err := NewClient(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Chat(ctx, Request{Model: "m", MaxTokens: 10, Messages: []Message{{Role: RoleUser, Text: "x"}}})
	if err == nil || !node.IsRetryable(err) {
		t.Fatalf("a cut stream: %+v %v", resp, err)
	}
	if u := store.UsageToday("claude"); u.InputTokens != 9 || u.Requests != 1 {
		t.Fatalf("usage of a cut stream %+v", u)
	}
}
