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
