package api

import (
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Pegasus8/piworker/internal/backup"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type backupsEnv struct {
	agentTestEnv
	svc      *backup.Service
	dbPath   string
	restarts *atomic.Int32
}

func newBackupsEnv(t *testing.T, withAuth bool) backupsEnv {
	t.Helper()
	env := backupsEnv{agentTestEnv: newAgentTestEnv(t, AgentHandlerOptions{}), restarts: &atomic.Int32{}}
	env.dbPath = filepath.Join(t.TempDir(), "piworker.db")
	env.svc = backup.NewService(backup.ServiceOptions{
		Sources:  backup.Sources{DB: env.store.DB(), Version: "v1.0.0"},
		DBPath:   env.dbPath,
		Settings: env.store,
	})
	auth := env.auth
	if !withAuth {
		auth = nil
	}
	NewBackupsHandler(BackupsOptions{
		Service: env.svc, Auth: auth, Logger: zerolog.Nop(),
		Restart: func() { env.restarts.Add(1) },
	}).RegisterRoutes(env.router)
	return env
}

func TestBackupsOverviewAndBackUpNow(t *testing.T) {
	env := newBackupsEnv(t, true)

	w := env.do("GET", "/api/backups", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	ov := data[BackupsOverview](t, w)
	assert.Equal(t, backup.DefaultSettings(), ov.Settings)
	assert.Equal(t, filepath.Join(filepath.Dir(env.dbPath), "backups"), ov.Dir)
	assert.Equal(t, ov.Dir, ov.DefaultDir)
	assert.Empty(t, ov.Backups)
	assert.NotNil(t, ov.NextRun)
	assert.False(t, ov.RestorePending)

	w = env.do("POST", "/api/backups", nil)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	created := data[backup.Info](t, w)
	assert.Equal(t, backup.ReasonManual, created.Manifest.Reason)

	ov = data[BackupsOverview](t, env.do("GET", "/api/backups", nil))
	require.Len(t, ov.Backups, 1)
	assert.Equal(t, created.Name, ov.Backups[0].Name)
	require.NotNil(t, ov.Status.LastSuccess)
	// The manifest's per-file checksums are not part of the listing.
	assert.Nil(t, ov.Backups[0].Manifest.Files)
}

func TestBackupSettingsNeedPasswordOnlyToChangeTheDirectory(t *testing.T) {
	env := newBackupsEnv(t, true)

	// Time and retention: no step-up.
	w := env.do("PUT", "/api/backups/settings", map[string]any{"enabled": true, "time": "22:05", "retention": map[string]int{"daily": 3, "weekly": 0, "monthly": 1}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	set, err := env.svc.Settings()
	require.NoError(t, err)
	assert.Equal(t, "22:05", set.Time)

	// Invalid settings: 400.
	w = env.do("PUT", "/api/backups/settings", map[string]any{"enabled": true, "time": "25:00", "retention": map[string]int{"daily": 1}})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// A new directory decides where copies of every secret go: the
	// password is confirmed.
	usb := filepath.Join(t.TempDir(), "usb")
	body := map[string]any{"enabled": true, "time": "22:05", "dir": usb, "retention": map[string]int{"daily": 3}}
	w = env.do("PUT", "/api/backups/settings", body)
	assert.Equal(t, http.StatusForbidden, w.Code)
	body["password"] = "wrong"
	w = env.do("PUT", "/api/backups/settings", body)
	assert.Equal(t, http.StatusForbidden, w.Code)
	body["password"] = "test-password"
	w = env.do("PUT", "/api/backups/settings", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	dir, err := env.svc.Dir()
	require.NoError(t, err)
	assert.Equal(t, usb, dir)

	// Unchanged directory: no password needed again.
	delete(body, "password")
	body["time"] = "01:00"
	w = env.do("PUT", "/api/backups/settings", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestRestoreNeedsPasswordStagesAndRestarts(t *testing.T) {
	env := newBackupsEnv(t, true)
	created := data[backup.Info](t, env.do("POST", "/api/backups", nil))
	path := "/api/backups/" + created.Name + "/restore"

	w := env.do("POST", path, map[string]any{})
	assert.Equal(t, http.StatusForbidden, w.Code)
	w = env.do("POST", path, map[string]any{"password": "wrong"})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.False(t, backup.Pending(env.dbPath))
	assert.EqualValues(t, 0, env.restarts.Load())

	w = env.do("POST", "/api/backups/piworker-backup-20200101T000000Z.tar.gz/restore", map[string]any{"password": "test-password"})
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = env.do("POST", path, map[string]any{"password": "test-password"})
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	res := data[BackupRestoreResponse](t, w)
	assert.True(t, res.Restarting)
	assert.True(t, backup.Pending(env.dbPath))
	assert.EqualValues(t, 1, env.restarts.Load())

	ov := data[BackupsOverview](t, env.do("GET", "/api/backups", nil))
	assert.True(t, ov.RestorePending)
}

// Without authentication anyone on the network can already do everything;
// restore works without a password (there is none to confirm).
func TestRestoreWithoutAuthentication(t *testing.T) {
	env := newBackupsEnv(t, false)
	created := data[backup.Info](t, env.do("POST", "/api/backups", nil))
	w := env.do("POST", "/api/backups/"+created.Name+"/restore", map[string]any{})
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
}

func TestDeleteBackup(t *testing.T) {
	env := newBackupsEnv(t, true)
	created := data[backup.Info](t, env.do("POST", "/api/backups", nil))
	w := env.do("DELETE", "/api/backups/notes.txt", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	w = env.do("DELETE", "/api/backups/"+created.Name, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	ov := data[BackupsOverview](t, env.do("GET", "/api/backups", nil))
	assert.Empty(t, ov.Backups)
}
