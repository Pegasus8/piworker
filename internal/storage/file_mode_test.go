//go:build !windows

package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A new database file is created private (0600) whatever the process umask,
// so a binary started by hand does not leave the accounts and secrets
// readable by other users (docs/design/secrets-at-rest.md, ADR-0030).
func TestNewDatabaseFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.db")
	s, err := NewSQLiteStore(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.SaveSecret("probe", "value")) // makes SQLite write its WAL
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(p)
		require.NoError(t, err, p)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), p)
	}
}

// An existing database readable by other users loses those permission bits
// on open (with its WAL and shared-memory files); the owner's and group's
// are kept (an installation may give its group read access on purpose).
func TestExistingDatabaseLosesOtherUsersAccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.db")
	s, err := NewSQLiteStore(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(p); err == nil {
			require.NoError(t, os.Chmod(p, 0o644))
		}
	}

	s, err = NewSQLiteStore(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	for _, p := range []string{path + "-wal", path + "-shm"} {
		if info, err := os.Stat(p); err == nil {
			assert.Zero(t, info.Mode().Perm()&0o007, p)
		}
	}
}
