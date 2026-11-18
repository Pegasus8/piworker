package api

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/backup"
	"github.com/Pegasus8/piworker/internal/backup/fakes3"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPassphrase = "correct horse battery staple"

func newRemoteBackupsEnv(t *testing.T) (backupsEnv, *fakes3.Server, map[string]any) {
	t.Helper()
	env := backupsEnv{agentTestEnv: newAgentTestEnv(t, AgentHandlerOptions{}), restarts: &atomic.Int32{}}
	env.dbPath = filepath.Join(t.TempDir(), "piworker.db")
	fake := fakes3.New("backups")
	url, tr := fakes3.Start(t, fake)
	env.svc = backup.NewService(backup.ServiceOptions{
		Sources: backup.Sources{DB: env.store.DB()}, DBPath: env.dbPath, Settings: env.store, RemoteTransport: tr,
	})
	NewBackupsHandler(BackupsOptions{Service: env.svc, Auth: env.auth, Logger: zerolog.Nop(), Restart: func() { env.restarts.Add(1) }}).RegisterRoutes(env.router)
	remote := map[string]any{"enabled": true, "endpoint": url, "bucket": "backups", "prefix": "pi", "accessKeyId": "AKID", "pathStyle": true}
	return env, fake, remote
}

func with(m map[string]any, kv ...any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	for i := 0; i < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

func TestRemoteSettingsNeedPasswordAndHideSecrets(t *testing.T) {
	env, fake, remote := newRemoteBackupsEnv(t)
	body := with(remote, "secretKey", "zzSECRETzz-key", "passphrase", testPassphrase)

	w := env.do("PUT", "/api/backups/remote", body)
	assert.Equal(t, http.StatusForbidden, w.Code, "the password is confirmed")
	w = env.do("PUT", "/api/backups/remote", with(body, "password", "test-password"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "zzSECRETzz")

	ov := data[BackupsOverview](t, env.do("GET", "/api/backups", nil))
	assert.True(t, ov.Remote.HasSecretKey)
	assert.True(t, ov.Remote.HasPassphrase)
	assert.Equal(t, "pi/", ov.Remote.Settings.Prefix)
	raw := env.do("GET", "/api/backups", nil).Body.String()
	assert.NotContains(t, raw, "zzSECRETzz")
	assert.NotContains(t, raw, testPassphrase)

	// Weak passphrase: 400.
	w = env.do("PUT", "/api/backups/remote", with(remote, "passphrase", "short", "password", "test-password"))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// A manual backup is copied there.
	created := data[backup.Info](t, env.do("POST", "/api/backups", nil))
	assert.Equal(t, []string{"pi/" + created.Name + ".age"}, fake.Keys())
	list := data[[]backup.Info](t, env.do("GET", "/api/backups/remote/list", nil))
	require.Len(t, list, 1)
	assert.Equal(t, created.Name, list[0].Name)
}

func TestRemoteTestNeverSendsTheStoredKeyElsewhere(t *testing.T) {
	env, fake, remote := newRemoteBackupsEnv(t)
	require.Equal(t, http.StatusOK, env.do("PUT", "/api/backups/remote", with(remote, "secretKey", "k", "passphrase", testPassphrase, "password", "test-password")).Code)

	w := env.do("POST", "/api/backups/remote/test", remote)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Another endpoint or key ID with the stored secret: refused.
	w = env.do("POST", "/api/backups/remote/test", with(remote, "endpoint", "https://evil.example"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w = env.do("POST", "/api/backups/remote/test", with(remote, "accessKeyId", "OTHER"))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// A failing destination answers the reason.
	fake.Deny(true)
	w = env.do("POST", "/api/backups/remote/test", remote)
	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, errorMessage(t, w), "Access Denied")
}

func TestRemoteRestore(t *testing.T) {
	env, _, remote := newRemoteBackupsEnv(t)
	require.Equal(t, http.StatusOK, env.do("PUT", "/api/backups/remote", with(remote, "secretKey", "k", "passphrase", testPassphrase, "password", "test-password")).Code)
	created := data[backup.Info](t, env.do("POST", "/api/backups", nil))
	path := "/api/backups/remote/" + created.Name + "/restore"
	assert.Equal(t, http.StatusForbidden, env.do("POST", path, map[string]any{}).Code)
	w := env.do("POST", path, map[string]any{"password": "test-password"})
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	assert.True(t, backup.Pending(env.dbPath))
	assert.EqualValues(t, 1, env.restarts.Load())
}

func TestDownloadIsEncryptedAndUploadRestores(t *testing.T) {
	env := newBackupsEnv(t, true)
	created := data[backup.Info](t, env.do("POST", "/api/backups", nil))
	path := "/api/backups/" + created.Name + "/download"

	assert.Equal(t, http.StatusForbidden, env.do("POST", path, map[string]any{"passphrase": testPassphrase}).Code)
	assert.Equal(t, http.StatusBadRequest, env.do("POST", path, map[string]any{"passphrase": "short", "password": "test-password"}).Code)
	w := env.do("POST", path, map[string]any{"passphrase": testPassphrase, "password": "test-password"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), created.Name+".age")
	sealed := w.Body.Bytes()
	assert.True(t, backup.IsEncrypted(sealed))

	up := map[string]any{"data": base64.StdEncoding.EncodeToString(sealed), "passphrase": testPassphrase}
	assert.Equal(t, http.StatusForbidden, env.do("POST", "/api/backups/upload", up).Code)
	w = env.do("POST", "/api/backups/upload", with(up, "password", "test-password", "passphrase", "wrong passphrase!"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, backup.Pending(env.dbPath))
	w = env.do("POST", "/api/backups/upload", with(up, "password", "test-password"))
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	assert.True(t, backup.Pending(env.dbPath))
	assert.EqualValues(t, 1, env.restarts.Load())
}

// Without authentication nothing confirms who downloads every secret: the
// download is refused, as the stores export with secret values is.
func TestDownloadRefusedWithoutAuthentication(t *testing.T) {
	env := newBackupsEnv(t, false)
	created := data[backup.Info](t, env.do("POST", "/api/backups", nil))
	w := env.do("POST", "/api/backups/"+created.Name+"/download", map[string]any{"passphrase": testPassphrase})
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// A fresh installation restores a backup on the setup page with its setup
// code instead of creating an account.
func TestSetupRestore(t *testing.T) {
	// An installation with a backup to move.
	old := newBackupsEnv(t, true)
	created := data[backup.Info](t, old.do("POST", "/api/backups", nil))
	raw, err := os.ReadFile(filepath.Join(old.svc.DefaultDir(), created.Name))
	require.NoError(t, err)
	var sealed bytes.Buffer
	require.NoError(t, old.svc.Download(created.Name, testPassphrase, &sealed))

	// A fresh one: no account, a setup code.
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "fresh.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	users, err := storage.NewSQLiteUserStoreWithDB(store.DB())
	require.NoError(t, err)
	code, err := users.NewAuthCode("setup", "", time.Now())
	require.NoError(t, err)
	auth := NewAuthHandler(users, false, nil)
	router := mux.NewRouter()
	auth.RegisterRoutes(router)
	router.Use(AuthMiddleware(auth))
	dbPath := filepath.Join(t.TempDir(), "piworker.db")
	svc := backup.NewService(backup.ServiceOptions{Sources: backup.Sources{DB: store.DB()}, DBPath: dbPath, Settings: store})
	var restarts atomic.Int32
	NewBackupsHandler(BackupsOptions{Service: svc, Auth: auth, Logger: zerolog.Nop(), Restart: func() { restarts.Add(1) }}).RegisterRoutes(router)
	post := func(body map[string]any) int {
		return authRequest(router, "POST", "/api/auth/setup/restore", body, nil).Code
	}

	enc := base64.StdEncoding.EncodeToString
	assert.Equal(t, http.StatusForbidden, post(map[string]any{"code": "wrong", "data": enc(raw)}))
	// A wrong passphrase does not use up the code.
	assert.Equal(t, http.StatusBadRequest, post(map[string]any{"code": code, "data": enc(sealed.Bytes()), "passphrase": "wrong passphrase!"}))
	assert.Equal(t, http.StatusBadRequest, post(map[string]any{"code": code, "data": enc([]byte("garbage"))}))
	assert.False(t, backup.Pending(dbPath))
	assert.Equal(t, http.StatusAccepted, post(map[string]any{"code": code, "data": enc(sealed.Bytes()), "passphrase": testPassphrase}))
	assert.True(t, backup.Pending(dbPath))
	assert.EqualValues(t, 1, restarts.Load())
	// The code is used up.
	assert.Equal(t, http.StatusForbidden, post(map[string]any{"code": code, "data": enc(raw)}))
	list, _ := svc.List()
	assert.Empty(t, list, "a fresh installation has nothing to back up first")

	// With an account the setup restore is closed, whatever the code.
	assert.Equal(t, http.StatusForbidden, authRequest(old.router, "POST", "/api/auth/setup/restore", map[string]any{"code": code, "data": enc(raw)}, nil).Code)
}
