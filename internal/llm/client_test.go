package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
)

// chatServer answers every chat completion with answer and counts requests.
func chatServer(t *testing.T, answer string, promptTokens, completionTokens int) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "served-model",
			"choices": []any{map[string]any{
				"index":         0,
				"finish_reason": "stop",
				"message":       map[string]any{"role": "assistant", "content": answer},
			}},
			"usage": map[string]any{"prompt_tokens": promptTokens, "completion_tokens": completionTokens},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestNewClientSpendsFromTheStoreOfItsContext(t *testing.T) {
	srv, calls := chatServer(t, "hello", 30, 20)
	store := NewStore()
	conn := Connection{Name: "local", Provider: ProviderOpenAI, BaseURL: srv.URL + "/v1", DailyTokenLimit: 60}
	if err := store.Set(conn); err != nil {
		t.Fatal(err)
	}
	ctx := WithStore(context.Background(), store)
	client, err := NewClient(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{Model: "m", MaxTokens: 100, Messages: []Message{{Role: RoleUser, Text: "hi"}}}
	resp, err := client.Chat(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello" || resp.Usage.InputTokens != 30 || resp.Usage.OutputTokens != 20 || resp.Usage.Requests != 1 {
		t.Fatalf("response %+v", resp)
	}
	if u := store.UsageToday("local"); u.Total() != 50 || u.Requests != 1 {
		t.Fatalf("usage not recorded: %+v", u)
	}
	// 50 < 60: one more call is allowed, then the budget is spent.
	if _, err := client.Chat(ctx, req); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Chat(ctx, req); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("over budget: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("the provider was called over budget: %d calls", calls.Load())
	}
	if node.IsRetryable(func() error { _, err := client.Chat(ctx, req); return err }()) {
		t.Fatal("an exhausted budget must not be retried")
	}

	// The stored connection's limit applies, not the one the client was built with.
	unlimited := conn
	unlimited.DailyTokenLimit = 0
	c2, _ := NewClient(ctx, unlimited)
	if _, err := c2.Chat(ctx, req); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("stored limit ignored: %v", err)
	}
	// Models spends no tokens and is not refused by the budget.
	srvModels := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer srvModels.Close()
	conn.BaseURL = srvModels.URL
	c3, _ := NewClient(ctx, conn)
	if models, err := c3.Models(ctx); err != nil || len(models) != 1 {
		t.Fatalf("models over budget: %v %v", models, err)
	}
}

func TestNewClientRecordsUsageOfAnInvalidAnswer(t *testing.T) {
	srv, _ := chatServer(t, "not json at all", 10, 5)
	store := NewStore()
	conn := Connection{Name: "local", Provider: ProviderOpenAI, BaseURL: srv.URL}
	_ = store.Set(conn)
	ctx := WithStore(context.Background(), store)
	client, err := NewClient(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Chat(ctx, Request{Model: "m", MaxTokens: 10, JSON: true, Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if !errors.Is(err, ErrInvalidAnswer) {
		t.Fatalf("want ErrInvalidAnswer, got %v", err)
	}
	if u := store.UsageToday("local"); u.Total() != 15 {
		t.Fatalf("usage of an invalid answer not recorded: %+v", u)
	}
}

func TestNewClientChecksTheRequest(t *testing.T) {
	srv, calls := chatServer(t, "x", 1, 1)
	conn := Connection{Name: "local", Provider: ProviderOpenAI, BaseURL: srv.URL}
	ctx := context.Background()
	client, err := NewClient(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Chat(ctx, Request{MaxTokens: 10, Messages: []Message{{Role: RoleUser, Text: "hi"}}}); !errors.Is(err, ErrNoModel) {
		t.Fatalf("no model: %v", err)
	}
	if _, err := client.Chat(ctx, Request{Model: "m", Messages: []Message{{Role: RoleUser, Text: "hi"}}}); err == nil {
		t.Fatal("MaxTokens 0 accepted")
	}
	if _, err := client.Chat(ctx, Request{Model: "m", MaxTokens: 5, Schema: map[string]any{"type": "array"}}); err == nil {
		t.Fatal("a non-object schema accepted")
	}
	if _, _, err := client.Embed(ctx, "", []string{"x"}); !errors.Is(err, ErrNoModel) {
		t.Fatalf("embed without a model: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("an invalid request reached the provider")
	}
}

func TestNewClientRefusesInASandbox(t *testing.T) {
	ctx := node.WithSandbox(context.Background(), node.NewSandbox(1024, 4096))
	if _, err := NewClient(ctx, Connection{Name: "x", Provider: ProviderOpenAI}); err == nil {
		t.Fatal("a sandboxed context got a client")
	}
}

func TestNewClientResolvesTheKeyThroughItsContext(t *testing.T) {
	var auth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	store := secrets.NewStore()
	_ = store.Set("OPENAI", "sk-secret-value")
	ctx := secrets.WithStore(context.Background(), store)
	client, err := NewClient(ctx, Connection{Name: "x", Provider: ProviderOpenAI, BaseURL: srv.URL, APIKey: "{{secret.OPENAI}}"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Models(ctx); err != nil {
		t.Fatal(err)
	}
	if auth.Load() != "Bearer sk-secret-value" {
		t.Fatalf("authorization %q", auth.Load())
	}
	_, err = NewClient(context.Background(), Connection{Name: "x", Provider: ProviderOpenAI, APIKey: "{{secret.MISSING}}"})
	if err == nil || !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("undefined secret: %v", err)
	}
	if _, err := NewClient(context.Background(), Connection{Name: "x", Provider: ProviderAnthropic}); err == nil {
		t.Fatal("an Anthropic connection without a key got a client")
	}
}

func TestClassifyStatus(t *testing.T) {
	transient := []int{408, 409, 425, 429, 500, 502, 503, 504, 529}
	for _, s := range transient {
		if !node.IsRetryable(statusError(s, "x", "")) {
			t.Errorf("%d should be transient", s)
		}
	}
	for _, s := range []int{400, 401, 403, 404, 413, 422} {
		if node.IsRetryable(statusError(s, "x", "")) {
			t.Errorf("%d should be permanent", s)
		}
	}
	err := statusError(401, "invalid key sk-abc123 "+strings.Repeat("y", 1000), "sk-abc123")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 401 {
		t.Fatalf("not an APIError: %v", err)
	}
	if strings.Contains(err.Error(), "sk-abc123") || !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("key not redacted: %v", err)
	}
	if len(apiErr.Message) > maxErrorMessage {
		t.Fatalf("message not capped: %d", len(apiErr.Message))
	}
}

func TestRequestErrorIsTransientAndRedacted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing listens any more
	client, err := NewClient(context.Background(), Connection{Name: "x", Provider: ProviderOpenAI, BaseURL: url, APIKey: "sk-net"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Models(context.Background())
	if err == nil || !node.IsRetryable(err) {
		t.Fatalf("network error should be transient: %v", err)
	}
	if strings.Contains(err.Error(), "sk-net") {
		t.Fatalf("key in error: %v", err)
	}

	// A cancelled context is the caller's: not transient.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer slow.Close()
	client, _ = NewClient(context.Background(), Connection{Name: "x", Provider: ProviderOpenAI, BaseURL: slow.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = client.Models(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || node.IsRetryable(err) {
		t.Fatalf("deadline: %v (retryable %v)", err, node.IsRetryable(err))
	}
}
