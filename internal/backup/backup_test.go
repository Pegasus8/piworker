package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// install is a data directory like a real installation's: a database with
// state (a flow, a setting, a user) and history (runs, events, audit,
// sessions), a tls/ directory and a configuration file.
type install struct {
	dir, dbPath, tlsDir, configPath string
	store                           *storage.SQLiteStore
}

func newInstall(t *testing.T) *install {
	t.Helper()
	dir := t.TempDir()
	in := &install{dir: dir, dbPath: filepath.Join(dir, "piworker.db"), tlsDir: filepath.Join(dir, "tls"), configPath: filepath.Join(dir, "piworker.toml")}
	store, err := storage.NewSQLiteStore(in.dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	users, err := storage.NewSQLiteUserStoreWithDB(store.DB())
	require.NoError(t, err)
	_ = users
	in.store = store
	db := store.DB()
	now := time.Now().UTC()
	mustExec(t, db, `INSERT INTO flows (id, name, description, data, state, created_at, updated_at) VALUES ('f1', 'Heating', '', '{"id":"f1","name":"Heating","nodes":[],"connections":[]}', 'running', ?, ?)`, now, now)
	mustExec(t, db, `INSERT INTO settings (key, value) VALUES ('agent_deploy', 'off')`)
	mustExec(t, db, `INSERT INTO users (username, password_hash, created_at) VALUES ('admin', 'hash', ?)`, now)
	mustExec(t, db, `INSERT INTO auth_sessions (token_hash, username, created_at, last_seen, expires_at) VALUES ('t', 'admin', 1, 1, 9999999999)`)
	mustExec(t, db, `INSERT INTO auth_codes (purpose, username, token_hash, expires_at) VALUES ('recovery', 'admin', 'h', 9999999999)`)
	mustExec(t, db, `INSERT INTO flow_runs (id, flow_id, started_at, status) VALUES ('r1', 'f1', ?, 'ok')`, now)
	mustExec(t, db, `INSERT INTO node_events (run_id, flow_id, node_id, node_type, phase, created_at) VALUES ('r1', 'f1', 'n1', 'trigger-manual', 'end', ?)`, now)
	require.NoError(t, os.MkdirAll(in.tlsDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(in.tlsDir, "ca.crt"), []byte("CA CERT"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(in.tlsDir, "ca.key"), []byte("CA KEY"), 0o600))
	require.NoError(t, os.WriteFile(in.configPath, []byte("addr = \":8443\"\n"), 0o600))
	return in
}

func (in *install) sources() Sources {
	return Sources{DB: in.store.DB(), TLSDir: in.tlsDir, ConfigPath: in.configPath, Version: "v1.2.3"}
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	_, err := db.Exec(q, args...)
	require.NoError(t, err)
}

func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&n))
	return n
}

// entries reads an archive's entry names and contents.
func entries(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		b, err := io.ReadAll(tr)
		require.NoError(t, err)
		out[h.Name] = b
	}
	return out
}

var t0 = time.Date(2026, 11, 13, 3, 17, 0, 0, time.UTC)

func TestCreateWritesStateWithoutHistory(t *testing.T) {
	in := newInstall(t)
	backups := filepath.Join(in.dir, "backups")

	info, err := Create(context.Background(), in.sources(), backups, ReasonScheduled, t0)
	require.NoError(t, err)
	assert.Equal(t, "piworker-backup-20261113T031700Z.tar.gz", info.Name)
	assert.Equal(t, ReasonScheduled, info.Manifest.Reason)
	assert.Equal(t, "v1.2.3", info.Manifest.PiWorker)
	assert.Equal(t, Format, info.Manifest.Format)
	assert.Equal(t, SchemaVersion, info.Manifest.Schema)
	assert.NotEmpty(t, info.Manifest.ContentHash)

	path := filepath.Join(backups, info.Name)
	st, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	dst, err := os.Stat(backups)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dst.Mode().Perm())
	assert.Equal(t, st.Size(), info.Size)

	files := entries(t, path)
	assert.ElementsMatch(t, []string{"manifest.json", "piworker.db", "tls/ca.crt", "tls/ca.key", "config/piworker.toml"}, keys(files))
	assert.Equal(t, "CA KEY", string(files["tls/ca.key"]))
	assert.Equal(t, "addr = \":8443\"\n", string(files["config/piworker.toml"]))

	// The database holds the state and none of the history.
	snap := filepath.Join(t.TempDir(), "snap.db")
	require.NoError(t, os.WriteFile(snap, files["piworker.db"], 0o600))
	db, err := sql.Open("sqlite3", snap)
	require.NoError(t, err)
	defer db.Close()
	assert.Equal(t, 1, count(t, db, "flows"))
	assert.Equal(t, 1, count(t, db, "settings"))
	assert.Equal(t, 1, count(t, db, "users"))
	for _, table := range HistoryTables {
		assert.Equal(t, 0, count(t, db, table), table)
	}
	// The live database is untouched.
	assert.Equal(t, 1, count(t, in.store.DB(), "flow_runs"))
	assert.Equal(t, 1, count(t, in.store.DB(), "auth_sessions"))

	// No work files are left behind.
	left, err := os.ReadDir(backups)
	require.NoError(t, err)
	assert.Len(t, left, 1)
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestCreateWithoutTLSOrConfig(t *testing.T) {
	in := newInstall(t)
	src := in.sources()
	src.TLSDir = filepath.Join(in.dir, "missing")
	src.ConfigPath = ""
	info, err := Create(context.Background(), src, filepath.Join(in.dir, "b"), ReasonManual, t0)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"manifest.json", "piworker.db"}, keys(entries(t, filepath.Join(in.dir, "b", info.Name))))
}

// The content hash changes with state, not with history or time, so an
// unchanged installation can skip its daily backup.
func TestContentHashFollowsStateOnly(t *testing.T) {
	in := newInstall(t)
	dir := filepath.Join(in.dir, "b")
	a, err := Create(context.Background(), in.sources(), dir, ReasonScheduled, t0)
	require.NoError(t, err)
	mustExec(t, in.store.DB(), `INSERT INTO flow_runs (id, flow_id, started_at, status) VALUES ('r2', 'f1', ?, 'ok')`, time.Now())
	b, err := Create(context.Background(), in.sources(), dir, ReasonScheduled, t0.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, a.Manifest.ContentHash, b.Manifest.ContentHash)

	mustExec(t, in.store.DB(), `UPDATE settings SET value = 'low' WHERE key = 'agent_deploy'`)
	c, err := Create(context.Background(), in.sources(), dir, ReasonScheduled, t0.Add(2*time.Hour))
	require.NoError(t, err)
	assert.NotEqual(t, a.Manifest.ContentHash, c.Manifest.ContentHash)

	require.NoError(t, os.WriteFile(filepath.Join(in.tlsDir, "ca.crt"), []byte("NEW CA"), 0o600))
	d, err := Create(context.Background(), in.sources(), dir, ReasonScheduled, t0.Add(3*time.Hour))
	require.NoError(t, err)
	assert.NotEqual(t, c.Manifest.ContentHash, d.Manifest.ContentHash)
}

// Two backups in the same second get distinct names.
func TestCreateSameSecond(t *testing.T) {
	in := newInstall(t)
	dir := filepath.Join(in.dir, "b")
	a, err := Create(context.Background(), in.sources(), dir, ReasonManual, t0)
	require.NoError(t, err)
	b, err := Create(context.Background(), in.sources(), dir, ReasonManual, t0)
	require.NoError(t, err)
	assert.NotEqual(t, a.Name, b.Name)
	assert.Equal(t, "piworker-backup-20261113T031700Z-2.tar.gz", b.Name)
}

func TestListReadsManifestsNewestFirstAndIgnoresOtherFiles(t *testing.T) {
	in := newInstall(t)
	dir := filepath.Join(in.dir, "b")
	_, err := Create(context.Background(), in.sources(), dir, ReasonScheduled, t0)
	require.NoError(t, err)
	_, err = Create(context.Background(), in.sources(), dir, ReasonManual, t0.Add(time.Hour))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "piworker-backup-20261113T000000Z.tar.gz"), []byte("not a backup"), 0o600))

	list, err := List(dir)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, ReasonManual, list[0].Manifest.Reason)
	assert.Equal(t, ReasonScheduled, list[1].Manifest.Reason)
	assert.True(t, list[0].Manifest.Created.Equal(t0.Add(time.Hour)))

	// A missing directory is an empty list.
	none, err := List(filepath.Join(in.dir, "nope"))
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestValidateRejectsTamperedAndForeignArchives(t *testing.T) {
	in := newInstall(t)
	dir := filepath.Join(in.dir, "b")
	info, err := Create(context.Background(), in.sources(), dir, ReasonManual, t0)
	require.NoError(t, err)
	good := entries(t, filepath.Join(dir, info.Name))

	write := func(name string, files map[string][]byte, order ...string) string {
		p := filepath.Join(t.TempDir(), name)
		f, err := os.Create(p)
		require.NoError(t, err)
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		for _, n := range order {
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: n, Mode: 0o600, Size: int64(len(files[n])), Typeflag: tar.TypeReg}))
			_, err := tw.Write(files[n])
			require.NoError(t, err)
		}
		require.NoError(t, tw.Close())
		require.NoError(t, gz.Close())
		require.NoError(t, f.Close())
		return p
	}
	clone := func() map[string][]byte {
		c := map[string][]byte{}
		for k, v := range good {
			c[k] = v
		}
		return c
	}
	all := []string{"manifest.json", "piworker.db", "tls/ca.crt", "tls/ca.key", "config/piworker.toml"}

	// Untouched copy validates.
	_, err = Validate(write("ok.tar.gz", clone(), all...), t.TempDir())
	require.NoError(t, err)

	tampered := clone()
	tampered["tls/ca.key"] = []byte("OTHER KEY")
	_, err = Validate(write("t.tar.gz", tampered, all...), t.TempDir())
	assert.ErrorIs(t, err, ErrInvalidBackup)

	extra := clone()
	extra["../evil"] = []byte("x")
	_, err = Validate(write("e.tar.gz", extra, append(all, "../evil")...), t.TempDir())
	assert.ErrorIs(t, err, ErrInvalidBackup)

	missing := clone()
	_, err = Validate(write("m.tar.gz", missing, "manifest.json", "tls/ca.crt", "tls/ca.key", "config/piworker.toml"), t.TempDir())
	assert.ErrorIs(t, err, ErrInvalidBackup)

	var m Manifest
	require.NoError(t, json.Unmarshal(good["manifest.json"], &m))
	m.Schema = SchemaVersion + 1
	newer := clone()
	newer["manifest.json"], _ = json.Marshal(m)
	_, err = Validate(write("n.tar.gz", newer, all...), t.TempDir())
	assert.ErrorIs(t, err, ErrNewerBackup)

	m.Schema = SchemaVersion
	m.Format = "something-else"
	foreign := clone()
	foreign["manifest.json"], _ = json.Marshal(m)
	_, err = Validate(write("f.tar.gz", foreign, all...), t.TempDir())
	assert.ErrorIs(t, err, ErrInvalidBackup)

	// A database that is not SQLite fails the integrity check even with
	// matching checksums.
	corrupt := clone()
	corrupt["piworker.db"] = []byte(strings.Repeat("x", 4096))
	require.NoError(t, json.Unmarshal(good["manifest.json"], &m))
	m.Files["piworker.db"] = sha256Hex(corrupt["piworker.db"])
	corrupt["manifest.json"], _ = json.Marshal(m)
	_, err = Validate(write("c.tar.gz", corrupt, all...), t.TempDir())
	assert.ErrorIs(t, err, ErrInvalidBackup)

	_, err = Validate(filepath.Join(t.TempDir(), "absent.tar.gz"), t.TempDir())
	assert.Error(t, err)
}
