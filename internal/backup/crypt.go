package backup

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"

	"filippo.io/age"
	"github.com/Pegasus8/piworker/internal/storesbackup"
)

// Copies that leave the device (remote destinations, downloads) are
// encrypted with age and a passphrase (scrypt), so the standard age tool
// opens them without PiWorker: age -d -o backup.tar.gz backup.tar.gz.age.

// EncryptedSuffix is appended to the name of an encrypted copy.
const EncryptedSuffix = ".age"

// workFactor is log2 of scrypt's N for new copies: 32 MiB, about a second
// on a Raspberry Pi 3. maxWorkFactor bounds what a copy may ask for when
// opened (256 MiB, the age tool's default), so a crafted file cannot
// exhaust a Pi's memory.
const (
	workFactor    = 15
	maxWorkFactor = 18
)

var ageHeader = []byte("age-encryption.org/v1\n")

// Errors of encrypted copies.
var (
	ErrWrongPassphrase = errors.New("the passphrase does not open this backup")
	ErrWeakPassphrase  = storesbackup.ErrWeakPassphrase
)

// CheckPassphrase applies the export rules (ADR-0031): at least 12 characters.
func CheckPassphrase(p string) error { return storesbackup.CheckPassphrase(p) }

// IsEncrypted reports whether data starts like an age file.
func IsEncrypted(head []byte) bool { return bytes.HasPrefix(head, ageHeader) }

// Encrypt writes src to dst sealed with passphrase.
func Encrypt(dst io.Writer, src io.Reader, passphrase string) error {
	if err := CheckPassphrase(passphrase); err != nil {
		return err
	}
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return err
	}
	r.SetWorkFactor(workFactor)
	w, err := age.Encrypt(dst, r)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, src); err != nil {
		return err
	}
	return w.Close()
}

// Decrypt writes the content of the age file src to dst.
func Decrypt(dst io.Writer, src io.Reader, passphrase string) error {
	br := bufio.NewReader(src)
	head, _ := br.Peek(len(ageHeader))
	if !IsEncrypted(head) {
		return fmt.Errorf("%w: not an encrypted backup", ErrInvalidBackup)
	}
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return ErrWrongPassphrase
	}
	id.SetMaxWorkFactor(maxWorkFactor)
	r, err := age.Decrypt(br, id)
	if err != nil {
		var noMatch *age.NoIdentityMatchError
		if errors.As(err, &noMatch) {
			return ErrWrongPassphrase
		}
		return fmt.Errorf("%w: %v", ErrInvalidBackup, err)
	}
	if _, err := io.Copy(dst, r); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidBackup, err)
	}
	return nil
}
