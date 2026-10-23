package main

import (
	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOptionalAdminBootstrapPreservesExistingAccount(t *testing.T) {
	store, err := storage.NewSQLiteUserStore(":memory:")
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, ensureDefaultAdmin(store, config.Default()))
	exists, err := store.UserExists()
	require.NoError(t, err)
	require.False(t, exists)
	cfg := config.Default()
	cfg.AdminUser = "owner"
	cfg.AdminPass = "original-password"
	require.NoError(t, ensureDefaultAdmin(store, cfg))
	original, err := store.GetUser("owner")
	require.NoError(t, err)
	cfg.AdminPass = "replacement-password"
	require.NoError(t, ensureDefaultAdmin(store, cfg))
	after, err := store.GetUser("owner")
	require.NoError(t, err)
	require.Equal(t, original.PasswordHash, after.PasswordHash)
}
