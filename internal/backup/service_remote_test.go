package backup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRemoteService(t *testing.T, in *install, c *clock, fake *fakeS3) (*Service, RemoteSettings) {
	t.Helper()
	rs, tr := startFakeS3(t, fake)
	s := NewService(ServiceOptions{
		Sources: in.sources(), DBPath: in.dbPath, Settings: in.store,
		Now: c.Now, Location: time.UTC, Jitter: func() time.Duration { return 0 },
		RemoteTransport: tr,
	})
	return s, rs
}

func TestSaveRemoteKeepsSecretsWriteOnly(t *testing.T) {
	in := newInstall(t)
	s, rs := newRemoteService(t, in, &clock{now: t0}, newFakeS3("backups"))

	// Enabling needs the secret key and the passphrase the first time.
	assert.ErrorIs(t, s.SaveRemote(rs, "", pass), ErrInvalidSettings)
	assert.ErrorIs(t, s.SaveRemote(rs, "secret", ""), ErrInvalidSettings)
	assert.ErrorIs(t, s.SaveRemote(rs, "secret", "short"), ErrWeakPassphrase)
	require.NoError(t, s.SaveRemote(rs, "secret", pass))

	st, err := s.RemoteState()
	require.NoError(t, err)
	assert.True(t, st.HasSecretKey)
	assert.True(t, st.HasPassphrase)
	assert.Equal(t, "pi/", st.Settings.Prefix)

	// Empty values keep the stored ones.
	rs.Prefix = "other"
	require.NoError(t, s.SaveRemote(rs, "", ""))
	assert.True(t, s.PassphraseMatches(pass))
	assert.False(t, s.PassphraseMatches("another passphrase"))

	// The local settings are separate: saving them keeps the remote.
	require.NoError(t, s.SaveSettings(DefaultSettings()))
	st, err = s.RemoteState()
	require.NoError(t, err)
	assert.True(t, st.Settings.Enabled)

	// Disabled settings need nothing else.
	require.NoError(t, s.SaveRemote(RemoteSettings{}, "", ""))
}

func TestBackUpUploadsEncryptedCopyAndPrunesRemote(t *testing.T) {
	in := newInstall(t)
	c := &clock{now: t0}
	fake := newFakeS3("backups")
	s, rs := newRemoteService(t, in, c, fake)
	require.NoError(t, s.SaveRemote(rs, "secret", pass))

	info, _, err := s.BackUp(context.Background(), ReasonScheduled)
	require.NoError(t, err)
	key := "pi/" + info.Name + EncryptedSuffix
	require.Equal(t, []string{key}, fake.Keys())
	st := s.Status()
	require.NotNil(t, st.RemoteLastSuccess)
	assert.Empty(t, st.RemoteLastError)

	// The remote copy is the local archive, encrypted.
	local, err := os.ReadFile(filepath.Join(s.DefaultDir(), info.Name))
	require.NoError(t, err)
	assert.False(t, bytes.Contains(fake.Object(key), local[:64]))
	var plain bytes.Buffer
	require.NoError(t, Decrypt(&plain, bytes.NewReader(fake.Object(key)), pass))
	assert.Equal(t, local, plain.Bytes())

	// Retention applies to the remote copies too.
	require.NoError(t, s.SaveSettings(Settings{Enabled: true, Time: "03:17", Retention: Retention{Daily: 1}}))
	mustExec(t, in.store.DB(), `UPDATE settings SET value = 'low' WHERE key = 'agent_deploy'`)
	c.now = t0.Add(48 * time.Hour)
	second, _, err := s.BackUp(context.Background(), ReasonScheduled)
	require.NoError(t, err)
	assert.Equal(t, []string{"pi/" + second.Name + EncryptedSuffix}, fake.Keys())

	// Copies of other tools under the prefix are never touched.
	fake.Put("pi/notes.txt", []byte("mine"))
	c.now = t0.Add(72 * time.Hour)
	mustExec(t, in.store.DB(), `UPDATE settings SET value = 'off' WHERE key = 'agent_deploy'`)
	_, _, err = s.BackUp(context.Background(), ReasonScheduled)
	require.NoError(t, err)
	assert.Contains(t, fake.Keys(), "pi/notes.txt")

	list, err := s.ListRemote(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.True(t, list[0].Manifest.Created.Equal(t0.Add(72*time.Hour)))
}

// An unchanged day makes no new backup, but a newest copy missing at the
// destination (a failed upload) is uploaded then.
func TestSkippedBackupStillUploadsMissingCopy(t *testing.T) {
	in := newInstall(t)
	c := &clock{now: t0}
	fake := newFakeS3("backups")
	s, rs := newRemoteService(t, in, c, fake)
	require.NoError(t, s.SaveRemote(rs, "secret", pass))
	fake.Deny(true)
	first, _, err := s.BackUp(context.Background(), ReasonScheduled)
	require.NoError(t, err)
	require.NotEmpty(t, s.Status().RemoteLastError)
	fake.Deny(false)

	c.now = t0.Add(24 * time.Hour)
	_, skipped, err := s.BackUp(context.Background(), ReasonScheduled)
	require.NoError(t, err)
	assert.True(t, skipped)
	assert.Equal(t, []string{"pi/" + first.Name + EncryptedSuffix}, fake.Keys())
	assert.Empty(t, s.Status().RemoteLastError)
}

func TestRemoteFailureKeepsLocalBackup(t *testing.T) {
	in := newInstall(t)
	c := &clock{now: t0}
	fake := newFakeS3("backups")
	s, rs := newRemoteService(t, in, c, fake)
	require.NoError(t, s.SaveRemote(rs, "secret", pass))
	fake.Deny(true)
	info, _, err := s.BackUp(context.Background(), ReasonManual)
	require.NoError(t, err, "the local backup succeeded")
	_, err = os.Stat(filepath.Join(s.DefaultDir(), info.Name))
	require.NoError(t, err)
	st := s.Status()
	assert.Contains(t, st.RemoteLastError, "Access Denied")
	assert.Empty(t, st.LastError)
	assert.Nil(t, st.RemoteLastSuccess)

	err = s.TestRemote(context.Background(), rs, "")
	assert.ErrorIs(t, err, ErrRemote)
	fake.Deny(false)
	require.NoError(t, s.TestRemote(context.Background(), rs, ""))
}

func TestStageRemoteRestore(t *testing.T) {
	in := newInstall(t)
	c := &clock{now: t0}
	fake := newFakeS3("backups")
	s, rs := newRemoteService(t, in, c, fake)
	require.NoError(t, s.SaveRemote(rs, "secret", pass))
	info, _, err := s.BackUp(context.Background(), ReasonManual)
	require.NoError(t, err)
	// Only the remote copy is left.
	require.NoError(t, os.Remove(filepath.Join(s.DefaultDir(), info.Name)))

	c.now = t0.Add(time.Hour)
	m, err := s.StageRemoteRestore(context.Background(), info.Name)
	require.NoError(t, err)
	assert.Equal(t, info.Manifest.ContentHash, m.ContentHash)
	assert.True(t, Pending(in.dbPath))

	_, err = s.StageRemoteRestore(context.Background(), "piworker-backup-20200101T000000Z.tar.gz")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStageUploadPlainAndEncrypted(t *testing.T) {
	in := newInstall(t)
	c := &clock{now: t0}
	s := newService(t, in, c)
	info, _, err := s.BackUp(context.Background(), ReasonManual)
	require.NoError(t, err)
	raw, err := os.ReadFile(filepath.Join(s.DefaultDir(), info.Name))
	require.NoError(t, err)

	// Plain archive, with a backup of the current state first.
	c.now = t0.Add(time.Hour)
	m, err := s.StageUpload(context.Background(), bytes.NewReader(raw), "", true)
	require.NoError(t, err)
	assert.Equal(t, info.Manifest.ContentHash, m.ContentHash)
	list, _ := s.List()
	assert.Equal(t, ReasonBeforeRestore, list[0].Manifest.Reason)
	require.NoError(t, CancelPending(in.dbPath))

	// Encrypted download, uploaded on a fresh installation (no backup first).
	var sealed bytes.Buffer
	require.NoError(t, s.Download(info.Name, pass, &sealed))
	assert.True(t, IsEncrypted(sealed.Bytes()))
	before, _ := s.List()
	_, err = s.StageUpload(context.Background(), bytes.NewReader(sealed.Bytes()), "", false)
	assert.ErrorIs(t, err, ErrWrongPassphrase)
	_, err = s.StageUpload(context.Background(), bytes.NewReader(sealed.Bytes()), "wrong passphrase!", false)
	assert.ErrorIs(t, err, ErrWrongPassphrase)
	assert.False(t, Pending(in.dbPath))
	_, err = s.StageUpload(context.Background(), bytes.NewReader(sealed.Bytes()), pass, false)
	require.NoError(t, err)
	assert.True(t, Pending(in.dbPath))
	after, _ := s.List()
	assert.Equal(t, len(before), len(after), "no backup first on a fresh installation")

	_, err = s.StageUpload(context.Background(), strings.NewReader("garbage"), "", false)
	assert.ErrorIs(t, err, ErrInvalidBackup)

	assert.ErrorIs(t, s.Download("nope.tar.gz", pass, &bytes.Buffer{}), ErrNotFound)
	assert.ErrorIs(t, s.Download(info.Name, "short", &bytes.Buffer{}), ErrWeakPassphrase)

	// Leftover work files are removed.
	ents, _ := os.ReadDir(in.dir)
	for _, e := range ents {
		assert.False(t, strings.HasPrefix(e.Name(), ".upload-"), e.Name())
	}
}
