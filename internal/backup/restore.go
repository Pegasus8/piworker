package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Directories next to the database.
const (
	// PendingDir holds a validated restore until the next start applies it.
	PendingDir = "restore-pending"
	// PreviousDir holds the database and tls/ a restore replaced, until the
	// next restore.
	PreviousDir = "restore-previous"

	readyMarker    = "ready"
	applyingMarker = "applying"
	stagingSuffix  = ".tmp"
)

// dbSidecars are the files SQLite keeps next to a database.
var dbSidecars = []string{"", "-wal", "-shm", "-journal"}

// Stage validates the backup at archive and stages it next to dbPath. The
// running installation is not touched: ApplyPending applies it at the next
// start. A stage already pending is replaced.
func Stage(archive, dbPath string) (Manifest, error) {
	dataDir := filepath.Dir(dbPath)
	staging := filepath.Join(dataDir, PendingDir+stagingSuffix)
	if err := os.RemoveAll(staging); err != nil {
		return Manifest{}, err
	}
	if err := os.Mkdir(staging, 0o700); err != nil {
		return Manifest{}, err
	}
	m, err := Validate(archive, staging)
	if err != nil {
		os.RemoveAll(staging)
		return Manifest{}, err
	}
	if err := os.WriteFile(filepath.Join(staging, readyMarker), nil, 0o600); err != nil {
		os.RemoveAll(staging)
		return Manifest{}, err
	}
	pending := filepath.Join(dataDir, PendingDir)
	if err := os.RemoveAll(pending); err != nil {
		os.RemoveAll(staging)
		return Manifest{}, err
	}
	if err := os.Rename(staging, pending); err != nil {
		os.RemoveAll(staging)
		return Manifest{}, err
	}
	return m, syncDir(dataDir)
}

// Pending reports whether a complete restore is staged next to dbPath.
func Pending(dbPath string) bool {
	_, err := os.Stat(filepath.Join(filepath.Dir(dbPath), PendingDir, readyMarker))
	return err == nil
}

// CancelPending removes a staged restore that has not started applying.
func CancelPending(dbPath string) error {
	pending := filepath.Join(filepath.Dir(dbPath), PendingDir)
	if _, err := os.Stat(filepath.Join(pending, applyingMarker)); err == nil {
		return errors.New("the restore is already being applied")
	}
	return os.RemoveAll(pending)
}

// ApplyPending applies a staged restore: the current database (with its
// sidecar files) and tls/ move to restore-previous/, the backup's take
// their place. It must run before the database is opened. A stage without
// its ready marker is incomplete and removed. An apply interrupted midway
// resumes without discarding what was already moved aside. tls/ is
// replaced only when the backup has one.
func ApplyPending(dbPath, tlsDir string) (bool, error) {
	dataDir := filepath.Dir(dbPath)
	pending := filepath.Join(dataDir, PendingDir)
	os.RemoveAll(filepath.Join(dataDir, PendingDir+stagingSuffix))
	if _, err := os.Stat(pending); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if !Pending(dbPath) {
		return false, os.RemoveAll(pending)
	}
	previous := filepath.Join(dataDir, PreviousDir)
	marker := filepath.Join(pending, applyingMarker)
	if _, err := os.Stat(marker); errors.Is(err, os.ErrNotExist) {
		// First attempt: make room for what is replaced, then record that
		// moving has started so a resumed attempt keeps it.
		if err := os.RemoveAll(previous); err != nil {
			return false, err
		}
		if err := os.Mkdir(previous, 0o700); err != nil {
			return false, err
		}
		if err := os.WriteFile(marker, nil, 0o600); err != nil {
			return false, err
		}
		if err := syncDir(pending); err != nil {
			return false, err
		}
	}

	base := filepath.Base(dbPath)
	for _, suffix := range dbSidecars {
		if err := moveIfExists(dbPath+suffix, filepath.Join(previous, base+suffix)); err != nil {
			return false, err
		}
	}
	pendingTLS := filepath.Join(pending, "tls")
	if exists(pendingTLS) {
		if err := moveIfExists(tlsDir, filepath.Join(previous, "tls")); err != nil {
			return false, err
		}
		if err := moveIfExists(pendingTLS, tlsDir); err != nil {
			return false, err
		}
	}
	if err := moveIfExists(filepath.Join(pending, dbEntry), dbPath); err != nil {
		return false, err
	}
	if !exists(dbPath) {
		return false, fmt.Errorf("restore: %s is missing after applying; the previous database is in %s", dbPath, previous)
	}
	// The configuration is restored by the installer (piworkerctl restore),
	// not here: keep the backup's copy next to the previous state for
	// reference.
	_ = moveIfExists(filepath.Join(pending, "config"), filepath.Join(previous, "config-from-backup"))
	if err := os.RemoveAll(pending); err != nil {
		return true, err
	}
	return true, syncDir(dataDir)
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// moveIfExists renames src to dst (replacing dst); nothing when src is gone.
func moveIfExists(src, dst string) error {
	if !exists(src) {
		return nil
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return os.Rename(src, dst)
}
