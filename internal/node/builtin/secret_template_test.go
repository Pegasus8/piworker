package builtin

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderTemplateNeverLeaksSecrets is a security regression guard: the
// payload-producing template path MUST NOT resolve {{secret.NAME}}, or any flow
// (or the node-test endpoint, which echoes processing-node output) could
// exfiltrate secret values. Secrets are resolved only via expandSecrets.
func TestRenderTemplateNeverLeaksSecrets(t *testing.T) {
	require.NoError(t, secrets.DefaultStore.Set("api_key", "s3cr3t"))
	defer secrets.DefaultStore.Delete("api_key")

	msg := types.NewMessage(nil, types.DataTypeAny)
	out := renderTemplate("Bearer {{secret.api_key}}", msg)
	assert.NotContains(t, out, "s3cr3t", "renderTemplate must never surface a secret value")
}

func TestExpandSecretsOnlyTouchesSecrets(t *testing.T) {
	require.NoError(t, secrets.DefaultStore.Set("tok", "abc"))
	defer secrets.DefaultStore.Delete("tok")

	assert.Equal(t, "abc", expandSecrets("{{secret.tok}}"))
	// Non-secret placeholders are left untouched (credential fields must not
	// interpolate message payload).
	assert.Equal(t, "{{payload}}", expandSecrets("{{payload}}"))
	assert.Equal(t, "plain", expandSecrets("plain"))
	// Missing secret renders empty.
	assert.Equal(t, "", expandSecrets("{{secret.absent}}"))
}
