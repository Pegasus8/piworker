package backup

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Settings keys of the remote destination. The secret key and the
// passphrase are stored apart from the settings and never returned.
const (
	RemoteSettingsKey   = "backup_remote"
	remoteSecretKeyKey  = "backup_remote_secret"
	remotePassphraseKey = "backup_passphrase"
)

// remoteTimeout bounds one synchronisation with the remote destination.
const remoteTimeout = 10 * time.Minute

// MaxUploadSize bounds a backup file uploaded to restore.
const MaxUploadSize = 512 << 20

// RemoteState is the remote destination as the editor sees it.
type RemoteState struct {
	Settings      RemoteSettings `json:"settings"`
	HasSecretKey  bool           `json:"hasSecretKey"`
	HasPassphrase bool           `json:"hasPassphrase"`
}

func (s *Service) setting(key string) (string, error) {
	v, _, err := s.opts.Settings.GetSetting(key)
	return v, err
}

// RemoteState returns the stored remote settings and whether its secrets
// are set.
func (s *Service) RemoteState() (RemoteState, error) {
	var st RemoteState
	raw, ok, err := s.opts.Settings.GetSetting(RemoteSettingsKey)
	if err != nil {
		return st, err
	}
	if ok {
		if err := json.Unmarshal([]byte(raw), &st.Settings); err != nil {
			return st, fmt.Errorf("read remote backup settings: %w", err)
		}
	}
	secret, err := s.setting(remoteSecretKeyKey)
	if err != nil {
		return st, err
	}
	phrase, err := s.setting(remotePassphraseKey)
	if err != nil {
		return st, err
	}
	st.HasSecretKey, st.HasPassphrase = secret != "", phrase != ""
	return st, nil
}

// SaveRemote stores the remote destination. An empty secretKey or
// passphrase keeps the stored one; enabling needs both.
func (s *Service) SaveRemote(rs RemoteSettings, secretKey, passphrase string) error {
	if passphrase != "" {
		if err := CheckPassphrase(passphrase); err != nil {
			return err
		}
	}
	current, err := s.RemoteState()
	if err != nil {
		return err
	}
	if rs.Enabled {
		n, _, err := rs.normalize()
		if err != nil {
			return err
		}
		n.Enabled = true
		rs = n
		if secretKey == "" && !current.HasSecretKey {
			return fmt.Errorf("%w: enter the secret access key", ErrInvalidSettings)
		}
		if passphrase == "" && !current.HasPassphrase {
			return fmt.Errorf("%w: choose a passphrase for the encrypted copies", ErrInvalidSettings)
		}
	}
	raw, err := json.Marshal(rs)
	if err != nil {
		return err
	}
	if err := s.opts.Settings.SetSetting(RemoteSettingsKey, string(raw)); err != nil {
		return err
	}
	if secretKey != "" {
		if err := s.opts.Settings.SetSetting(remoteSecretKeyKey, secretKey); err != nil {
			return err
		}
	}
	if passphrase != "" {
		if err := s.opts.Settings.SetSetting(remotePassphraseKey, passphrase); err != nil {
			return err
		}
	}
	return nil
}

// PassphraseMatches reports whether p is the stored passphrase.
func (s *Service) PassphraseMatches(p string) bool {
	stored, err := s.setting(remotePassphraseKey)
	return err == nil && stored != "" && subtle.ConstantTimeCompare([]byte(stored), []byte(p)) == 1
}

// remote connects to the stored destination; nil when it is disabled.
func (s *Service) remote() (ObjectStore, RemoteSettings, error) {
	st, err := s.RemoteState()
	if err != nil || !st.Settings.Enabled {
		return nil, st.Settings, err
	}
	secret, err := s.setting(remoteSecretKeyKey)
	if err != nil {
		return nil, st.Settings, err
	}
	store, err := NewS3(st.Settings, secret, s.opts.RemoteTransport)
	if err != nil {
		return nil, st.Settings, err
	}
	n, _, _ := st.Settings.normalize()
	return store, n, nil
}

// TestRemote lists the destination rs with secretKey (the stored one when
// empty) to check the address, bucket and credentials.
func (s *Service) TestRemote(ctx context.Context, rs RemoteSettings, secretKey string) error {
	if secretKey == "" {
		var err error
		if secretKey, err = s.setting(remoteSecretKeyKey); err != nil {
			return err
		}
	}
	store, err := NewS3(rs, secretKey, s.opts.RemoteTransport)
	if err != nil {
		return err
	}
	n, _, _ := rs.normalize()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	_, err = store.List(ctx, n.Prefix)
	return err
}

// syncRemote makes sure the newest local backup has an encrypted copy at
// the destination and applies retention there. Failures are recorded in
// st, never returned: the local backup stands.
func (s *Service) syncRemote(ctx context.Context, dir string, keep Retention, st *Status) {
	store, rs, err := s.remote()
	if store == nil && err == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	fail := func(err error) {
		st.RemoteLastError = err.Error()
		s.opts.Logger.Error().Err(err).Msg("Copy to the remote backup destination failed")
	}
	if err != nil {
		fail(err)
		return
	}
	if st.LastName != "" && exists(filepath.Join(dir, st.LastName)) {
		if err := s.upload(ctx, store, rs, dir, st.LastName); err != nil {
			fail(err)
			return
		}
	}
	list, err := s.listRemote(ctx, store, rs)
	if err != nil {
		fail(err)
		return
	}
	for _, in := range SelectExpired(list, keep, s.opts.Location) {
		if err := store.Delete(ctx, rs.Prefix+in.Name+EncryptedSuffix); err != nil {
			fail(err)
			return
		}
		s.opts.Logger.Info().Str("file", in.Name+EncryptedSuffix).Msg("Expired remote backup removed")
	}
	now := s.opts.Now()
	st.RemoteLastSuccess, st.RemoteLastError, st.RemoteLastName = &now, "", st.LastName
}

// upload encrypts the local archive name and stores it, unless the
// destination already has it.
func (s *Service) upload(ctx context.Context, store ObjectStore, rs RemoteSettings, dir, name string) error {
	key := rs.Prefix + name + EncryptedSuffix
	if ok, err := store.Exists(ctx, key); err != nil || ok {
		return err
	}
	phrase, err := s.setting(remotePassphraseKey)
	if err != nil {
		return err
	}
	src, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer src.Close()
	tmp, err := os.CreateTemp(dir, ".work-upload-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := Encrypt(tmp, src, phrase); err != nil {
		return err
	}
	size, err := tmp.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := store.Put(ctx, key, tmp, size); err != nil {
		return err
	}
	s.opts.Logger.Info().Str("file", key).Int64("bytes", size).Msg("Backup copied to the remote destination")
	return nil
}

// listRemote returns the destination's backups, newest first; Created
// comes from the name (the manifest is inside the encrypted copy).
func (s *Service) listRemote(ctx context.Context, store ObjectStore, rs RemoteSettings) ([]Info, error) {
	objs, err := store.List(ctx, rs.Prefix)
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, o := range objs {
		rest, ok := strings.CutPrefix(o.Key, rs.Prefix)
		if !ok || strings.Contains(rest, "/") {
			continue
		}
		name, ok := strings.CutSuffix(rest, EncryptedSuffix)
		if !ok {
			continue
		}
		created, ok := createdFromName(name)
		if !ok {
			continue
		}
		out = append(out, Info{Name: name, Size: o.Size, Manifest: Manifest{Created: created}})
	}
	sortNewestFirst(out)
	return out, nil
}

// createdFromName reads the time a backup's name carries.
func createdFromName(name string) (time.Time, bool) {
	m := nameRe.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, false
	}
	t, err := time.Parse("20060102T150405Z", m[1])
	return t, err == nil
}

// ListRemote lists the backups at the remote destination, newest first; an
// empty list when it is disabled.
func (s *Service) ListRemote(ctx context.Context) ([]Info, error) {
	store, rs, err := s.remote()
	if store == nil || err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	return s.listRemote(ctx, store, rs)
}

// StageRemoteRestore downloads, decrypts and stages the named remote
// backup, then backs up the current state.
func (s *Service) StageRemoteRestore(ctx context.Context, name string) (Manifest, error) {
	if !IsBackupName(name) {
		return Manifest{}, ErrNotFound
	}
	store, rs, err := s.remote()
	if err != nil {
		return Manifest{}, err
	}
	if store == nil {
		return Manifest{}, ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	key := rs.Prefix + name + EncryptedSuffix
	if ok, err := store.Exists(ctx, key); err != nil {
		return Manifest{}, err
	} else if !ok {
		return Manifest{}, ErrNotFound
	}
	rc, err := store.Get(ctx, key)
	if err != nil {
		return Manifest{}, err
	}
	defer rc.Close()
	phrase, err := s.setting(remotePassphraseKey)
	if err != nil {
		return Manifest{}, err
	}
	return s.StageUpload(ctx, rc, phrase, true)
}

// StageUpload stages a backup read from r: a plain archive, or an
// encrypted copy opened with passphrase. With backupFirst the current state
// is backed up after the backup was validated (not on a fresh
// installation, which has nothing to keep).
func (s *Service) StageUpload(ctx context.Context, r io.Reader, passphrase string, backupFirst bool) (Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tmp, err := os.CreateTemp(s.DataDir(), ".upload-")
	if err != nil {
		return Manifest{}, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	n, err := io.Copy(tmp, io.LimitReader(r, MaxUploadSize+1))
	if err != nil {
		return Manifest{}, err
	}
	if n > MaxUploadSize {
		return Manifest{}, fmt.Errorf("%w: larger than %d MiB", ErrInvalidBackup, MaxUploadSize>>20)
	}
	head := make([]byte, 32)
	k, _ := tmp.ReadAt(head, 0)
	archive := tmp.Name()
	if IsEncrypted(head[:k]) {
		if passphrase == "" {
			return Manifest{}, ErrWrongPassphrase
		}
		plain, err := os.CreateTemp(s.DataDir(), ".upload-")
		if err != nil {
			return Manifest{}, err
		}
		defer os.Remove(plain.Name())
		defer plain.Close()
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return Manifest{}, err
		}
		if err := Decrypt(plain, tmp, passphrase); err != nil {
			return Manifest{}, err
		}
		if err := plain.Sync(); err != nil {
			return Manifest{}, err
		}
		archive = plain.Name()
	}
	m, err := Stage(archive, s.opts.DBPath)
	if err != nil {
		return Manifest{}, err
	}
	if backupFirst {
		if _, _, err := s.backUpLocked(ctx, ReasonBeforeRestore); err != nil {
			if cerr := CancelPending(s.opts.DBPath); cerr != nil {
				s.opts.Logger.Error().Err(cerr).Msg("Cannot cancel the staged restore")
			}
			return Manifest{}, fmt.Errorf("back up the current state first: %w", err)
		}
	}
	s.opts.Logger.Warn().Time("created", m.Created).Msg("Restore staged; it is applied when PiWorker starts again")
	return m, nil
}

// Download writes the named local backup encrypted with passphrase: a
// copy that leaves the device is always encrypted (ADR-0030).
func (s *Service) Download(name, passphrase string, w io.Writer) error {
	if err := CheckPassphrase(passphrase); err != nil {
		return err
	}
	p, err := s.path(name)
	if err != nil {
		return err
	}
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	defer f.Close()
	return Encrypt(w, f, passphrase)
}
