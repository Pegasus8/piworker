// Package backup makes and restores whole-state backups of a PiWorker
// installation (ADR-0032, docs/design/backups-and-updates.md).
//
// A backup is a tar.gz archive holding a manifest, a consistent snapshot of
// the database without history, the tls/ directory and the configuration
// file. A restore is staged next to the database and applied at the next
// start, before the database is opened (ApplyPending).
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	// The SQLite driver, registered as "sqlite3".
	_ "github.com/mattn/go-sqlite3"
)

// Format names the archive format in its manifest.
const Format = "piworker-backup"

// FormatVersion is the archive layout's version.
const FormatVersion = 1

// SchemaVersion marks the database schema this binary understands. Bump it
// when a migration makes a database unreadable for older binaries: a backup
// with a higher value is refused on restore (ErrNewerBackup).
const SchemaVersion = 1

// Why a backup was made.
const (
	ReasonScheduled     = "scheduled"
	ReasonManual        = "manual"
	ReasonBeforeRestore = "before-restore"
)

// HistoryTables are emptied in the snapshot: run history, node events, the
// agent audit and the short-lived sign-in state. They are history, not
// state, and restoring a backup revokes every session.
var HistoryTables = []string{"flow_runs", "node_events", "mcp_audit", "mcp_rejections", "auth_sessions", "auth_codes"}

// Errors a restore reports.
var (
	ErrInvalidBackup = errors.New("not a valid PiWorker backup")
	ErrNewerBackup   = errors.New("the backup was made by a newer PiWorker; update PiWorker first")
)

// Entry names inside an archive.
const (
	manifestEntry = "manifest.json"
	dbEntry       = "piworker.db"
	configEntry   = "config/piworker.toml"
	tlsPrefix     = "tls/"
)

// Limits when reading an archive.
const (
	maxManifestSize = 1 << 20
	maxDBSize       = 2 << 30
	maxSmallFile    = 1 << 20
	maxEntries      = 64
)

var (
	nameRe    = regexp.MustCompile(`^piworker-backup-(\d{8}T\d{6}Z)(?:-(\d+))?\.tar\.gz$`)
	tlsNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

// Manifest describes a backup.
type Manifest struct {
	Format   string    `json:"format"`
	Version  int       `json:"version"`
	Schema   int       `json:"schema"`
	PiWorker string    `json:"piworker"`
	Created  time.Time `json:"created"`
	Host     string    `json:"host,omitempty"`
	Reason   string    `json:"reason"`
	// ContentHash covers the state (database rows without history, tls/,
	// configuration): equal hashes mean nothing changed.
	ContentHash string `json:"contentHash"`
	// Files maps every other entry to its SHA-256.
	Files map[string]string `json:"files"`
}

// Info is a backup file in a directory.
type Info struct {
	Name     string   `json:"name"`
	Size     int64    `json:"size"`
	Manifest Manifest `json:"manifest"`
}

// Sources is what a backup reads.
type Sources struct {
	DB *sql.DB
	// TLSDir is the local CA and server certificate directory; empty or
	// missing: none.
	TLSDir string
	// ConfigPath is the configuration file; empty or missing: none.
	ConfigPath string
	// Version is this binary's version.
	Version string
}

// IsBackupName reports whether name is an archive name Create writes.
func IsBackupName(name string) bool { return nameRe.MatchString(name) }

// Create writes a backup of src into dir (created 0700 if missing) and
// returns it. The archive appears atomically under its final name.
func Create(ctx context.Context, src Sources, dir, reason string, now time.Time) (Info, error) {
	if src.DB == nil {
		return Info{}, errors.New("backup: no database")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Info{}, fmt.Errorf("create backup directory: %w", err)
	}
	work, err := os.MkdirTemp(dir, ".work-")
	if err != nil {
		return Info{}, fmt.Errorf("create work directory: %w", err)
	}
	defer os.RemoveAll(work)

	snap := filepath.Join(work, dbEntry)
	if err := snapshot(ctx, src.DB, snap); err != nil {
		return Info{}, err
	}

	files := map[string]string{dbEntry: snap}
	if src.TLSDir != "" {
		names, err := tlsFiles(src.TLSDir)
		if err != nil {
			return Info{}, err
		}
		for _, n := range names {
			files[tlsPrefix+n] = filepath.Join(src.TLSDir, n)
		}
	}
	if src.ConfigPath != "" {
		if st, err := os.Stat(src.ConfigPath); err == nil && st.Mode().IsRegular() {
			files[configEntry] = src.ConfigPath
		}
	}

	m := Manifest{
		Format: Format, Version: FormatVersion, Schema: SchemaVersion,
		PiWorker: src.Version, Created: now.UTC().Truncate(time.Second), Reason: reason,
		Files: map[string]string{},
	}
	m.Host, _ = os.Hostname()
	for entry, p := range files {
		sum, err := fileSHA256(p)
		if err != nil {
			return Info{}, err
		}
		m.Files[entry] = sum
	}
	m.ContentHash, err = contentHash(snap, m.Files)
	if err != nil {
		return Info{}, err
	}

	tmp := filepath.Join(work, "archive.tar.gz")
	if err := writeArchive(tmp, m, files); err != nil {
		return Info{}, err
	}
	name, err := placeArchive(tmp, dir, m.Created)
	if err != nil {
		return Info{}, err
	}
	st, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		return Info{}, err
	}
	return Info{Name: name, Size: st.Size(), Manifest: m}, nil
}

// snapshot writes a consistent copy of db to dst (VACUUM INTO, while the
// database stays in use) and empties the history tables in the copy.
func snapshot(ctx context.Context, db *sql.DB, dst string) error {
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, dst); err != nil {
		return fmt.Errorf("snapshot database: %w", err)
	}
	if err := os.Chmod(dst, 0o600); err != nil {
		return err
	}
	cp, err := sql.Open("sqlite3", dst)
	if err != nil {
		return err
	}
	defer cp.Close()
	cp.SetMaxOpenConns(1)
	for _, table := range HistoryTables {
		var exists int
		if err := cp.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		if _, err := cp.ExecContext(ctx, `DELETE FROM "`+table+`"`); err != nil {
			return fmt.Errorf("empty %s in the snapshot: %w", table, err)
		}
	}
	if _, err := cp.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("compact the snapshot: %w", err)
	}
	return cp.Close()
}

// tlsFiles lists the regular files directly in dir; a missing dir is none.
func tlsFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var out []string
	for _, e := range ents {
		if e.Type().IsRegular() && tlsNameRe.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// contentHash hashes the snapshot's schema and rows (each table's rows
// sorted, so storage order and page layout do not matter) and the other
// files' checksums. Time and history do not enter it.
func contentHash(dbPath string, files map[string]string) (string, error) {
	db, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return "", err
	}
	defer db.Close()
	h := sha256.New()
	rows, err := db.Query(`SELECT name, sql FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return "", err
	}
	type table struct{ name, sql string }
	var tables []table
	for rows.Next() {
		var t table
		var s sql.NullString
		if err := rows.Scan(&t.name, &s); err != nil {
			rows.Close()
			return "", err
		}
		t.sql = s.String
		tables = append(tables, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	for _, t := range tables {
		fmt.Fprintf(h, "table %q %q\n", t.name, t.sql)
		lines, err := tableRows(db, t.name)
		if err != nil {
			return "", err
		}
		for _, l := range lines {
			fmt.Fprintf(h, "%s\n", l)
		}
	}
	entries := make([]string, 0, len(files))
	for e := range files {
		if e != dbEntry {
			entries = append(entries, e)
		}
	}
	sort.Strings(entries)
	for _, e := range entries {
		fmt.Fprintf(h, "file %q %s\n", e, files[e])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// tableRows returns each row of table as a JSON line, sorted.
func tableRows(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(`SELECT * FROM "` + strings.ReplaceAll(table, `"`, `""`) + `"`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = hex.EncodeToString(b)
			}
		}
		line, err := json.Marshal(vals)
		if err != nil {
			return nil, err
		}
		out = append(out, string(line))
	}
	sort.Strings(out)
	return out, rows.Err()
}

// writeArchive writes the manifest first, then the files in name order.
func writeArchive(dst string, m Manifest, files map[string]string) (err error) {
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := writeEntry(tw, manifestEntry, m.Created, int64(len(mb)), strings.NewReader(string(mb))); err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		src, err := os.Open(files[n])
		if err != nil {
			return err
		}
		st, err := src.Stat()
		if err == nil {
			err = writeEntry(tw, n, m.Created, st.Size(), src)
		}
		src.Close()
		if err != nil {
			return fmt.Errorf("archive %s: %w", n, err)
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return f.Sync()
}

func writeEntry(tw *tar.Writer, name string, mod time.Time, size int64, r io.Reader) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: size, ModTime: mod, Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
		return err
	}
	n, err := io.Copy(tw, r)
	if err == nil && n != size {
		err = fmt.Errorf("size changed while archiving (%d of %d bytes)", n, size)
	}
	return err
}

// placeArchive links tmp into dir under the first free name for created.
func placeArchive(tmp, dir string, created time.Time) (string, error) {
	stamp := created.UTC().Format("20060102T150405Z")
	for i := 1; i < 1000; i++ {
		name := "piworker-backup-" + stamp + ".tar.gz"
		if i > 1 {
			name = "piworker-backup-" + stamp + "-" + strconv.Itoa(i) + ".tar.gz"
		}
		// Link fails when the name exists: no backup is ever overwritten.
		err := os.Link(tmp, filepath.Join(dir, name))
		if err == nil {
			return name, syncDir(dir)
		}
		if !errors.Is(err, os.ErrExist) {
			// Filesystems without hard links (FAT on a USB drive):
			// exclusive create, then copy.
			if ok, cerr := copyExclusive(tmp, filepath.Join(dir, name)); cerr != nil {
				return "", cerr
			} else if ok {
				return name, syncDir(dir)
			}
		}
	}
	return "", errors.New("no free backup name")
}

// copyExclusive copies src to a new file dst; false when dst exists.
func copyExclusive(src, dst string) (bool, error) {
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	in, err := os.Open(src)
	if err != nil {
		out.Close()
		os.Remove(dst)
		return false, err
	}
	defer in.Close()
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return false, err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return false, err
	}
	return true, out.Close()
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	_ = d.Sync() // not supported everywhere; the data itself is synced
	return nil
}

// List returns the backups in dir, newest first. Files that are not
// PiWorker backups, or whose manifest cannot be read, are left out. A
// missing directory is an empty list.
func List(dir string) ([]Info, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, e := range ents {
		if !e.Type().IsRegular() || !IsBackupName(e.Name()) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		m, err := ReadManifest(p)
		if err != nil {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		out = append(out, Info{Name: e.Name(), Size: st.Size(), Manifest: m})
	}
	sortNewestFirst(out)
	return out, nil
}

// ReadManifest reads only the manifest, the archive's first entry.
func ReadManifest(p string) (Manifest, error) {
	f, err := os.Open(p)
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return Manifest{}, ErrInvalidBackup
	}
	tr := tar.NewReader(gz)
	h, err := tr.Next()
	if err != nil || h.Name != manifestEntry || h.Size > maxManifestSize {
		return Manifest{}, ErrInvalidBackup
	}
	return decodeManifest(io.LimitReader(tr, maxManifestSize))
}

func decodeManifest(r io.Reader) (Manifest, error) {
	var m Manifest
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		return Manifest{}, ErrInvalidBackup
	}
	if m.Format != Format || m.Version != FormatVersion || m.Files == nil {
		return Manifest{}, ErrInvalidBackup
	}
	return m, nil
}

// Validate extracts the archive at p into dir (which must exist and be
// empty) and checks it completely: the manifest, only known entries as
// regular files within their size limits, every checksum, the schema, and
// SQLite's integrity check. On error dir may hold partial content.
func Validate(p, dir string) (Manifest, error) {
	f, err := os.Open(p)
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: not a gzip archive", ErrInvalidBackup)
	}
	tr := tar.NewReader(gz)

	var m Manifest
	seen := map[string]string{}
	for i := 0; ; i++ {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Manifest{}, fmt.Errorf("%w: %v", ErrInvalidBackup, err)
		}
		if i >= maxEntries {
			return Manifest{}, fmt.Errorf("%w: too many entries", ErrInvalidBackup)
		}
		if h.Typeflag != tar.TypeReg {
			return Manifest{}, fmt.Errorf("%w: %q is not a regular file", ErrInvalidBackup, h.Name)
		}
		if i == 0 {
			if h.Name != manifestEntry || h.Size > maxManifestSize {
				return Manifest{}, fmt.Errorf("%w: no manifest", ErrInvalidBackup)
			}
			if m, err = decodeManifest(io.LimitReader(tr, maxManifestSize)); err != nil {
				return Manifest{}, err
			}
			continue
		}
		limit := int64(maxSmallFile)
		if h.Name == dbEntry {
			limit = maxDBSize
		}
		if !knownEntry(h.Name) || h.Size > limit {
			return Manifest{}, fmt.Errorf("%w: unexpected entry %q", ErrInvalidBackup, h.Name)
		}
		if _, dup := seen[h.Name]; dup {
			return Manifest{}, fmt.Errorf("%w: duplicate entry %q", ErrInvalidBackup, h.Name)
		}
		sum, err := extract(tr, filepath.Join(dir, filepath.FromSlash(h.Name)), h.Size)
		if err != nil {
			return Manifest{}, err
		}
		seen[h.Name] = sum
	}
	if m.Files == nil {
		return Manifest{}, fmt.Errorf("%w: no manifest", ErrInvalidBackup)
	}
	if len(seen) != len(m.Files) {
		return Manifest{}, fmt.Errorf("%w: entries do not match the manifest", ErrInvalidBackup)
	}
	for name, want := range m.Files {
		if seen[name] != want {
			return Manifest{}, fmt.Errorf("%w: checksum of %q does not match", ErrInvalidBackup, name)
		}
	}
	if _, ok := seen[dbEntry]; !ok {
		return Manifest{}, fmt.Errorf("%w: no database", ErrInvalidBackup)
	}
	if m.Schema > SchemaVersion {
		return Manifest{}, ErrNewerBackup
	}
	if err := checkIntegrity(filepath.Join(dir, dbEntry)); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func knownEntry(name string) bool {
	if name == dbEntry || name == configEntry {
		return true
	}
	if strings.HasPrefix(name, tlsPrefix) {
		base := strings.TrimPrefix(name, tlsPrefix)
		return tlsNameRe.MatchString(base) && path.Clean(name) == name
	}
	return false
}

// extract writes exactly size bytes from r to a new private file p.
func extract(r io.Reader, p string, size int64) (string, error) {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	out, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(r, size))
	if err == nil && n != size {
		err = fmt.Errorf("%w: truncated entry", ErrInvalidBackup)
	}
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// checkIntegrity runs SQLite's integrity check on a database file.
func checkIntegrity(p string) error {
	db, err := sql.Open("sqlite3", "file:"+p+"?mode=ro")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidBackup, err)
	}
	defer db.Close()
	var res string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil {
		return fmt.Errorf("%w: database: %v", ErrInvalidBackup, err)
	}
	if res != "ok" {
		return fmt.Errorf("%w: database integrity: %s", ErrInvalidBackup, res)
	}
	var flows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'flows'`).Scan(&flows); err != nil || flows == 0 {
		return fmt.Errorf("%w: not a PiWorker database", ErrInvalidBackup)
	}
	return nil
}
