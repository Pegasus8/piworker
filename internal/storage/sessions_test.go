package storage

import (
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionsPersistExpireAndCannotResurrect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.db")
	s, err := NewSQLiteUserStore(path)
	require.NoError(t, err)
	require.NoError(t, s.CreateUser(&types.User{Username: "admin", PasswordHash: "original-hash"}))
	now := time.Now().Truncate(time.Second)
	token, err := s.CreateSession("admin", "original-hash", now)
	require.NoError(t, err)
	var stored string
	require.NoError(t, s.db.QueryRow("SELECT token_hash FROM auth_sessions").Scan(&stored))
	require.NotEqual(t, token, stored)
	require.NoError(t, s.Close())
	s, err = NewSQLiteUserStore(path)
	require.NoError(t, err)
	defer s.Close()
	session, err := s.GetSession(token, now.Add(6*24*time.Hour), true)
	require.NoError(t, err)
	require.Equal(t, "admin", session.Username)
	_, err = s.GetSession(token, now.Add(14*24*time.Hour), true)
	require.ErrorIs(t, err, ErrSessionNotFound)
	_, err = s.GetSession(token, now.Add(6*24*time.Hour), true)
	require.ErrorIs(t, err, ErrSessionNotFound)
	token, err = s.CreateSession("admin", "original-hash", now)
	require.NoError(t, err)
	for day := 6; day <= 30; day += 6 {
		_, err = s.GetSession(token, now.Add(time.Duration(day)*24*time.Hour), true)
		if day < 30 {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, ErrSessionNotFound)
		}
	}
}
func TestRecoveryCodeExpiryAndConcurrentCredentialGuard(t *testing.T) {
	s, err := NewSQLiteUserStore(":memory:")
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.CreateUser(&types.User{Username: "admin", PasswordHash: "old"}))
	now := time.Now()
	code, err := s.NewAuthCode("recovery", "admin", now)
	require.NoError(t, err)
	require.Error(t, s.RedeemAuthCode("recovery", code, "admin", "new", now.Add(time.Hour)))
	code, err = s.NewAuthCode("recovery", "admin", now)
	require.NoError(t, err)
	require.NoError(t, s.RedeemAuthCode("recovery", code, "admin", "new", now))
	_, err = s.CreateSession("admin", "old", now)
	require.Error(t, err)
	require.Error(t, s.RedeemAuthCode("recovery", code, "admin", "other", now))
}

func TestSetupClaimIsAtomic(t *testing.T) {
	s, err := NewSQLiteUserStore(filepath.Join(t.TempDir(), "setup.db"))
	require.NoError(t, err)
	defer s.Close()
	now := time.Now()
	code, err := s.NewAuthCode("setup", "", now)
	require.NoError(t, err)
	results := make(chan error, 2)
	for _, name := range []string{"first", "second"} {
		go func(name string) { results <- s.RedeemAuthCode("setup", code, name, "hash", now) }(name)
	}
	successes := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	var count int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count))
	require.Equal(t, 1, count)
}

// A passive check (touch=false: the badge poll, live streams) only reads, so
// it must not take SQLite's write lock: with another process holding it
// (piworkerctl, -recover-account), the check still answers at once instead
// of waiting for busy_timeout.
func TestPassiveSessionCheckDoesNotTakeTheWriteLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.db")
	s, err := NewSQLiteUserStore(path)
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.CreateUser(&types.User{Username: "admin", PasswordHash: "hash"}))
	now := time.Now().Truncate(time.Second)
	token, err := s.CreateSession("admin", "hash", now)
	require.NoError(t, err)

	other, err := openDB(path)
	require.NoError(t, err)
	defer other.Close()
	writer, err := other.Begin() // BEGIN IMMEDIATE: holds the write lock
	require.NoError(t, err)
	defer writer.Rollback() //nolint:errcheck // released at the end

	done := make(chan error, 1)
	go func() {
		session, err := s.GetSession(token, now.Add(time.Minute), false)
		if err == nil && session.Username != "admin" {
			err = ErrSessionNotFound
		}
		done <- err
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		require.NoError(t, writer.Rollback())
		<-done
		t.Fatal("a passive session check waited for another writer")
	}

	// It still never revives an expired session: the delete waits for the
	// lock like any write, once it is released.
	require.NoError(t, writer.Rollback())
	_, err = s.GetSession(token, now.Add(SessionIdleTimeout+time.Minute), false)
	require.ErrorIs(t, err, ErrSessionNotFound)
	var left int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM auth_sessions").Scan(&left))
	require.Equal(t, 0, left, "an expired session is deleted when found")
}
