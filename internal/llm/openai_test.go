package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
)

// fakeOpenAI is an OpenAI-compatible server: it records each request and
// answers with the next scripted response for its path.
type fakeOpenAI struct {
	t       *testing.T
	mu      sync.Mutex
	reqs    []recordedRequest
	answers map[string][]fakeAnswer
}

type recordedRequest struct {
	Path   string
	Header http.Header
	Body   map[string]any
	Form   map[string]string
	File   []byte
	Name   string
}

type fakeAnswer struct {
	status int
	body   string
}

func newFakeOpenAI(t *testing.T) (*fakeOpenAI, *httptest.Server) {
	f := &fakeOpenAI{t: t, answers: map[string][]fakeAnswer{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeOpenAI) answer(path string, status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers[path] = append(f.answers[path], fakeAnswer{status, body})
}

func (f *fakeOpenAI) last() recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reqs) == 0 {
		f.t.Fatal("no request")
	}
	return f.reqs[len(f.reqs)-1]
}

func (f *fakeOpenAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := recordedRequest{Path: r.URL.Path, Header: r.Header.Clone()}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			f.t.Errorf("multipart: %v", err)
		}
		rec.Form = map[string]string{}
		for k, v := range r.MultipartForm.Value {
			rec.Form[k] = v[0]
		}
		if fh := r.MultipartForm.File["file"]; len(fh) == 1 {
			file, _ := fh[0].Open()
			rec.File, _ = io.ReadAll(file)
			rec.Name = fh[0].Filename
			_ = file.Close()
		}
	} else if r.Body != nil {
		data, _ := io.ReadAll(r.Body)
		if len(data) > 0 {
			if err := json.Unmarshal(data, &rec.Body); err != nil {
				f.t.Errorf("request body is not JSON: %v", err)
			}
		}
	}
	f.mu.Lock()
	f.reqs = append(f.reqs, rec)
	queue := f.answers[r.URL.Path]
	var a fakeAnswer
	if len(queue) > 0 {
		a = queue[0]
		f.answers[r.URL.Path] = queue[1:]
	} else {
		a = fakeAnswer{404, `{"error":{"message":"no scripted answer"}}`}
	}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(a.status)
	_, _ = io.WriteString(w, a.body)
}

func newTestOpenAI(t *testing.T, conn Connection) Client {
	t.Helper()
	c, err := NewClient(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestOpenAIChatText(t *testing.T) {
	f, srv := newFakeOpenAI(t)
	f.answer("/v1/chat/completions", 200, `{"model":"llama3","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Hi there"}}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`)
	c := newTestOpenAI(t, Connection{Name: "ollama", Provider: ProviderOpenAI, BaseURL: srv.URL + "/v1"})
	resp, err := c.Chat(context.Background(), Request{
		Model: "llama3", System: "Be brief.", MaxTokens: 50, Effort: "max",
		Messages: []Message{
			{Role: RoleUser, Text: "earlier"},
			{Role: RoleAssistant, Text: "answer"},
			{Role: RoleUser, Text: "Look", Images: []Media{{MediaType: "image/png", Data: pngBytes}, {URL: "https://example.com/x.jpg"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "Hi there" || resp.Stop != StopEnd || resp.Model != "llama3" || resp.Usage != (Usage{12, 3, 1}) {
		t.Fatalf("response %+v", resp)
	}
	if resp.Message.Role != RoleAssistant || resp.Message.Text != "Hi there" {
		t.Fatalf("message %+v", resp.Message)
	}
	req := f.last()
	if req.Header.Get("Authorization") != "" {
		t.Fatal("Authorization sent without a key")
	}
	body := req.Body
	if body["max_tokens"] != float64(50) || body["max_completion_tokens"] != nil {
		t.Fatalf("a local server gets max_tokens: %v", body)
	}
	if body["reasoning_effort"] != "high" {
		t.Fatalf("effort max is sent as high: %v", body["reasoning_effort"])
	}
	for _, k := range []string{"temperature", "response_format", "tools", "stream"} {
		if _, ok := body[k]; ok {
			t.Errorf("unexpected %s", k)
		}
	}
	msgs := body["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("messages %v", msgs)
	}
	if m := msgs[0].(map[string]any); m["role"] != "system" || m["content"] != "Be brief." {
		t.Fatalf("system %v", m)
	}
	if m := msgs[1].(map[string]any); m["role"] != "user" || m["content"] != "earlier" {
		t.Fatalf("user text %v", m)
	}
	parts := msgs[3].(map[string]any)["content"].([]any)
	if len(parts) != 3 || parts[0].(map[string]any)["type"] != "text" {
		t.Fatalf("parts %v", parts)
	}
	img := parts[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
	if !strings.HasPrefix(img, "data:image/png;base64,") {
		t.Fatalf("inline image %q", img)
	}
	if parts[2].(map[string]any)["image_url"].(map[string]any)["url"] != "https://example.com/x.jpg" {
		t.Fatalf("URL image %v", parts[2])
	}
}

func TestOpenAIDefaultEndpointUsesMaxCompletionTokens(t *testing.T) {
	c := &openAIClient{conn: Connection{Provider: ProviderOpenAI}, key: "k"}
	body, err := c.chatBody(Request{Model: "gpt", MaxTokens: 7, Messages: []Message{{Role: RoleUser, Text: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	if body["max_completion_tokens"] != 7 || body["max_tokens"] != nil {
		t.Fatalf("default endpoint: %v", body)
	}
}

func TestOpenAIToolRoundTrip(t *testing.T) {
	f, srv := newFakeOpenAI(t)
	f.answer("/chat/completions", 200, `{"model":"gpt","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lights","arguments":"{\"on\":true}"}},{"id":"call_2","type":"function","function":{"name":"temp","arguments":""}}]}}],"usage":{"prompt_tokens":20,"completion_tokens":8}}`)
	f.answer("/chat/completions", 200, `{"model":"gpt","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Done"}}],"usage":{"prompt_tokens":40,"completion_tokens":2}}`)
	c := newTestOpenAI(t, Connection{Name: "gpt", Provider: ProviderOpenAI, BaseURL: srv.URL, APIKey: "sk-test"})
	tools := []Tool{{Name: "lights", Description: "Switch the lights", InputSchema: json.RawMessage(`{"type":"object","properties":{"on":{"type":"boolean"}},"required":["on"]}`)}, {Name: "temp", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	req := Request{Model: "gpt", MaxTokens: 100, Tools: tools, Messages: []Message{{Role: RoleUser, Text: "lights on"}}}
	resp, err := c.Chat(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Stop != StopToolUse || len(resp.ToolCalls) != 2 || resp.ToolCalls[0].Name != "lights" || string(resp.ToolCalls[0].Input) != `{"on":true}` || string(resp.ToolCalls[1].Input) != `{}` {
		t.Fatalf("tool calls %+v", resp.ToolCalls)
	}
	first := f.last()
	if first.Header.Get("Authorization") != "Bearer sk-test" {
		t.Fatalf("authorization %q", first.Header.Get("Authorization"))
	}
	fn := first.Body["tools"].([]any)[0].(map[string]any)
	if fn["type"] != "function" || fn["function"].(map[string]any)["name"] != "lights" || fn["function"].(map[string]any)["parameters"].(map[string]any)["type"] != "object" {
		t.Fatalf("tools %v", fn)
	}

	req.Messages = append(req.Messages, resp.Message, Message{Role: RoleUser, ToolResults: []ToolResult{
		{CallID: "call_1", Content: `{"ok":true}`},
		{CallID: "call_2", Content: "sensor offline", IsError: true},
	}})
	resp, err = c.Chat(context.Background(), req)
	if err != nil || resp.Text != "Done" {
		t.Fatalf("second answer %+v %v", resp, err)
	}
	msgs := f.last().Body["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("messages %v", msgs)
	}
	assistant := msgs[1].(map[string]any)
	calls := assistant["tool_calls"].([]any)
	if assistant["role"] != "assistant" || len(calls) != 2 || calls[0].(map[string]any)["id"] != "call_1" {
		t.Fatalf("assistant replay %v", assistant)
	}
	if args := calls[0].(map[string]any)["function"].(map[string]any)["arguments"]; args != `{"on":true}` {
		t.Fatalf("arguments replay %v", args)
	}
	tool1, tool2 := msgs[2].(map[string]any), msgs[3].(map[string]any)
	if tool1["role"] != "tool" || tool1["tool_call_id"] != "call_1" || tool1["content"] != `{"ok":true}` {
		t.Fatalf("tool result %v", tool1)
	}
	if tool2["tool_call_id"] != "call_2" || !strings.Contains(tool2["content"].(string), "sensor offline") || !strings.Contains(strings.ToLower(tool2["content"].(string)), "error") {
		t.Fatalf("tool error result %v", tool2)
	}
}

func TestOpenAIStructuredOutput(t *testing.T) {
	f, srv := newFakeOpenAI(t)
	f.answer("/chat/completions", 200, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"category\":\"a\",\"confidence\":0.9}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	f.answer("/chat/completions", 200, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"category\":\"zzz\",\"confidence\":0.9}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	f.answer("/chat/completions", 200, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"x\":1}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	f.answer("/chat/completions", 200, `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":"{\"x\":"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	c := newTestOpenAI(t, Connection{Name: "gpt", Provider: ProviderOpenAI, BaseURL: srv.URL})
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"category": map[string]any{"enum": []any{"a", "b"}}, "confidence": map[string]any{"type": "number"}},
		"required":   []any{"category", "confidence"},
	}
	msgs := []Message{{Role: RoleUser, Text: "classify"}}
	resp, err := c.Chat(context.Background(), Request{Model: "gpt", MaxTokens: 10, Schema: schema, Messages: msgs})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := resp.JSON.(map[string]any); !ok || v["category"] != "a" {
		t.Fatalf("json %#v", resp.JSON)
	}
	format := f.last().Body["response_format"].(map[string]any)
	js := format["json_schema"].(map[string]any)
	if format["type"] != "json_schema" || js["name"] != "answer" || js["strict"] != true {
		t.Fatalf("response_format %v", format)
	}
	if js["schema"].(map[string]any)["additionalProperties"] != false {
		t.Fatal("schema not closed")
	}
	// An answer outside the schema is invalid.
	if _, err := c.Chat(context.Background(), Request{Model: "gpt", MaxTokens: 10, Schema: schema, Messages: msgs}); !errors.Is(err, ErrInvalidAnswer) {
		t.Fatalf("invalid answer: %v", err)
	}
	// A schema with an optional property is not strict.
	optional := map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "number"}, "y": map[string]any{"type": "string"}}, "required": []any{"x"}}
	if _, err := c.Chat(context.Background(), Request{Model: "gpt", MaxTokens: 10, Schema: optional, Messages: msgs}); err != nil {
		t.Fatal(err)
	}
	if strict := f.last().Body["response_format"].(map[string]any)["json_schema"].(map[string]any)["strict"]; strict != false {
		t.Fatalf("strict %v", strict)
	}
	// Cut at max tokens.
	if _, err := c.Chat(context.Background(), Request{Model: "gpt", MaxTokens: 10, JSON: true, System: "S", Messages: msgs}); !errors.Is(err, ErrInvalidAnswer) || !strings.Contains(err.Error(), "maxTokens") {
		t.Fatalf("cut answer: %v", err)
	}
	body := f.last().Body
	if body["response_format"].(map[string]any)["type"] != "json_object" {
		t.Fatalf("json mode %v", body["response_format"])
	}
	system := body["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.HasPrefix(system, "S") || !strings.Contains(system, "Answer with one JSON object and nothing else.") {
		t.Fatalf("json instruction %q", system)
	}
}

func TestOpenAIRefusals(t *testing.T) {
	f, srv := newFakeOpenAI(t)
	f.answer("/chat/completions", 200, `{"choices":[{"finish_reason":"content_filter","message":{"role":"assistant","content":""}}],"usage":{"prompt_tokens":1,"completion_tokens":0}}`)
	f.answer("/chat/completions", 200, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":null,"refusal":"I can't help with that."}}],"usage":{"prompt_tokens":1,"completion_tokens":5}}`)
	c := newTestOpenAI(t, Connection{Name: "gpt", Provider: ProviderOpenAI, BaseURL: srv.URL})
	req := Request{Model: "gpt", MaxTokens: 10, Messages: []Message{{Role: RoleUser, Text: "x"}}}
	if _, err := c.Chat(context.Background(), req); !errors.Is(err, ErrRefused) || node.IsRetryable(err) {
		t.Fatalf("content filter: %v", err)
	}
	_, err := c.Chat(context.Background(), req)
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "can't help") {
		t.Fatalf("refusal field: %v", err)
	}
}

func TestOpenAIErrors(t *testing.T) {
	f, srv := newFakeOpenAI(t)
	f.answer("/chat/completions", 401, `{"error":{"message":"Incorrect API key provided: sk-live-123. You can find your key at ...","type":"invalid_request_error"}}`)
	f.answer("/chat/completions", 429, `{"error":{"message":"Rate limit reached"}}`)
	f.answer("/chat/completions", 500, `upstream exploded`)
	f.answer("/chat/completions", 400, `{"error":"model 'x' not found"}`)
	c := newTestOpenAI(t, Connection{Name: "gpt", Provider: ProviderOpenAI, BaseURL: srv.URL, APIKey: "sk-live-123"})
	req := Request{Model: "gpt", MaxTokens: 10, Messages: []Message{{Role: RoleUser, Text: "x"}}}

	_, err := c.Chat(context.Background(), req)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 401 || node.IsRetryable(err) {
		t.Fatalf("401: %v", err)
	}
	if strings.Contains(err.Error(), "sk-live-123") || !strings.Contains(err.Error(), "Incorrect API key provided: <redacted>") {
		t.Fatalf("401 message: %v", err)
	}
	_, err = c.Chat(context.Background(), req)
	if !errors.As(err, &apiErr) || apiErr.Status != 429 || !node.IsRetryable(err) || apiErr.Message != "Rate limit reached" {
		t.Fatalf("429: %v", err)
	}
	_, err = c.Chat(context.Background(), req)
	if !errors.As(err, &apiErr) || apiErr.Status != 500 || !node.IsRetryable(err) || apiErr.Message != "upstream exploded" {
		t.Fatalf("500: %v", err)
	}
	_, err = c.Chat(context.Background(), req)
	if !errors.As(err, &apiErr) || apiErr.Message != "model 'x' not found" {
		t.Fatalf("400 with a string error: %v", err)
	}
}

func TestOpenAIEmbeddingsOrderedByIndex(t *testing.T) {
	f, srv := newFakeOpenAI(t)
	f.answer("/embeddings", 200, `{"data":[{"index":1,"embedding":[0,1]},{"index":0,"embedding":[1,0]}],"usage":{"prompt_tokens":7}}`)
	f.answer("/embeddings", 200, `{"data":[{"index":0,"embedding":[1,0]}]}`)
	c := newTestOpenAI(t, Connection{Name: "gpt", Provider: ProviderOpenAI, BaseURL: srv.URL})
	embs, usage, err := c.Embed(context.Background(), "nomic-embed-text", []string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	if len(embs) != 2 || embs[0][0] != 1 || embs[1][1] != 1 {
		t.Fatalf("order: %v", embs)
	}
	if usage != (Usage{InputTokens: 7, Requests: 1}) {
		t.Fatalf("usage %+v", usage)
	}
	body := f.last().Body
	if body["model"] != "nomic-embed-text" || len(body["input"].([]any)) != 2 {
		t.Fatalf("request %v", body)
	}
	if _, _, err := c.Embed(context.Background(), "m", []string{"a", "b"}); err == nil {
		t.Fatal("a missing embedding accepted")
	}
}

func TestOpenAITranscription(t *testing.T) {
	f, srv := newFakeOpenAI(t)
	f.answer("/audio/transcriptions", 200, `{"text":"hello world"}`)
	store := NewStore()
	conn := Connection{Name: "gpt", Provider: ProviderOpenAI, BaseURL: srv.URL}
	_ = store.Set(conn)
	ctx := WithStore(context.Background(), store)
	c, err := NewClient(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	audio := append([]byte("ID3"), make([]byte, 100)...)
	text, err := c.Transcribe(ctx, "whisper-1", Media{MediaType: "audio/mpeg", Data: audio}, "es")
	if err != nil || text != "hello world" {
		t.Fatalf("text %q err %v", text, err)
	}
	req := f.last()
	if req.Form["model"] != "whisper-1" || req.Form["language"] != "es" || req.Form["response_format"] != "json" {
		t.Fatalf("form %v", req.Form)
	}
	if len(req.File) != len(audio) || !strings.HasSuffix(req.Name, ".mp3") {
		t.Fatalf("file %q (%d bytes)", req.Name, len(req.File))
	}
	if u := store.UsageToday("gpt"); u.Requests != 1 || u.Total() != 0 {
		t.Fatalf("transcription usage %+v", u)
	}
	if _, err := c.Transcribe(ctx, "whisper-1", Media{MediaType: "audio/mpeg", Data: make([]byte, MaxAudioBytes+1)}, ""); err == nil {
		t.Fatal("audio over the limit accepted")
	}
	if _, err := c.Transcribe(ctx, "whisper-1", Media{URL: "https://example.com/a.mp3"}, ""); err == nil {
		t.Fatal("audio by URL accepted (the node fetches it)")
	}
}

func TestOpenAIModels(t *testing.T) {
	f, srv := newFakeOpenAI(t)
	f.answer("/models", 200, `{"object":"list","data":[{"id":"zeta"},{"id":"alpha","name":"Alpha model"}]}`)
	c := newTestOpenAI(t, Connection{Name: "gpt", Provider: ProviderOpenAI, BaseURL: srv.URL})
	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != (ModelInfo{ID: "alpha", Name: "Alpha model"}) || models[1].ID != "zeta" {
		t.Fatalf("models %+v", models)
	}
}

func TestOpenAIBodyLimit(t *testing.T) {
	f, srv := newFakeOpenAI(t)
	f.answer("/models", 200, `{"data":[{"id":"`+strings.Repeat("a", maxModelsBody)+`"}]}`)
	c := newTestOpenAI(t, Connection{Name: "gpt", Provider: ProviderOpenAI, BaseURL: srv.URL})
	if _, err := c.Models(context.Background()); err == nil {
		t.Fatal("an oversized body accepted")
	}
}

// Unknown content is never named by the system's MIME table for
// application/octet-stream (".bin" where /etc/mime.types is read).
func TestAudioFileNameOfUnknownContent(t *testing.T) {
	for _, mt := range []string{"", "application/octet-stream"} {
		if name := audioFileName(mt); name != "audio" {
			t.Errorf("%q: %q", mt, name)
		}
	}
	if name := audioFileName("audio/flac"); name != "audio.flac" {
		t.Errorf("flac: %q", name)
	}
}
