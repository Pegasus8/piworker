package storage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	require.NoError(t, db.QueryRow(`PRAGMA user_version`).Scan(&v))
	return v
}

func TestSchemaVersionIsTheNumberOfMigrations(t *testing.T) {
	assert.Equal(t, len(migrations), SchemaVersion, "add a migration, bump SchemaVersion")
}

func TestNewStoreIsAtTheCurrentSchemaVersion(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "new.db"))
	require.NoError(t, err)
	defer store.Close()
	assert.Equal(t, SchemaVersion, userVersion(t, store.DB()))
	// The accounts tables are part of the schema, with or without auth.
	var n int
	require.NoError(t, store.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('users','auth_sessions','auth_codes')`).Scan(&n))
	assert.Equal(t, 3, n)
}

// A database written before versions existed (user_version 0, the oldest
// table shapes) is brought to the current schema, keeping its rows.
func TestUnversionedDatabaseIsUpgraded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := openDB(path)
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE flows (id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT, data TEXT NOT NULL,
			state TEXT NOT NULL DEFAULT 'inactive', created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL);
		CREATE TABLE flow_runs (id TEXT PRIMARY KEY, flow_id TEXT NOT NULL, started_at DATETIME NOT NULL,
			finished_at DATETIME, status TEXT NOT NULL DEFAULT 'running', node_count INTEGER NOT NULL DEFAULT 0,
			error_count INTEGER NOT NULL DEFAULT 0);
		INSERT INTO flows (id, name, data, created_at, updated_at)
			VALUES ('f1', 'old', '{"id":"f1","name":"old","nodes":[],"connections":[]}', '2026-01-01', '2026-01-01');`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := NewSQLiteStore(path)
	require.NoError(t, err)
	defer store.Close()
	assert.Equal(t, SchemaVersion, userVersion(t, store.DB()))
	f, err := store.GetFlow("f1")
	require.NoError(t, err)
	assert.Equal(t, 1, f.Version)
	v, err := store.GetFlowVersion("f1", 1)
	require.NoError(t, err, "the flow got its first stored version")
	assert.Equal(t, 1, v.Version)
}

// A database from a newer PiWorker is refused, untouched: this binary does
// not know what its tables mean.
func TestNewerSchemaIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newer.db")
	store, err := NewSQLiteStore(path)
	require.NoError(t, err)
	_, err = store.DB().Exec(`PRAGMA user_version = 9999`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	_, err = NewSQLiteStore(path)
	require.ErrorIs(t, err, ErrSchemaTooNew)
	assert.Contains(t, err.Error(), "9999")

	db, err := openDB(path)
	require.NoError(t, err)
	defer db.Close()
	assert.Equal(t, 9999, userVersion(t, db))
}

func TestMigrateRunsOnlyPendingMigrationsInOrder(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "m.db"))
	require.NoError(t, err)
	defer db.Close()
	var ran []string
	step := func(name string) migration {
		return migration{name: name, up: func(tx *sql.Tx) error {
			ran = append(ran, name)
			_, err := tx.Exec(`CREATE TABLE ` + name + ` (x)`)
			return err
		}}
	}
	list := []migration{step("a"), step("b")}
	require.NoError(t, migrate(db, list))
	assert.Equal(t, []string{"a", "b"}, ran)
	assert.Equal(t, 2, userVersion(t, db))

	require.NoError(t, migrate(db, list), "nothing pending")
	assert.Equal(t, []string{"a", "b"}, ran)

	require.NoError(t, migrate(db, append(list, step("c"))))
	assert.Equal(t, []string{"a", "b", "c"}, ran)
	assert.Equal(t, 3, userVersion(t, db))
}

// A migration that fails leaves nothing of itself behind and the version
// where it was, so the next start tries it again.
func TestFailedMigrationRollsBack(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "m.db"))
	require.NoError(t, err)
	defer db.Close()
	boom := errors.New("boom")
	list := []migration{
		{name: "ok", up: func(tx *sql.Tx) error { _, err := tx.Exec(`CREATE TABLE ok (x)`); return err }},
		{name: "bad", up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`CREATE TABLE half (x)`); err != nil {
				return err
			}
			return boom
		}},
	}
	err = migrate(db, list)
	require.ErrorIs(t, err, boom)
	assert.Contains(t, err.Error(), "bad")
	assert.Equal(t, 1, userVersion(t, db))
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'half'`).Scan(&n))
	assert.Zero(t, n)
}
