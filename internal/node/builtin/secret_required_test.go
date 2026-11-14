package builtin

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStrictSecretFieldsAreRequired: the settings that cannot work without
// their secret (the Telegram bot token, the notification URL, the database
// DSN) report the secret names they reference, so deploy can refuse a flow
// whose secrets are not defined. Nothing is resolved here.
func TestStrictSecretFieldsAreRequired(t *testing.T) {
	cases := []struct {
		typ  string
		cfg  map[string]any
		want map[string][]string
	}{
		{"action-telegram", map[string]any{"botToken": "{{secret.tg_token}}", "chatId": "1"},
			map[string][]string{"botToken": {"tg_token"}}},
		{"action-notify", map[string]any{"url": "https://hooks.example.com/{{secret.hook_a}}/{{ secret.hook_b }}"},
			map[string][]string{"url": {"hook_a", "hook_b"}}},
		{"action-database", map[string]any{"driver": "sqlite", "dsn": "{{secret.db_dsn}}", "query": "SELECT 1"},
			map[string][]string{"dsn": {"db_dsn"}}},
		{"action-telegram", map[string]any{"botToken": "literal-token", "chatId": "1"}, map[string][]string{}},
	}
	for _, c := range cases {
		n, err := node.DefaultRegistry.Create(c.typ, c.cfg)
		require.NoError(t, err, c.typ)
		req, ok := n.(node.SecretRequirer)
		require.True(t, ok, "%s requires its secrets", c.typ)
		assert.Equal(t, c.want, req.RequiredSecrets(), c.typ)
	}
}

// TestOptionalSecretFieldsAreReferenced: the settings that resolve their
// secrets when the node runs but do not refuse a deploy without them (an
// MQTT or SMTP username and password, an HTTP request's URL and headers)
// report the names they reference, so Lint and deploy can warn
// (unresolved_secret) instead of letting an undefined one become an empty
// value or a failed run unnoticed. Nothing is resolved here.
func TestOptionalSecretFieldsAreReferenced(t *testing.T) {
	cases := []struct {
		typ  string
		cfg  map[string]any
		want map[string][]string
	}{
		{"action-email", map[string]any{"host": "smtp.example.com", "from": "a@example.com", "to": "b@example.com",
			"username": "{{secret.smtp_user}}", "password": "{{secret.smtp_pass}}"},
			map[string][]string{"username": {"smtp_user"}, "password": {"smtp_pass"}}},
		{"action-mqtt", map[string]any{"broker": "tcp://localhost:1883", "topic": "t", "password": "{{ secret.mq }}"},
			map[string][]string{"password": {"mq"}}},
		{"trigger-mqtt", map[string]any{"broker": "tcp://localhost:1883", "topic": "t", "username": "{{secret.mq_user}}"},
			map[string][]string{"username": {"mq_user"}}},
		{"action-http", map[string]any{"url": "https://api.example.com/?key={{secret.api_key}}",
			"headers": map[string]any{"Authorization": "Bearer {{secret.api_token}}", "X-Plain": "x"}},
			map[string][]string{"url": {"api_key"}, "headers": {"api_token"}}},
		{"action-email", map[string]any{"host": "smtp.example.com", "from": "a@example.com", "to": "b@example.com",
			"password": "literal"}, map[string][]string{}},
	}
	for _, c := range cases {
		n, err := node.DefaultRegistry.Create(c.typ, c.cfg)
		require.NoError(t, err, c.typ)
		ref, ok := n.(node.SecretReferencer)
		require.True(t, ok, "%s declares the secrets it resolves", c.typ)
		assert.Equal(t, c.want, ref.SecretReferences(), c.typ)
	}
}
