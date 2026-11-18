package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/require"
)

func (a testApp) login(t *testing.T) map[string]string {
	t.Helper()
	w := a.do("POST", "/api/auth/login", "application/json", `{"username":"admin","password":"test-password"}`, editorHeaders)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	headers := map[string]string{"Cookie": w.Result().Cookies()[0].String()}
	for k, v := range editorHeaders {
		headers[k] = v
	}
	return headers
}

func TestAgentRoutesAreWiredWithAuth(t *testing.T) {
	app := newTestApp(t, true)
	require.Equal(t, http.StatusUnauthorized, app.do("GET", "/api/agent/tokens", "", "", nil).Code)
	headers := app.login(t)

	w := app.do("POST", "/api/agent/tokens", "application/json", `{"name":"laptop","scopes":["call"],"password":"test-password"}`, headers)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	_, _, ok := storage.ParseAPIToken(created.Data.Token)
	require.True(t, ok)

	for _, path := range []string{"/api/agent/tokens", "/api/agent/audit", "/api/agent/approvals", "/api/agent/settings"} {
		require.Equal(t, http.StatusOK, app.do("GET", path, "", "", headers).Code, path)
	}
}

func TestRecoveryRevokesAgentTokensInServer(t *testing.T) {
	app := newTestApp(t, true)
	tok, _, err := app.store.CreateAPIToken(storage.NewAPIToken{Name: "x", Scopes: []string{storage.ScopeCall}}, time.Now())
	require.NoError(t, err)
	code, err := app.users.NewAuthCode("recovery", "admin", time.Now())
	require.NoError(t, err)
	body := `{"username":"admin","password":"replacement-password","code":"` + code + `"}`
	w := app.do("POST", "/api/auth/recover", "application/json", body, editorHeaders)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	stored, err := app.store.GetAPIToken(tok.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.RevokedAt)
}

func TestAgentTokensCannotBeCreatedWithoutAuth(t *testing.T) {
	app := newTestApp(t, false)
	app.do("POST", "/api/agent/tokens", "application/json", `{"name":"x","scopes":["call"],"password":"x"}`, editorHeaders)
	list, err := app.store.ListAPITokens()
	require.NoError(t, err)
	require.Empty(t, list, "without accounts there is no step-up and no MCP, so no tokens")
}

// mcp.enabled without authentication must stop the process before it opens
// the database or listens.
func TestMCPWithoutAuthIsAStartupError(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "piworker")
	build := exec.Command("go", "build", "-o", binary, ".")
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))

	dir := t.TempDir()
	// A server that wrongly starts would run forever; the deadline turns that
	// into a failure.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-auth=false", "-addr", "127.0.0.1:0")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PIWORKER_MCP_ENABLED=true")
	output, err = cmd.CombinedOutput()
	require.NoError(t, ctx.Err(), "the server started instead of refusing the configuration")
	require.Error(t, err, string(output))
	require.Contains(t, string(output), "mcp.enabled")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "configuration is validated before the database is created")
}
