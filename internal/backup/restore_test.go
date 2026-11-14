package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A staged restore is applied at the next start: the database and tls/ of
// the backup replace the current ones, which are kept in restore-previous/.
func TestStageThenApplyPendingRestoresWholeState(t *testing.T) {
	in := newInstall(t)
	info, err := Create(context.Background(), in.sources(), filepath.Join(in.dir, "backups"), ReasonManual, t0)
	require.NoError(t, err)

	// Change everything after the backup.
	mustExec(t, in.store.DB(), `DELETE FROM flows`)
	mustExec(t, in.store.DB(), `INSERT INTO flows (id, name, description, data, state, created_at, updated_at) VALUES ('f2', 'Later', '', '{}', 'inactive', ?, ?)`, time.Now(), time.Now())
	require.NoError(t, os.WriteFile(filepath.Join(in.tlsDir, "ca.key"), []byte("NEWER KEY"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(in.tlsDir, "server.crt"), []byte("NEWER SERVER"), 0o600))

	m, err := Stage(filepath.Join(in.dir, "backups", info.Name), in.dbPath)
	require.NoError(t, err)
	assert.Equal(t, info.Manifest.ContentHash, m.ContentHash)
	assert.True(t, Pending(in.dbPath))

	// Staging touches nothing live.
	assert.Equal(t, 1, count(t, in.store.DB(), "flows"))
	key, _ := os.ReadFile(filepath.Join(in.tlsDir, "ca.key"))
	assert.Equal(t, "NEWER KEY", string(key))

	require.NoError(t, in.store.Close())
	applied, err := ApplyPending(in.dbPath, in.tlsDir)
	require.NoError(t, err)
	assert.True(t, applied)
	assert.False(t, Pending(in.dbPath))

	store, err := storage.NewSQLiteStore(in.dbPath)
	require.NoError(t, err)
	defer store.Close()
	var name, state string
	require.NoError(t, store.DB().QueryRow(`SELECT name, state FROM flows`).Scan(&name, &state))
	assert.Equal(t, "Heating", name)
	assert.Equal(t, "running", state, "flows come back as they were")
	assert.Equal(t, 0, count(t, store.DB(), "auth_sessions"), "every session is revoked")

	key, _ = os.ReadFile(filepath.Join(in.tlsDir, "ca.key"))
	assert.Equal(t, "CA KEY", string(key))
	_, err = os.Stat(filepath.Join(in.tlsDir, "server.crt"))
	assert.True(t, os.IsNotExist(err), "tls/ is the backup's, not a merge")

	prev := filepath.Join(in.dir, PreviousDir)
	key, _ = os.ReadFile(filepath.Join(prev, "tls", "ca.key"))
	assert.Equal(t, "NEWER KEY", string(key))
	_, err = os.Stat(filepath.Join(prev, "piworker.db"))
	require.NoError(t, err)
	st, err := os.Stat(in.dbPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())

	// Nothing pending: a second start applies nothing.
	applied, err = ApplyPending(in.dbPath, in.tlsDir)
	require.NoError(t, err)
	assert.False(t, applied)
}

// A backup without tls/ leaves the current certificates in place.
func TestApplyPendingKeepsTLSWhenBackupHasNone(t *testing.T) {
	in := newInstall(t)
	src := in.sources()
	src.TLSDir = ""
	info, err := Create(context.Background(), src, filepath.Join(in.dir, "b"), ReasonManual, t0)
	require.NoError(t, err)
	_, err = Stage(filepath.Join(in.dir, "b", info.Name), in.dbPath)
	require.NoError(t, err)
	require.NoError(t, in.store.Close())
	_, err = ApplyPending(in.dbPath, in.tlsDir)
	require.NoError(t, err)
	key, _ := os.ReadFile(filepath.Join(in.tlsDir, "ca.key"))
	assert.Equal(t, "CA KEY", string(key))
}

// An apply interrupted after the current files were moved aside finishes
// on the next start instead of discarding them.
func TestApplyPendingResumesAfterInterruption(t *testing.T) {
	in := newInstall(t)
	info, err := Create(context.Background(), in.sources(), filepath.Join(in.dir, "b"), ReasonManual, t0)
	require.NoError(t, err)
	mustExec(t, in.store.DB(), `UPDATE flows SET name = 'Changed'`)
	_, err = Stage(filepath.Join(in.dir, "b", info.Name), in.dbPath)
	require.NoError(t, err)
	require.NoError(t, in.store.Close())

	// Simulate a crash right after the current database was moved aside.
	pending := filepath.Join(in.dir, PendingDir)
	prev := filepath.Join(in.dir, PreviousDir)
	require.NoError(t, os.MkdirAll(prev, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(pending, applyingMarker), nil, 0o600))
	require.NoError(t, os.Rename(in.dbPath, filepath.Join(prev, "piworker.db")))

	applied, err := ApplyPending(in.dbPath, in.tlsDir)
	require.NoError(t, err)
	assert.True(t, applied)
	db, err := sql.Open("sqlite3", filepath.Join(prev, "piworker.db"))
	require.NoError(t, err)
	defer db.Close()
	var name string
	require.NoError(t, db.QueryRow(`SELECT name FROM flows`).Scan(&name))
	assert.Equal(t, "Changed", name, "the previous database survives the resumed apply")
}

// A half-written stage (no ready marker) is never applied.
func TestIncompleteStageIsIgnored(t *testing.T) {
	in := newInstall(t)
	require.NoError(t, os.MkdirAll(filepath.Join(in.dir, PendingDir), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(in.dir, PendingDir, "piworker.db"), []byte("partial"), 0o600))
	assert.False(t, Pending(in.dbPath))
	require.NoError(t, in.store.Close())
	applied, err := ApplyPending(in.dbPath, in.tlsDir)
	require.NoError(t, err)
	assert.False(t, applied)
	_, err = os.Stat(filepath.Join(in.dir, PendingDir))
	assert.True(t, os.IsNotExist(err), "the partial stage is removed")
}

func TestStageRejectsInvalidBackupWithoutStaging(t *testing.T) {
	in := newInstall(t)
	bad := filepath.Join(t.TempDir(), "piworker-backup-20261113T000000Z.tar.gz")
	require.NoError(t, os.WriteFile(bad, []byte("garbage"), 0o600))
	_, err := Stage(bad, in.dbPath)
	assert.ErrorIs(t, err, ErrInvalidBackup)
	assert.False(t, Pending(in.dbPath))
	_, err = os.Stat(filepath.Join(in.dir, PendingDir))
	assert.True(t, os.IsNotExist(err))
}
