package storage

import (
	"errors"
	"io/fs"
	"os"
	"runtime"
	"strings"

	"github.com/rs/zerolog/log"
)

// The database holds the accounts, the secrets and the broker connections'
// passwords in plaintext: PiWorker does not encrypt them itself
// (docs/design/secrets-at-rest.md, ADR-0030). What protects them on the
// device is the file's permissions, so they are not left to the umask of
// whoever starts the binary.

// sidecarSuffixes are the files SQLite keeps next to a database. SQLite
// creates them with the permissions of the database file.
var sidecarSuffixes = []string{"", "-wal", "-shm", "-journal"}

// protectDBFiles makes the database at path private before SQLite opens it:
// a database that does not exist yet is created empty with mode 0600 (SQLite
// accepts an empty file as a new database, and gives its WAL and
// shared-memory files the same mode); an existing one, with its sidecar
// files, loses the permission bits of other users (owner and group bits
// stay: an installation may grant its group read access on purpose). A
// change it makes, and one it cannot make, is logged; neither stops the
// start. In-memory and URI databases, and Windows (no permission bits), are
// left alone.
func protectDBFiles(path string) {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") || runtime.GOOS == "windows" {
		return
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_ = f.Close()
		// The umask cannot widen 0600, but a group-writable default ACL
		// could: set the mode explicitly.
		_ = os.Chmod(path, 0o600)
		return
	}
	if !errors.Is(err, fs.ErrExist) {
		return // SQLite reports the real problem when it opens the file
	}
	for _, suffix := range sidecarSuffixes {
		p := path + suffix
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		mode := info.Mode().Perm()
		if mode&0o007 == 0 {
			continue
		}
		if err := os.Chmod(p, mode&^0o007); err != nil {
			log.Warn().Err(err).Str("path", p).Msg("Database file is readable by other users and its permissions could not be restricted; restrict them (chmod o-rwx)")
			continue
		}
		log.Warn().Str("path", p).Str("was", mode.String()).Str("now", (mode &^ 0o007).String()).
			Msg("Database file was readable by other users; removed their access (it holds accounts and secrets)")
	}
}
