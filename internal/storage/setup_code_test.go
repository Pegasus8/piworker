package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupCodeValidAndConsume(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "s.db"))
	require.NoError(t, err)
	defer store.Close()
	users, err := NewSQLiteUserStoreWithDB(store.DB())
	require.NoError(t, err)
	now := time.Now()
	code, err := users.NewAuthCode("setup", "", now)
	require.NoError(t, err)

	ok, err := users.SetupCodeValid(code, now)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, _ = users.SetupCodeValid("wrong", now)
	assert.False(t, ok)
	ok, _ = users.SetupCodeValid(code, now.Add(AuthCodeLifetime+time.Second))
	assert.False(t, ok, "expired")

	// Checking does not consume it.
	ok, _ = users.SetupCodeValid(code, now)
	assert.True(t, ok)
	require.NoError(t, users.ConsumeSetupCode(code))
	ok, _ = users.SetupCodeValid(code, now)
	assert.False(t, ok)

	// Once an account exists no setup code is valid.
	code, err = users.NewAuthCode("setup", "", now)
	require.NoError(t, err)
	require.NoError(t, users.CreateUser(&types.User{Username: "admin", PasswordHash: "x"}))
	ok, _ = users.SetupCodeValid(code, now)
	assert.False(t, ok)
}
