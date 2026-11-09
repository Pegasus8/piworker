package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hostGuardRouter(t *testing.T, allowed ...string) http.Handler {
	t.Helper()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "app.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	manager := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	t.Cleanup(manager.StopAll)
	cfg := config.Default()
	cfg.Auth = false
	cfg.AllowedHosts = allowed
	router, _ := buildRouter(cfg, store, manager, node.DefaultRegistry, nil)
	return router
}

func getWithHost(h http.Handler, method, path, host string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Host = host
	r.Header.Set("Origin", "http://"+host)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// DNS rebinding: a page on attacker.example re-pointed at the Pi must not
// reach the API, the web UI or the MCP endpoint, even with auth disabled.
func TestRouterRejectsUnknownHosts(t *testing.T) {
	h := hostGuardRouter(t, "backend")
	for _, path := range []string{"/api/flows", "/api/health", "/", "/mcp"} {
		assert.Equalf(t, http.StatusForbidden, getWithHost(h, "GET", path, "attacker.example").Code, "GET %s", path)
	}
	// Preflights are rejected too, before CORS answers them.
	assert.Equal(t, http.StatusForbidden, getWithHost(h, "OPTIONS", "/api/flows", "attacker.example:8080").Code)
}

func TestRouterAcceptsKnownHosts(t *testing.T) {
	h := hostGuardRouter(t, "backend")
	for _, host := range []string{"backend:8080", "192.168.1.50:8080", "localhost:8080", "127.0.0.1"} {
		w := getWithHost(h, "GET", "/api/flows", host)
		assert.Equalf(t, http.StatusOK, w.Code, "Host %s: %s", host, w.Body.String())
	}
}

func TestRouterWebhooksIgnoreHost(t *testing.T) {
	h := hostGuardRouter(t)
	w := getWithHost(h, "POST", "/webhooks/not-registered", "sensor.example")
	assert.Equal(t, http.StatusNotFound, w.Code, "reaches the webhook hub, which knows no such path")
}
