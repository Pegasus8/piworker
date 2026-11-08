package storage

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A credential reset revokes agent tokens in the same transaction as the
// password change (the user store shares the flow store's handle), so either
// both happen or neither does.

func credentialStores(t *testing.T) (*SQLiteStore, *SQLiteUserStore) {
	t.Helper()
	s := createTestStore(t)
	t.Cleanup(func() { s.Close() })
	users, err := NewSQLiteUserStoreWithDB(s.DB())
	require.NoError(t, err)
	require.NoError(t, users.CreateUser(&types.User{Username: "admin", PasswordHash: "old-hash"}))
	return s, users
}

func passwordHash(t *testing.T, users *SQLiteUserStore) string {
	t.Helper()
	u, err := users.GetUser("admin")
	require.NoError(t, err)
	return u.PasswordHash
}

func revokeAll(now time.Time) TxHook {
	return func(tx *sql.Tx) error {
		_, err := RevokeAllAPITokensTx(tx, now)
		return err
	}
}

func TestRecoveryRevokesTokensInTheSameTransaction(t *testing.T) {
	s, users := credentialStores(t)
	tok, _ := newTestToken(t, s, NewAPIToken{})
	epoch, err := s.TokensEpoch()
	require.NoError(t, err)

	code, err := users.NewAuthCode("recovery", "admin", testNow)
	require.NoError(t, err)
	require.NoError(t, users.RedeemAuthCode("recovery", code, "admin", "new-hash", testNow, revokeAll(testNow)))

	assert.Equal(t, "new-hash", passwordHash(t, users))
	stored, err := s.GetAPIToken(tok.ID)
	require.NoError(t, err)
	assert.NotNil(t, stored.RevokedAt)
	after, err := s.TokensEpoch()
	require.NoError(t, err)
	assert.Greater(t, after, epoch)
}

func TestFailedRevocationRollsBackTheRecovery(t *testing.T) {
	s, users := credentialStores(t)
	tok, _ := newTestToken(t, s, NewAPIToken{})
	code, err := users.NewAuthCode("recovery", "admin", testNow)
	require.NoError(t, err)

	boom := errors.New("database is locked")
	err = users.RedeemAuthCode("recovery", code, "admin", "new-hash", testNow,
		revokeAll(testNow), func(*sql.Tx) error { return boom })
	require.ErrorIs(t, err, boom)

	assert.Equal(t, "old-hash", passwordHash(t, users), "the password is unchanged")
	stored, err := s.GetAPIToken(tok.ID)
	require.NoError(t, err)
	assert.Nil(t, stored.RevokedAt, "the revocation is rolled back with it")
	// The code was not consumed: the admin can simply retry.
	require.NoError(t, users.RedeemAuthCode("recovery", code, "admin", "new-hash", testNow, revokeAll(testNow)))
	assert.Equal(t, "new-hash", passwordHash(t, users))
}

func TestBadRecoveryCodeNeverRunsTheHook(t *testing.T) {
	s, users := credentialStores(t)
	tok, _ := newTestToken(t, s, NewAPIToken{})
	_, err := users.NewAuthCode("recovery", "admin", testNow)
	require.NoError(t, err)

	called := false
	err = users.RedeemAuthCode("recovery", strings.Repeat("A", 43), "admin", "new-hash", testNow,
		func(tx *sql.Tx) error { called = true; return revokeAll(testNow)(tx) })
	require.ErrorIs(t, err, ErrAuthCode)
	assert.False(t, called)
	stored, err := s.GetAPIToken(tok.ID)
	require.NoError(t, err)
	assert.Nil(t, stored.RevokedAt)
}

func TestPasswordChangeRevokesTokensAtomically(t *testing.T) {
	s, users := credentialStores(t)
	tok, _ := newTestToken(t, s, NewAPIToken{})
	session, err := users.CreateSession("admin", "old-hash", testNow)
	require.NoError(t, err)

	boom := errors.New("disk full")
	err = users.ChangePassword("admin", "old-hash", "new-hash", revokeAll(testNow), func(*sql.Tx) error { return boom })
	require.ErrorIs(t, err, boom)
	assert.Equal(t, "old-hash", passwordHash(t, users))
	_, err = users.GetSession(session, testNow, false)
	require.NoError(t, err, "sessions survive a change that did not happen")
	stored, err := s.GetAPIToken(tok.ID)
	require.NoError(t, err)
	assert.Nil(t, stored.RevokedAt)

	require.NoError(t, users.ChangePassword("admin", "old-hash", "new-hash", revokeAll(testNow)))
	assert.Equal(t, "new-hash", passwordHash(t, users))
	stored, err = s.GetAPIToken(tok.ID)
	require.NoError(t, err)
	assert.NotNil(t, stored.RevokedAt)

	// A stale current hash changes nothing and runs no hook.
	called := false
	err = users.ChangePassword("admin", "old-hash", "third-hash", func(*sql.Tx) error { called = true; return nil })
	require.ErrorIs(t, err, ErrUserNotFound)
	assert.False(t, called)
}
