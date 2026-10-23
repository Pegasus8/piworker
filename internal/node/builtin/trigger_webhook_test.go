package builtin

import (
	"context"
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
	require.True(t, webhook.DefaultHub.Dispatch("test-hook-deliver", webhook.Request{
		Method: "POST",
		Path:   "test-hook-deliver",
		Body:   map[string]interface{}{"hello": "world"},
	}))

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

	// Missing token: rejected.
	webhook.DefaultHub.Dispatch("test-hook-secure", webhook.Request{Method: "POST", Headers: map[string]string{}})
	select {
	case <-out:
		t.Fatal("message must be rejected without the token")
	case <-time.After(100 * time.Millisecond):
	}

	// Correct token via header: delivered.
	webhook.DefaultHub.Dispatch("test-hook-secure", webhook.Request{Method: "POST", Headers: map[string]string{"X-Webhook-Token": "s3cr3t"}})
	select {
	case <-out:
	case <-time.After(time.Second):
		t.Fatal("expected a message with the correct token")
	}
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
