package builtin

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/Pegasus8/piworker/internal/webhook"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startWebhook(t *testing.T, cfg map[string]interface{}) (*WebhookTrigger, error) {
	t.Helper()
	n, err := NewWebhookTrigger(cfg)
	require.NoError(t, err)
	wt := n.(*WebhookTrigger)
	err = wt.Start(context.Background(), make(chan *types.Message, 8))
	if err == nil {
		t.Cleanup(func() { _ = wt.Stop() })
	}
	return wt, err
}

func dispatchToken(path, token string) webhook.Result {
	res, _ := webhook.DefaultHub.Dispatch(path, webhook.Request{Method: "POST", Path: path, Token: token})
	return res
}

func TestWebhookTokenResolvesSecret(t *testing.T) {
	setSecret(t, "hook_tok", "abc")
	_, err := startWebhook(t, map[string]interface{}{"path": "secret-hook", "token": "{{secret.hook_tok}}"})
	require.NoError(t, err)

	assert.Equal(t, webhook.Accepted, dispatchToken("secret-hook", "abc"))
	assert.Equal(t, webhook.Unauthorized, dispatchToken("secret-hook", "{{secret.hook_tok}}"), "the placeholder itself is not the token")
	assert.Equal(t, webhook.Unauthorized, dispatchToken("secret-hook", ""))
}

func TestWebhookTokenSecretWithLiteralText(t *testing.T) {
	setSecret(t, "hook_tok", "abc")
	_, err := startWebhook(t, map[string]interface{}{"path": "secret-hook-mixed", "token": "pre-{{secret.hook_tok}}"})
	require.NoError(t, err)
	assert.Equal(t, webhook.Accepted, dispatchToken("secret-hook-mixed", "pre-abc"))
	assert.Equal(t, webhook.Unauthorized, dispatchToken("secret-hook-mixed", "abc"))
}

// A rotated secret applies to the running flow.
func TestWebhookTokenSecretIsReadPerRequest(t *testing.T) {
	setSecret(t, "hook_tok", "abc")
	_, err := startWebhook(t, map[string]interface{}{"path": "secret-hook-rotate", "token": "{{secret.hook_tok}}"})
	require.NoError(t, err)

	require.NoError(t, secrets.DefaultStore.Set("hook_tok", "def"))
	assert.Equal(t, webhook.Accepted, dispatchToken("secret-hook-rotate", "def"))
	assert.Equal(t, webhook.Unauthorized, dispatchToken("secret-hook-rotate", "abc"))
}

// A token that references a missing secret must never degrade into "no
// token required".
func TestWebhookTokenFailsClosed(t *testing.T) {
	_, err := startWebhook(t, map[string]interface{}{"path": "secret-hook-missing", "token": "{{secret.no_such_secret}}"})
	require.Error(t, err, "deploying with an undefined secret fails")
	assert.Contains(t, err.Error(), "no_such_secret")
	assert.False(t, webhook.DefaultHub.Has("secret-hook-missing"))

	setSecret(t, "hook_tok", "abc")
	_, err = startWebhook(t, map[string]interface{}{"path": "secret-hook-deleted", "token": "{{secret.hook_tok}}"})
	require.NoError(t, err)

	require.NoError(t, secrets.DefaultStore.Delete("hook_tok"))
	assert.Equal(t, webhook.Unauthorized, dispatchToken("secret-hook-deleted", ""))
	assert.Equal(t, webhook.Unauthorized, dispatchToken("secret-hook-deleted", "abc"))

	require.NoError(t, secrets.DefaultStore.Set("hook_tok", ""))
	assert.Equal(t, webhook.Unauthorized, dispatchToken("secret-hook-deleted", ""), "an empty secret is not an empty token")
}

func TestExpandSecretsStrict(t *testing.T) {
	setSecret(t, "tok", "abc")
	v, err := expandSecretsStrict(context.Background(), "x-{{secret.tok}}-{{payload}}")
	require.NoError(t, err)
	assert.Equal(t, "x-abc-{{payload}}", v)

	_, err = expandSecretsStrict(context.Background(), "{{secret.absent}}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"absent"`)
}

// A token whose secret was deleted or emptied rejects every request, but logs
// only when that starts (and when it recovers), not once per request: a
// device retrying every second would otherwise fill the SD card.
func TestWebhookUnresolvedTokenLogsOncePerTransition(t *testing.T) {
	setSecret(t, "hook_tok", "abc")
	wt, err := startWebhook(t, map[string]interface{}{"path": "secret-hook-logs", "token": "{{secret.hook_tok}}"})
	require.NoError(t, err)
	var buf bytes.Buffer
	wt.log = zerolog.New(&buf)
	lines := func(level string) int { return strings.Count(buf.String(), `"level":"`+level+`"`) }

	require.NoError(t, secrets.DefaultStore.Delete("hook_tok"))
	for range 5 {
		assert.Equal(t, webhook.Unauthorized, dispatchToken("secret-hook-logs", "abc"))
	}
	assert.Equal(t, 1, lines("warn"), buf.String())

	require.NoError(t, secrets.DefaultStore.Set("hook_tok", ""))
	assert.Equal(t, webhook.Unauthorized, dispatchToken("secret-hook-logs", ""))
	assert.Equal(t, 1, lines("warn"), "still unresolved: no new line")

	require.NoError(t, secrets.DefaultStore.Set("hook_tok", "abc"))
	assert.Equal(t, webhook.Accepted, dispatchToken("secret-hook-logs", "abc"))
	assert.Equal(t, webhook.Unauthorized, dispatchToken("secret-hook-logs", "wrong"))
	assert.Equal(t, 1, lines("info"), "the recovery is logged once")

	require.NoError(t, secrets.DefaultStore.Delete("hook_tok"))
	assert.Equal(t, webhook.Unauthorized, dispatchToken("secret-hook-logs", "abc"))
	assert.Equal(t, webhook.Unauthorized, dispatchToken("secret-hook-logs", "abc"))
	assert.Equal(t, 2, lines("warn"), "a new failure is a new transition")
	assert.NotContains(t, buf.String(), `"abc"`, "the secret value is never logged")
}
