package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newOpenApp is a router with authentication off (ADR-0002 allows it on a
// LAN Pi) and the given CORS origins.
func newOpenApp(t *testing.T, origins []string) http.Handler {
	t.Helper()
	store, err := storage.NewSQLiteStore(t.TempDir() + "/app.db")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	manager := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	t.Cleanup(manager.StopAll)
	cfg := config.Default()
	cfg.Auth = false
	cfg.CORSOrigins = origins
	router, _ := buildRouter(cfg, store, manager, node.DefaultRegistry, nil)
	return router
}

func fromOrigin(h http.Handler, method, path, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://192.168.1.10:8443"+path, nil)
	r.Header.Set("Origin", origin)
	if method != http.MethodGet {
		r.Header.Set("X-PiWorker-Request", "1")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// TestEmptyCorsOriginsFailClosed: an empty cors_origins (cors_origins = [],
// PIWORKER_CORS_ORIGINS=, -cors-origins "") answered every origin with
// "Access-Control-Allow-Origin: *", so with auth off any web page in the
// admin's browser could read the flows, variables and metrics of the Pi by
// its IP address. An empty list now means no cross-origin access
// (ADR-0004): no CORS headers grant anything.
func TestEmptyCorsOriginsFailClosed(t *testing.T) {
	for name, origins := range map[string][]string{
		"nil (environment or flag)": config.SplitOrigins(""),
		"empty (TOML)":              {},
	} {
		t.Run(name, func(t *testing.T) {
			app := newOpenApp(t, origins)
			for _, path := range []string{"/api/flows", "/api/variables", "/api/metrics"} {
				w := fromOrigin(app, http.MethodGet, path, "https://evil.example")
				assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"), path)
				assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"), path)
			}
			w := fromOrigin(app, http.MethodOptions, "/api/flows", "https://evil.example")
			assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"), "preflight")
		})
	}
}

// TestDefaultCorsOriginsAreEmpty: the editor is served from the same origin
// and needs no CORS, so the default trusts no other origin; the Vite dev
// server's origin (http://localhost:3000) is configured where the dev
// server runs, not in every production install.
func TestDefaultCorsOriginsAreEmpty(t *testing.T) {
	assert.Empty(t, config.Default().CORSOrigins)
	app := newOpenApp(t, config.Default().CORSOrigins)
	for _, origin := range []string{"http://localhost:3000", "http://localhost:8080"} {
		w := fromOrigin(app, http.MethodGet, "/api/flows", origin)
		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"), origin)
		w = fromOrigin(app, http.MethodPost, "/api/flows", origin)
		assert.Equal(t, http.StatusForbidden, w.Code, "%s may not write: %s", origin, w.Body.String())
	}

	// An origin the admin lists keeps its credentialed access.
	app = newOpenApp(t, []string{"http://localhost:3000"})
	w := fromOrigin(app, http.MethodGet, "/api/flows", "http://localhost:3000")
	assert.Equal(t, "http://localhost:3000", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}
