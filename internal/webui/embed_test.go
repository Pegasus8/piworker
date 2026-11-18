package webui

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The web UI renders text that agents and flows supply (docs/design/mcp.md
// §9): its responses forbid inline and foreign scripts, plugins, <base>
// rewrites and framing, so an injection bug cannot run code.
func TestHandlerSendsContentSecurityPolicy(t *testing.T) {
	directives := map[string]string{}
	for _, d := range strings.Split(ContentSecurityPolicy, ";") {
		name, value, _ := strings.Cut(strings.TrimSpace(d), " ")
		directives[name] = value
	}
	assert.Equal(t, "'self'", directives["script-src"], "no inline, eval'd or foreign scripts")
	assert.Equal(t, "'none'", directives["object-src"])
	assert.Equal(t, "'self'", directives["base-uri"])
	assert.Equal(t, "'none'", directives["frame-ancestors"])
	assert.Equal(t, "'self'", directives["default-src"])
	assert.Equal(t, "'self'", directives["connect-src"], "the UI only talks to its own server")

	h := Handler()
	for path, status := range map[string]int{"/": http.StatusOK, "/agents/approvals": http.StatusOK, "/vite.svg": http.StatusOK, "/index.html": http.StatusMovedPermanently} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, status, w.Code, path)
		assert.Equal(t, ContentSecurityPolicy, w.Header().Get("Content-Security-Policy"), path)
	}
}

// script-src 'self' only works if the page needs no inline script: every
// <script> of the built index.html loads a file.
func TestIndexNeedsNoInlineScript(t *testing.T) {
	if !IsAvailable() {
		t.Skip("no built web UI embedded")
	}
	data, err := fs.ReadFile(distFS, "dist/index.html")
	require.NoError(t, err)
	scripts := regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script>`).FindAllStringSubmatch(string(data), -1)
	require.NotEmpty(t, scripts)
	for _, s := range scripts {
		assert.Regexp(t, `\bsrc=`, s[1], "inline script: %s", s[0])
		assert.Empty(t, strings.TrimSpace(s[2]), "inline script body: %s", s[0])
	}
	assert.NotRegexp(t, `(?i)\son[a-z]+\s*=`, string(data), "inline event handlers are scripts too")

	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	body, err := io.ReadAll(w.Result().Body)
	require.NoError(t, err)
	assert.Equal(t, string(data), string(body))
}
