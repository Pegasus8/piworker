package main

import (
	"net/http"
	"testing"

	"github.com/Pegasus8/piworker/internal/webui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The editor pages served by the app carry the web UI's Content-Security-
// Policy (docs/design/mcp.md §9), with and without authentication.
func TestWebUIIsServedWithContentSecurityPolicy(t *testing.T) {
	for _, auth := range []bool{true, false} {
		app := newTestApp(t, auth)
		for _, path := range []string{"/", "/agents/approvals", "/trust"} {
			w := app.do("GET", path, "", "", nil)
			require.Equal(t, http.StatusOK, w.Code, path)
			assert.Equal(t, webui.ContentSecurityPolicy, w.Header().Get("Content-Security-Policy"), path)
		}
	}
}
