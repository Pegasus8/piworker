package secrets

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Expand replaces {{secret.NAME}} references (spaces allowed inside the
// braces) with the values of the context's store, an undefined one with "",
// and leaves every other placeholder as written (the node templates'
// semantics).
func TestExpand(t *testing.T) {
	store := NewStore()
	require.NoError(t, store.Set("mqtt", "pa55"))
	ctx := WithStore(context.Background(), store)

	assert.Equal(t, "pa55", Expand(ctx, "{{secret.mqtt}}"))
	assert.Equal(t, "u-pa55-x", Expand(ctx, "u-{{ secret.mqtt }}-x"))
	assert.Equal(t, "", Expand(ctx, "{{secret.missing}}"))
	assert.Equal(t, "{{payload.x}} ", Expand(ctx, "{{payload.x}} {{secret}}"), "a reference without a name is empty")
	assert.Equal(t, "plain", Expand(ctx, "plain"))
	assert.Equal(t, "", Expand(WithStore(context.Background(), nil), "{{secret.mqtt}}"), "an isolated store")
}
