package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/api"
	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type testApp struct {
	handler http.Handler
	flows   *api.FlowsHandler
	store   *storage.SQLiteStore
	users   *storage.SQLiteUserStore
	manager *flow.RuntimeManager
}

func newTestApp(t *testing.T, auth bool) testApp {
	t.Helper()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "app.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	manager := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	t.Cleanup(manager.StopAll)
	cfg := config.Default()
	cfg.Auth = auth
	var users *storage.SQLiteUserStore
	if auth {
		users, err = storage.NewSQLiteUserStoreWithDB(store.DB())
		require.NoError(t, err)
		hash, err := api.HashPassword("test-password")
		require.NoError(t, err)
		require.NoError(t, users.CreateUser(&types.User{Username: "admin", PasswordHash: hash}))
	}
	router, flows := buildRouter(cfg, store, manager, node.DefaultRegistry, users)
	return testApp{handler: router, flows: flows, store: store, users: users, manager: manager}
}

func (a testApp) do(method, path, contentType, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://piworker.local"+path, bytes.NewBufferString(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	a.handler.ServeHTTP(w, r)
	return w
}

var editorHeaders = map[string]string{"X-PiWorker-Request": "1", "Origin": "http://piworker.local"}

func TestAuthDisabledStillRejectsCrossSiteWrites(t *testing.T) {
	app := newTestApp(t, false)
	// A malicious page can send this "simple request" without a preflight.
	w := app.do("POST", "/api/flows", "text/plain", `{"id":"evil","name":"evil"}`, map[string]string{"Origin": "http://evil.example"})
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	w = app.do("POST", "/api/flows/evil/deploy", "", "", map[string]string{"Sec-Fetch-Site": "cross-site"})
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	flows, err := app.store.ListFlows(nil)
	require.NoError(t, err)
	require.Empty(t, flows)

	// Even with the header, a text/plain body is refused.
	w = app.do("POST", "/api/flows", "text/plain", `{"name":"x"}`, editorHeaders)
	require.Equal(t, http.StatusUnsupportedMediaType, w.Code, w.Body.String())

	// The editor's own requests keep working.
	w = app.do("POST", "/api/flows", "application/json", `{"name":"ok"}`, editorHeaders)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

func TestMetricsRequireAuthWhenEnabled(t *testing.T) {
	app := newTestApp(t, true)
	require.Equal(t, http.StatusUnauthorized, app.do("GET", "/api/metrics", "", "", nil).Code)

	token, err := app.users.CreateSession("admin", mustHash(t, app.users), time.Now())
	require.NoError(t, err)
	r := httptest.NewRequest("GET", "http://piworker.local/api/metrics", nil)
	r.AddCookie(&http.Cookie{Name: "piworker_session", Value: token})
	w := httptest.NewRecorder()
	app.handler.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "go_goroutines")
}

func TestMetricsOpenWhenAuthDisabled(t *testing.T) {
	app := newTestApp(t, false)
	w := app.do("GET", "/api/metrics", "", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "go_goroutines")
}

// Metrics have a single home under /api, behind the same auth as the API.
func TestMetricsOnlyUnderAPI(t *testing.T) {
	for _, auth := range []bool{false, true} {
		app := newTestApp(t, auth)
		w := app.do("GET", "/metrics", "", "", nil)
		require.NotContains(t, w.Body.String(), "go_goroutines", "auth=%v", auth)
	}
}

func mustHash(t *testing.T, users *storage.SQLiteUserStore) string {
	t.Helper()
	u, err := users.GetUser("admin")
	require.NoError(t, err)
	return u.PasswordHash
}
