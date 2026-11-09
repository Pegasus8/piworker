package vars

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContextStoresIsolateBothScopes: the global and the flow store carried
// by a context replace the real ones for every scoped lookup, so a sandboxed
// execution reads and writes only its own stores.
func TestContextStoresIsolateBothScopes(t *testing.T) {
	require.NoError(t, DefaultStore.Set("ctx_canary", "real"))
	t.Cleanup(func() { _ = DefaultStore.Delete("ctx_canary") })

	global, local := NewStore(), NewStore()
	ctx := WithFlowStore(WithStore(context.Background(), global), local)

	g, err := Scoped(ctx, "global")
	require.NoError(t, err)
	assert.Same(t, global, g)
	l, err := Scoped(ctx, "flow")
	require.NoError(t, err)
	assert.Same(t, local, l)

	require.NoError(t, g.Set("ctx_canary", "sandbox"))
	v, _ := DefaultStore.Get("ctx_canary")
	assert.Equal(t, "real", v, "the real global store is untouched")
}

// TestFromContextNilContext: a nil context means "no context stores".
func TestFromContextNilContext(t *testing.T) {
	//nolint:staticcheck // a nil context must not panic
	assert.Same(t, DefaultStore, FromContext(nil))
	//nolint:staticcheck // a nil context must not panic
	assert.Nil(t, FlowFromContext(nil))
	assert.Same(t, DefaultStore, FromContext(WithStore(context.Background(), nil)), "a nil store is no store")
}
