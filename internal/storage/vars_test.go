package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVarsPersistenceRoundTrip(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	loaded, err := store.LoadVars()
	require.NoError(t, err)
	assert.Empty(t, loaded)

	require.NoError(t, store.SaveVar("count", float64(3)))
	require.NoError(t, store.SaveVar("count", float64(7))) // overwrite
	require.NoError(t, store.SaveVar("obj", map[string]interface{}{"a": "b"}))

	loaded, err = store.LoadVars()
	require.NoError(t, err)
	assert.Equal(t, float64(7), loaded["count"])
	assert.Equal(t, map[string]interface{}{"a": "b"}, loaded["obj"])

	require.NoError(t, store.DeleteVar("count"))
	loaded, err = store.LoadVars()
	require.NoError(t, err)
	_, ok := loaded["count"]
	assert.False(t, ok)
	assert.Contains(t, loaded, "obj")
}

func TestVarsErrorWhenClosed(t *testing.T) {
	store := createTestStore(t)
	require.NoError(t, store.Close())

	_, err := store.LoadVars()
	assert.ErrorIs(t, err, ErrDatabaseClosed)
	assert.ErrorIs(t, store.SaveVar("a", 1), ErrDatabaseClosed)
	assert.ErrorIs(t, store.DeleteVar("a"), ErrDatabaseClosed)
}
