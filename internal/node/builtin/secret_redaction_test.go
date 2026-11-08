package builtin

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func failingClient() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: connection refused")
	})}
}

const testBotToken = "123456:ABC-supersecret-token"

func TestTelegramErrorDoesNotLeakToken(t *testing.T) {
	n, err := NewTelegramAction(map[string]interface{}{"botToken": testBotToken, "chatId": "1"})
	require.NoError(t, err)
	a := n.(*TelegramAction)
	a.client = failingClient()

	_, err = a.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "supersecret")
	assert.Contains(t, err.Error(), "api.telegram.org")
	assert.True(t, node.IsRetryable(err), "network failures stay transient")
}

func TestNotifyErrorDoesNotLeakWebhookURL(t *testing.T) {
	webhook := "https://discord.com/api/webhooks/42/very-secret-webhook-token"
	n, err := NewNotifyAction(map[string]interface{}{"url": webhook})
	require.NoError(t, err)
	a := n.(*NotifyAction)
	a.client = failingClient()

	_, err = a.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "very-secret-webhook-token")
	assert.NotContains(t, err.Error(), "/api/webhooks/42")
	assert.Contains(t, err.Error(), "discord.com")
}

func TestNotifyInvalidURLErrorDoesNotLeakURL(t *testing.T) {
	n, err := NewNotifyAction(map[string]interface{}{"url": "https://hooks.example.com/secret-path\x7f"})
	require.NoError(t, err)
	_, err = n.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret-path")
}

func TestNotifyHTTPErrorStatusFails(t *testing.T) {
	for status, retryable := range map[int]bool{404: false, 503: true} {
		n, err := NewNotifyAction(map[string]interface{}{"url": "https://hooks.example.com/x"})
		require.NoError(t, err)
		a := n.(*NotifyAction)
		a.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: http.NoBody, Request: r}, nil
		})}
		_, err = a.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
		require.Error(t, err, "status %d must fail the node", status)
		assert.Equal(t, retryable, node.IsRetryable(err), "status %d", status)
	}
}

// API keys are often put in the query string or userinfo; a network error must
// not echo them into logs and the UI. Host and path stay visible for debugging.
func TestHTTPErrorDoesNotLeakQueryOrUserinfo(t *testing.T) {
	n, err := NewHTTPAction(map[string]interface{}{
		"url":    "https://user:pa55word@api.example.com/v1/items?apikey=supersecret&x=1",
		"method": "GET",
	})
	require.NoError(t, err)
	a := n.(*HTTPAction)
	a.client = failingClient()

	_, err = a.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "supersecret")
	assert.NotContains(t, err.Error(), "pa55word")
	assert.Contains(t, err.Error(), "api.example.com/v1/items")
	assert.True(t, node.IsRetryable(err), "GET network failures stay transient")
}
