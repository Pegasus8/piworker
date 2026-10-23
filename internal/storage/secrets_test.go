package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretsPersistence(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	// Empty to start.
	loaded, err := store.LoadSecrets()
	require.NoError(t, err)
	assert.Empty(t, loaded)

	// Save + upsert.
	require.NoError(t, store.SaveSecret("token", "abc"))
	require.NoError(t, store.SaveSecret("token", "xyz")) // overwrite
	require.NoError(t, store.SaveSecret("smtp_pass", "p4ss"))

	loaded, err = store.LoadSecrets()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"token": "xyz", "smtp_pass": "p4ss"}, loaded)

	// Delete.
	require.NoError(t, store.DeleteSecret("token"))
	loaded, err = store.LoadSecrets()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"smtp_pass": "p4ss"}, loaded)
}

func TestSecretsErrorWhenClosed(t *testing.T) {
	store := createTestStore(t)
	require.NoError(t, store.Close())

	_, err := store.LoadSecrets()
	assert.ErrorIs(t, err, ErrDatabaseClosed)
	assert.ErrorIs(t, store.SaveSecret("a", "b"), ErrDatabaseClosed)
	assert.ErrorIs(t, store.DeleteSecret("a"), ErrDatabaseClosed)
}
