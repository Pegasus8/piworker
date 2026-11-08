package builtin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/Pegasus8/piworker/internal/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookTriggerDelivers(t *testing.T) {
	n, err := NewWebhookTrigger(map[string]interface{}{"path": "test-hook-deliver"})
	require.NoError(t, err)
	wt := n.(*WebhookTrigger)

	out := make(chan *types.Message, 1)
	require.NoError(t, wt.Start(context.Background(), out))
	defer wt.Stop()

	require.True(t, webhook.DefaultHub.Has("test-hook-deliver"))
	res, ok := webhook.DefaultHub.Dispatch("test-hook-deliver", webhook.Request{
		Method: "POST",
		Path:   "test-hook-deliver",
		Body:   map[string]interface{}{"hello": "world"},
	})
	require.True(t, ok)
	assert.Equal(t, webhook.Accepted, res)

	select {
	case msg := <-out:
		payload := msg.Payload.(map[string]interface{})
		assert.Equal(t, "POST", payload["method"])
		assert.Equal(t, "webhook", msg.Topic)
	case <-time.After(time.Second):
		t.Fatal("expected a message")
	}
}

func TestWebhookTriggerToken(t *testing.T) {
	n, err := NewWebhookTrigger(map[string]interface{}{"path": "test-hook-secure", "token": "s3cr3t"})
	require.NoError(t, err)
	wt := n.(*WebhookTrigger)

	out := make(chan *types.Message, 1)
	require.NoError(t, wt.Start(context.Background(), out))
	defer wt.Stop()

	// Missing or wrong token: rejected with Unauthorized.
	for _, tok := range []string{"", "s3cr3", "s3cr3tX"} {
		res, _ := webhook.DefaultHub.Dispatch("test-hook-secure", webhook.Request{Method: "POST", Token: tok})
		assert.Equal(t, webhook.Unauthorized, res, "token %q", tok)
	}
	select {
	case <-out:
		t.Fatal("message must be rejected without the token")
	case <-time.After(50 * time.Millisecond):
	}

	// Correct token: delivered.
	res, _ := webhook.DefaultHub.Dispatch("test-hook-secure", webhook.Request{Method: "POST", Token: "s3cr3t"})
	assert.Equal(t, webhook.Accepted, res)
	select {
	case <-out:
	case <-time.After(time.Second):
		t.Fatal("expected a message with the correct token")
	}
}

func TestWebhookTriggerMethodFilterAndBackpressure(t *testing.T) {
	n, err := NewWebhookTrigger(map[string]interface{}{"path": "test-hook-status", "method": "post"})
	require.NoError(t, err)
	wt := n.(*WebhookTrigger)
	out := make(chan *types.Message, 1)
	require.NoError(t, wt.Start(context.Background(), out))
	defer wt.Stop()

	res, _ := webhook.DefaultHub.Dispatch("test-hook-status", webhook.Request{Method: "GET"})
	assert.Equal(t, webhook.MethodNotAllowed, res)

	res, _ = webhook.DefaultHub.Dispatch("test-hook-status", webhook.Request{Method: "POST"})
	assert.Equal(t, webhook.Accepted, res)
	res, _ = webhook.DefaultHub.Dispatch("test-hook-status", webhook.Request{Method: "POST"})
	assert.Equal(t, webhook.Dropped, res, "a full queue must be reported, not acknowledged")
}

// End to end through the HTTP handler: the caller sees the real outcome and
// the secret never reaches the flow payload.
func TestWebhookTriggerHTTPStatusesAndRedaction(t *testing.T) {
	n, err := NewWebhookTrigger(map[string]interface{}{"path": "test-hook-http", "token": "s3cr3t"})
	require.NoError(t, err)
	wt := n.(*WebhookTrigger)
	out := make(chan *types.Message, 1)
	require.NoError(t, wt.Start(context.Background(), out))
	defer wt.Stop()

	h := webhook.DefaultHub.HandlerFunc()
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest("POST", webhook.PathPrefix+"test-hook-http?token=wrong", strings.NewReader("{}")))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	w = httptest.NewRecorder()
	r := httptest.NewRequest("POST", webhook.PathPrefix+"test-hook-http?token=s3cr3t&x=1", strings.NewReader(`{"a":1}`))
	r.Header.Set("Cookie", "session=abc")
	h(w, r)
	require.Equal(t, http.StatusOK, w.Code)

	msg := <-out
	p := msg.Payload.(map[string]interface{})
	assert.NotContains(t, p["query"], "token")
	assert.NotContains(t, p["headers"], "Cookie")
	assert.Equal(t, "1", p["query"].(map[string]interface{})["x"])
}

func TestWebhookTriggerDuplicatePath(t *testing.T) {
	n1, err := NewWebhookTrigger(map[string]interface{}{"path": "test-hook-dup"})
	require.NoError(t, err)
	n2, err := NewWebhookTrigger(map[string]interface{}{"path": "test-hook-dup"})
	require.NoError(t, err)

	out := make(chan *types.Message, 1)
	require.NoError(t, n1.(*WebhookTrigger).Start(context.Background(), out))
	defer n1.(*WebhookTrigger).Stop()

	require.Error(t, n2.(*WebhookTrigger).Start(context.Background(), out),
		"registering a second node on the same path must fail")
}

func TestWebhookTriggerRequiresPath(t *testing.T) {
	_, err := NewWebhookTrigger(map[string]interface{}{})
	require.Error(t, err)
}
