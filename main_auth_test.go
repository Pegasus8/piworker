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

func TestAdminBootstrapTrimsUsernameLikeLogin(t *testing.T) {
	store, err := storage.NewSQLiteUserStore(":memory:")
	require.NoError(t, err)
	defer store.Close()
	cfg := config.Default()
	cfg.AdminUser = "  owner\n"
	cfg.AdminPass = "original-password"
	require.NoError(t, ensureDefaultAdmin(store, cfg))
	// Login trims the submitted username, so the stored one must be trimmed too.
	u, err := store.GetUser("owner")
	require.NoError(t, err)
	require.NotNil(t, u, "a username with stray whitespace from env must still be able to sign in")
}

func TestAdminBootstrapRejectsInvalidCredentials(t *testing.T) {
	long := make([]byte, 129)
	for i := range long {
		long[i] = 'a'
	}
	cases := map[string][2]string{
		"blank username":    {"   ", "original-password"},
		"username too long": {string(long), "original-password"},
		"password too long": {"owner", string(long[:73])},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			store, err := storage.NewSQLiteUserStore(":memory:")
			require.NoError(t, err)
			defer store.Close()
			cfg := config.Default()
			cfg.AdminUser, cfg.AdminPass = c[0], c[1]
			require.Error(t, ensureDefaultAdmin(store, cfg))
			exists, err := store.UserExists()
			require.NoError(t, err)
			require.False(t, exists)
		})
	}
}
