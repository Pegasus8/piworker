package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoggingMiddlewareBoundsFields: the access log cuts the method and the
// path (Go accepts either up to the header limit), and logs the path that
// was requested, not one a handler rewrote.
func TestLoggingMiddlewareBoundsFields(t *testing.T) {
	var buf bytes.Buffer
	h := LoggingMiddleware(zerolog.New(&buf))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/rewritten"
		w.WriteHeader(http.StatusNoContent)
	}))
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Method = strings.Repeat("M", 900<<10)
	r.URL.Path = "/" + strings.Repeat("p", 900<<10)
	h.ServeHTTP(httptest.NewRecorder(), r)

	assert.Less(t, buf.Len(), 1024)
	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	assert.True(t, strings.HasPrefix(line["method"].(string), "MMMM"))
	assert.True(t, strings.HasPrefix(line["path"].(string), "/pppp"))
	assert.Equal(t, float64(http.StatusNoContent), line["status"])
}

// TestHostGuardRefusalsAreLoggedAtDebug: a request refused for an unknown
// Host (which anyone on the LAN can send) leaves no access log line at info
// level; the guard's own debug line, and a debug access line, remain.
func TestHostGuardRefusalsAreLoggedAtDebug(t *testing.T) {
	env := newFakeHostEnv("pi")
	chain := func(level zerolog.Level) (http.Handler, *bytes.Buffer) {
		var buf bytes.Buffer
		h := LoggingMiddleware(zerolog.New(&buf).Level(level))(
			MetricsMiddleware(guarded(NewHostGuard(env.options()))))
		return h, &buf
	}

	h, logs := chain(zerolog.InfoLevel)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, requestWithHost(http.MethodGet, "/api/flows", "attacker.example"))
	require.Equal(t, http.StatusForbidden, w.Code)
	assert.Zero(t, logs.Len(), "nothing at info level")

	w = httptest.NewRecorder()
	h.ServeHTTP(w, requestWithHost(http.MethodGet, "/api/flows", "pi.local"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, logs.String(), `"level":"info"`, "allowed requests are logged as before")

	h, logs = chain(zerolog.DebugLevel)
	h.ServeHTTP(httptest.NewRecorder(), requestWithHost(http.MethodGet, "/api/flows", "attacker.example"))
	assert.Contains(t, logs.String(), `"level":"debug"`)
	assert.Contains(t, logs.String(), `"status":403`)
}
