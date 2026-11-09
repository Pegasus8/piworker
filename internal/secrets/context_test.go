package secrets

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFromContextDefaultsToDefaultStore: a context without a store (and a
// nil context) resolves secrets from the process-wide store.
func TestFromContextDefaultsToDefaultStore(t *testing.T) {
	assert.Same(t, DefaultStore, FromContext(context.Background()))
	//nolint:staticcheck // a nil context must not panic
	assert.Same(t, DefaultStore, FromContext(nil))
	assert.Same(t, DefaultStore, FromContext(WithStore(context.Background(), nil)),
		"a nil store is no store")
}

// TestWithStoreIsolatesLookups: a store carried by the context replaces the
// process-wide one for every lookup made through that context, so a sandboxed
// execution (dry-run, test node) never sees the real secrets.
func TestWithStoreIsolatesLookups(t *testing.T) {
	require.NoError(t, DefaultStore.Set("ctx_canary", "real-value"))
	t.Cleanup(func() { _ = DefaultStore.Delete("ctx_canary") })

	isolated := NewStore()
	ctx := WithStore(context.Background(), isolated)
	assert.Same(t, isolated, FromContext(ctx))
	_, ok := FromContext(ctx).Get("ctx_canary")
	assert.False(t, ok, "the isolated store is empty")

	require.NoError(t, isolated.Set("ctx_canary", "sandbox"))
	v, _ := FromContext(ctx).Get("ctx_canary")
	assert.Equal(t, "sandbox", v)
	v, _ = DefaultStore.Get("ctx_canary")
	assert.Equal(t, "real-value", v, "writes to the isolated store never reach the default one")

	// A derived context keeps the store.
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	assert.Same(t, isolated, FromContext(child))
}
