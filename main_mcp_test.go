package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/api"
	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/events"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/mcpserver"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mcpApp is the real router with MCP enabled, served over TLS.
type mcpApp struct {
	testApp
	srv *httptest.Server
}

// mcpAppOption adds a runtime manager option built on the app's store.
type mcpAppOption func(t *testing.T, store *storage.SQLiteStore) flow.ManagerOption

// recordRuns records the runs of the app's flows in its store, as main
// does.
func recordRuns(t *testing.T, store *storage.SQLiteStore) flow.ManagerOption {
	recorder := events.NewRecorder(store, events.WithLogger(zerolog.Nop()))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		recorder.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return flow.WithManagerObserver(recorder.Observe)
}

func newMCPApp(t *testing.T, enabled bool, opts ...mcpAppOption) mcpApp {
	t.Helper()
	return newMCPAppWith(t, func(cfg *config.Config) { cfg.MCP.Enabled = enabled }, opts...)
}

// newMCPAppWith is newMCPApp with MCP enabled unless configure says
// otherwise.
func newMCPAppWith(t *testing.T, configure func(*config.Config), opts ...mcpAppOption) mcpApp {
	t.Helper()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "app.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	var managerOpts []flow.ManagerOption
	for _, opt := range opts {
		managerOpts = append(managerOpts, opt(t, store))
	}
	manager := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop(), managerOpts...)
	t.Cleanup(manager.StopAll)
	cfg := config.Default()
	cfg.Auth = true
	cfg.MCP.Enabled = true
	cfg.AllowedHosts = []string{"piworker.local"}
	configure(&cfg)
	users, err := storage.NewSQLiteUserStoreWithDB(store.DB())
	require.NoError(t, err)
	hash, err := api.HashPassword("test-password")
	require.NoError(t, err)
	require.NoError(t, users.CreateUser(&types.User{Username: "admin", PasswordHash: hash}))
	router, flows := buildRouter(cfg, store, manager, node.DefaultRegistry, users)
	srv := httptest.NewTLSServer(router)
	t.Cleanup(srv.Close)
	return mcpApp{testApp: testApp{handler: router, flows: flows, store: store, users: users, manager: manager}, srv: srv}
}

// deployTool saves and deploys trigger-tool "tool" (named name) → reply and
// returns its published info.
func (a mcpApp) deployTool(t *testing.T, name string, cfg map[string]any) flow.ToolInfo {
	t.Helper()
	toolCfg := map[string]any{"toolName": name, "title": "Garage door"}
	for k, v := range cfg {
		toolCfg[k] = v
	}
	f := saveToolFlow(t, a.testApp, toolCfg)
	require.NoError(t, a.manager.Deploy(context.Background(), f))
	_, tools := a.manager.Tools()
	for _, ti := range tools {
		if ti.ToolName == name {
			return ti
		}
	}
	t.Fatalf("tool %s is not published", name)
	return flow.ToolInfo{}
}

// token creates an agent token and returns it with its raw secret.
func (a mcpApp) token(t *testing.T, name string, scopes []string, grants ...flow.ToolInfo) (*storage.APIToken, string) {
	t.Helper()
	req := storage.NewAPIToken{Name: name, Scopes: scopes}
	for _, ti := range grants {
		req.Grants = append(req.Grants, storage.TokenGrant{FlowID: ti.FlowID, TriggerNodeID: ti.TriggerNodeID, Fingerprint: ti.Fingerprint})
	}
	tok, raw, err := a.store.CreateAPIToken(req, time.Now())
	require.NoError(t, err)
	return tok, raw
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

// connect opens an MCP client session with raw, at the client's default
// protocol version unless version is set.
func (a mcpApp) connect(t *testing.T, raw, version string) (*mcp.ClientSession, error) {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:             a.srv.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerTransport{token: raw, base: a.srv.Client().Transport}},
		DisableStandaloneSSE: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var opts *mcp.ClientSessionOptions
	if version != "" {
		opts = &mcp.ClientSessionOptions{ProtocolVersion: version}
	}
	session, err := client.Connect(ctx, transport, opts)
	if err == nil {
		t.Cleanup(func() { session.Close() })
	}
	return session, err
}

func callTool(t *testing.T, s *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err, "tool outcomes are results, not protocol errors")
	if res.IsError {
		// Clients validate structured content against the tool's output
		// schema even on errors: an error carries its envelope as text.
		assert.Nil(t, res.StructuredContent, "%s: an isError result carries no structured content", name)
	}
	return res
}

// structured decodes what a result says: its structured content, or the
// JSON envelope in the text block of an isError result.
func structured(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	var data []byte
	if res.IsError && res.StructuredContent == nil {
		require.Len(t, res.Content, 1)
		data = []byte(res.Content[0].(*mcp.TextContent).Text)
	} else {
		var err error
		data, err = json.Marshal(res.StructuredContent)
		require.NoError(t, err)
	}
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

func errCode(m map[string]any) string {
	e, _ := m["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

// at0618 is the protocol version header of a request after a 2025-06-18
// handshake.
var at0618 = map[string]string{"Mcp-Protocol-Version": "2025-06-18"}

// post sends a raw JSON-RPC request to /mcp over TLS.
func (a mcpApp) post(t *testing.T, raw, body string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, a.srv.URL+"/mcp", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if raw != "" {
		req.Header.Set("Authorization", "Bearer "+raw)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := a.srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, data
}

// An agent lists and calls exactly the tools granted to its token, with
// structured results and validation issues; hidden tools look unknown.
func TestMCPToolsEndToEnd(t *testing.T) {
	app := newMCPApp(t, true)
	garage := app.deployTool(t, "garage", nil)
	hidden := app.deployTool(t, "boiler", nil)
	_, raw := app.token(t, "laptop", []string{storage.ScopeCall}, garage)
	require.NotEmpty(t, hidden.Fingerprint)

	session, err := app.connect(t, raw, "")
	require.NoError(t, err)
	assert.Equal(t, "2026-07-28", session.InitializeResult().ProtocolVersion, "the current protocol, via server/discover")

	ctx := context.Background()
	list, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	names := map[string]*mcp.Tool{}
	for _, tool := range list.Tools {
		names[tool.Name] = tool
	}
	require.Len(t, names, 2, "the granted tool and pw_get_result")
	tool := names["garage"]
	require.NotNil(t, tool)
	assert.NotContains(t, names, "boiler")
	assert.Contains(t, names, "pw_get_result")
	assert.Equal(t, "Garage door", tool.Title)
	require.NotNil(t, tool.Annotations)
	assert.True(t, tool.Annotations.ReadOnlyHint, "only a reply is reachable")
	require.NotNil(t, tool.Annotations.DestructiveHint)
	assert.False(t, *tool.Annotations.DestructiveHint)
	require.NotNil(t, tool.Annotations.OpenWorldHint)
	assert.False(t, *tool.Annotations.OpenWorldHint)
	schema, err := json.Marshal(tool.InputSchema)
	require.NoError(t, err)
	assert.JSONEq(t, string(garage.Schema.JSON()), string(schema))

	res := callTool(t, session, "garage", map[string]any{"door": "left"})
	assert.False(t, res.IsError)
	assert.Equal(t, map[string]any{"door": "left"}, structured(t, res))
	require.Len(t, res.Content, 1)
	assert.JSONEq(t, `{"door":"left"}`, res.Content[0].(*mcp.TextContent).Text)

	res = callTool(t, session, "garage", map[string]any{"door": "up"})
	assert.True(t, res.IsError)
	invalid := structured(t, res)
	assert.Equal(t, "invalid_arguments", errCode(invalid))
	issues := invalid["error"].(map[string]any)["issues"].([]any)
	require.Len(t, issues, 1)
	assert.Equal(t, "/door", issues[0].(map[string]any)["pointer"])
	assert.NotEmpty(t, issues[0].(map[string]any)["message"])

	// A deployed tool that is not granted is answered exactly like a name
	// that does not exist.
	hiddenRes := callTool(t, session, "boiler", map[string]any{"door": "left"})
	unknownRes := callTool(t, session, "no_such", map[string]any{"door": "left"})
	assert.True(t, hiddenRes.IsError)
	assert.Equal(t, "not_found", errCode(structured(t, hiddenRes)))
	hiddenJSON, _ := json.Marshal(hiddenRes)
	unknownJSON, _ := json.Marshal(unknownRes)
	assert.Equal(t, strings.ReplaceAll(string(hiddenJSON), "boiler", "X"), strings.ReplaceAll(string(unknownJSON), "no_such", "X"))
}

// The older protocol versions work through the initialize handshake.
func TestMCPLegacyHandshake(t *testing.T) {
	app := newMCPApp(t, true)
	garage := app.deployTool(t, "garage", nil)
	_, raw := app.token(t, "laptop", []string{storage.ScopeCall}, garage)
	for _, version := range []string{"2025-11-25", "2025-06-18"} {
		session, err := app.connect(t, raw, version)
		require.NoError(t, err, version)
		assert.Equal(t, version, session.InitializeResult().ProtocolVersion)
		res := callTool(t, session, "garage", map[string]any{"door": "right"})
		assert.False(t, res.IsError, version)
		assert.Equal(t, map[string]any{"door": "right"}, structured(t, res))
	}
}

// A confirm:admin tool queues an approval; once the admin approves it in
// the editor, pw_get_result returns the stored result to the requesting
// token only.
func TestMCPApprovalFlow(t *testing.T) {
	app := newMCPApp(t, true)
	gate := app.deployTool(t, "gate", map[string]any{"confirm": "admin"})
	_, raw := app.token(t, "laptop", []string{storage.ScopeCall}, gate)
	_, otherRaw := app.token(t, "desk", []string{storage.ScopeCall}, gate)

	session, err := app.connect(t, raw, "")
	require.NoError(t, err)
	list, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	for _, tool := range list.Tools {
		if tool.Name == "gate" {
			assert.Equal(t, true, tool.Meta["anthropic/requiresUserInteraction"])
		}
	}

	res := callTool(t, session, "gate", map[string]any{"door": "right"})
	assert.True(t, res.IsError, "nothing ran yet")
	pending := structured(t, res)
	assert.Equal(t, "approval_required", pending["status"])
	runID, _ := pending["runId"].(string)
	require.NotEmpty(t, runID)

	waiting := callTool(t, session, "pw_get_result", map[string]any{"runId": runID})
	assert.False(t, waiting.IsError)
	assert.Equal(t, "approval_required", structured(t, waiting)["status"])

	headers := app.login(t)
	w := app.do("POST", "/api/agent/approvals/"+runID+"/approve", "", "", headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	done := callTool(t, session, "pw_get_result", map[string]any{"runId": runID})
	assert.False(t, done.IsError)
	got := structured(t, done)
	assert.Equal(t, "completed", got["status"])
	assert.Equal(t, map[string]any{"door": "right"}, got["result"])

	// Another token cannot read it: the run is unknown to it.
	other, err := app.connect(t, otherRaw, "")
	require.NoError(t, err)
	foreign := callTool(t, other, "pw_get_result", map[string]any{"runId": runID})
	assert.True(t, foreign.IsError)
	assert.Equal(t, "run_not_found", errCode(structured(t, foreign)))
}

// Authentication: 401 with a Bearer challenge and a JSON body without a
// token or with a bad, revoked one; 403 for a tool call without the call
// scope.
func TestMCPAuthentication(t *testing.T) {
	app := newMCPApp(t, true)
	garage := app.deployTool(t, "garage", nil)
	listBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

	_, err := app.connect(t, "", "")
	assert.Error(t, err)
	for _, raw := range []string{"", "pwk_nottherightshape", "pwk_aaaaaaaaaaaaaaaa_" + strings.Repeat("b", 52)} {
		resp, body := app.post(t, raw, listBody, at0618)
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, string(body))
		assert.True(t, strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), `Bearer realm="piworker"`))
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
		var envelope struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		require.NoError(t, json.Unmarshal(body, &envelope), string(body))
		assert.False(t, envelope.Success)
		assert.NotEmpty(t, envelope.Error)
	}

	// The read scope is served, but calling a tool needs the call scope.
	_, readRaw := app.token(t, "reader", []string{storage.ScopeRead}, garage)
	resp, body := app.post(t, readRaw, listBody, at0618)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	resp, body = app.post(t, readRaw, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"garage","arguments":{"door":"left"}}}`, at0618)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, string(body))
	assert.Contains(t, string(body), "call scope")
	reader, err := app.connect(t, readRaw, "")
	require.NoError(t, err)
	_, err = reader.CallTool(context.Background(), &mcp.CallToolParams{Name: "garage", Arguments: map[string]any{"door": "left"}})
	assert.Error(t, err, "refused before the tool runs")

	// Revoking the token in the editor cuts it off at once.
	tok, raw := app.token(t, "laptop", []string{storage.ScopeCall}, garage)
	resp, _ = app.post(t, raw, listBody, at0618)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	headers := app.login(t)
	w := app.do("DELETE", "/api/agent/tokens/"+tok.ID, "", "", headers)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	resp, _ = app.post(t, raw, listBody, at0618)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// Agent tokens work only on /mcp and sessions only on /api (ADR-0010).
func TestBearerTokensAreRejectedOnTheAPI(t *testing.T) {
	app := newMCPApp(t, true)
	_, raw := app.token(t, "laptop", []string{storage.ScopeCall, storage.ScopeRead})
	for _, path := range []string{"/api/flows", "/api/agent/tokens", "/api/agent/tools"} {
		w := app.do("GET", path, "", "", map[string]string{"Authorization": "Bearer " + raw})
		assert.Equal(t, http.StatusUnauthorized, w.Code, path)
	}
	w := app.do("POST", "/api/flows", "application/json", `{"name":"x"}`,
		map[string]string{"Authorization": "Bearer " + raw, "X-PiWorker-Request": "1"})
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// The editor's session cookie does not open /mcp.
	headers := app.login(t)
	r := httptest.NewRequest(http.MethodPost, "https://piworker.local/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	r.Header.Set("Origin", "https://piworker.local") // the editor's own page
	rec := httptest.NewRecorder()
	app.handler.ServeHTTP(rec, r)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// /mcp is outside /api: the request guard of ADR-0002 (X-PiWorker-Request,
// JSON-only bodies) does not apply, since a Bearer token cannot be sent by a
// cross-site page without a CORS preflight the router never grants.
func TestMCPIsNotBehindTheAPIRequestGuard(t *testing.T) {
	app := newMCPApp(t, true)
	_, raw := app.token(t, "laptop", []string{storage.ScopeCall})
	resp, body := app.post(t, raw, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		map[string]string{"Sec-Fetch-Site": "cross-site", "Mcp-Protocol-Version": "2025-06-18"})
	assert.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	// A preflight never allows the Authorization header.
	req, err := http.NewRequest(http.MethodOptions, app.srv.URL+"/mcp", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization")
	pre, err := app.srv.Client().Do(req)
	require.NoError(t, err)
	pre.Body.Close()
	assert.NotContains(t, strings.ToLower(pre.Header.Get("Access-Control-Allow-Headers")), "authorization")

	// A browser page of another origin is refused outright.
	resp, _ = app.post(t, raw, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		map[string]string{"Origin": "https://evil.example", "Mcp-Protocol-Version": "2025-06-18"})
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// Disabled MCP and OAuth discovery answer JSON 404s.
func TestMCPMounting(t *testing.T) {
	disabled := newMCPApp(t, false)
	enabled := newMCPApp(t, true)
	for name, app := range map[string]mcpApp{"disabled": disabled, "enabled": enabled} {
		paths := []string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-protected-resource/mcp", "/.well-known/openid-configuration"}
		if name == "disabled" {
			paths = append(paths, "/mcp")
		}
		for _, path := range paths {
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				req, err := http.NewRequest(method, app.srv.URL+path, bytes.NewReader(nil))
				require.NoError(t, err)
				resp, err := app.srv.Client().Do(req)
				require.NoError(t, err)
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				assert.Equal(t, http.StatusNotFound, resp.StatusCode, "%s %s %s", name, method, path)
				assert.Equal(t, "application/json", resp.Header.Get("Content-Type"), "%s %s", name, path)
				assert.True(t, json.Valid(body), string(body))
			}
		}
	}
	// Enabled, /mcp takes every method (the SDK answers 405 to GET in
	// stateless mode, after authentication).
	_, raw := enabled.token(t, "laptop", []string{storage.ScopeCall})
	req, err := http.NewRequest(http.MethodGet, enabled.srv.URL+"/mcp", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+raw)
	resp, err := enabled.srv.Client().Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	resp, _ = enabled.post(t, "", `{}`, nil)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// updateGoldens rewrites the golden files under testdata instead of
// comparing with them: a golden changes only when a test runs with -update,
// and the change is reviewed as a diff of the file.
var updateGoldens = flag.Bool("update", false, "rewrite the golden files under testdata instead of comparing with them")

// assertGolden compares v, encoded as indented JSON (HTML characters kept as
// they are), with the golden file at path; -update rewrites the file first.
func assertGolden(t *testing.T, path string, v any) {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	require.NoError(t, enc.Encode(v))
	if *updateGoldens {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "go test -run '%s' -update creates %s", t.Name(), path)
	if got := buf.String(); got != string(want) {
		t.Errorf("%s differs (- golden, + now); review it, then rewrite the file with -update:\n%s", path, lineDiff(string(want), got))
	}
}

// lineDiff shows the lines between the first and the last that differ,
// with three lines of context, at most 200 lines.
func lineDiff(want, got string) string {
	a, b := strings.Split(want, "\n"), strings.Split(got, "\n")
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA, endB = endA-1, endB-1
	}
	var out []string
	for _, l := range a[max(0, start-3):start] {
		out = append(out, "  "+l)
	}
	for _, l := range a[start:endA] {
		out = append(out, "- "+l)
	}
	for _, l := range b[start:endB] {
		out = append(out, "+ "+l)
	}
	for _, l := range a[endA:min(len(a), endA+3)] {
		out = append(out, "  "+l)
	}
	if len(out) > 200 {
		out = append(out[:200], "…")
	}
	return fmt.Sprintf("@@ line %d @@\n%s", start+1, strings.Join(out, "\n"))
}

// sharedToolsGolden pins the author and deploy tools as tools/list lists
// them (the adapter's own golden, one source of truth): the exchange
// goldens refer to it instead of repeating them.
const sharedToolsGolden = "internal/mcpserver/testdata/tools_author_deploy.golden.json"

// goldenClientInfo is the client the exchanges name.
var goldenClientInfo = map[string]any{"name": "golden", "version": "1"}

// Volatile values of an exchange: generated ids and durations (in text
// blocks too, where quotes are escaped).
var (
	uuidPattern     = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	durationPattern = regexp.MustCompile(`,\\*"durationMs\\*":\d+`)
)

// goldenExchange is one raw JSON-RPC exchange with /mcp as a golden file
// pins it: the request's MCP headers and body (never its token) and the
// answer's HTTP status and body (its content type when it is not JSON).
type goldenExchange struct {
	Name        string            `json:"name"`
	Headers     map[string]string `json:"headers,omitempty"`
	Request     any               `json:"request"`
	Status      int               `json:"status"`
	ContentType string            `json:"contentType,omitempty"`
	Response    any               `json:"response"`
}

// goldenTranscript sends raw JSON-RPC requests to /mcp over TLS as a client
// of one protocol version sends them, and records the exchanges.
type goldenTranscript struct {
	app       mcpApp
	version   string
	lastID    int
	ids       map[string]string          // generated id → its placeholder
	shared    map[string]json.RawMessage // the tools sharedToolsGolden pins, by name
	exchanges []goldenExchange
}

func newGoldenTranscript(t *testing.T, app mcpApp, version string) *goldenTranscript {
	t.Helper()
	data, err := os.ReadFile(filepath.FromSlash(sharedToolsGolden))
	require.NoError(t, err)
	var tools []json.RawMessage
	require.NoError(t, json.Unmarshal(data, &tools))
	shared := map[string]json.RawMessage{}
	for _, tool := range tools {
		var named struct{ Name string }
		require.NoError(t, json.Unmarshal(tool, &named))
		shared[named.Name] = tool
	}
	return &goldenTranscript{app: app, version: version, ids: map[string]string{}, shared: shared}
}

// send posts a request of method with params as the token raw, records the
// exchange as step and returns the answer as it came. initialize is sent as
// the legacy handshake (no version header); at 2026-07-28 every other
// request carries the per-request _meta and the Mcp-Method header, and
// Mcp-Name when it names a tool or a prompt.
func (g *goldenTranscript) send(t *testing.T, raw, step, method string, params map[string]any) map[string]any {
	t.Helper()
	headers := map[string]string{}
	if method != "initialize" {
		headers["Mcp-Protocol-Version"] = g.version
		if g.version >= "2026-07-28" {
			params = maps.Clone(params)
			if params == nil {
				params = map[string]any{}
			}
			params["_meta"] = map[string]any{
				"io.modelcontextprotocol/protocolVersion":    g.version,
				"io.modelcontextprotocol/clientInfo":         goldenClientInfo,
				"io.modelcontextprotocol/clientCapabilities": map[string]any{},
			}
			headers["Mcp-Method"] = method
			if name, ok := params["name"]; ok && (method == "tools/call" || method == "prompts/get") {
				headers["Mcp-Name"] = fmt.Sprint(name)
			}
		}
	}
	g.lastID++
	msg := map[string]any{"jsonrpc": "2.0", "id": g.lastID, "method": method}
	if params != nil {
		msg["params"] = params
	}
	body := mustJSON(t, msg)
	resp, data := g.app.post(t, raw, string(body), headers)
	ex := goldenExchange{Name: step, Headers: headers, Request: g.normalise(t, body), Status: resp.StatusCode}
	var answer map[string]any
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		ex.ContentType, ex.Response = ct, string(data)
	} else {
		require.NoError(t, json.Unmarshal(data, &answer), "%s: %s", step, data)
		ex.Response = g.normalise(t, data)
	}
	g.exchanges = append(g.exchanges, ex)
	return answer
}

// call sends a tools/call of tool with args.
func (g *goldenTranscript) call(t *testing.T, raw, step, tool string, args map[string]any) map[string]any {
	t.Helper()
	return g.send(t, raw, step, "tools/call", map[string]any{"name": tool, "arguments": args})
}

// normalise decodes a message with its volatile values replaced: generated
// ids by placeholders numbered in order of appearance in the transcript (an
// id reads the same everywhere, text blocks included) and durations
// (absent when 0 ms) removed. The author and deploy tools of a tools/list
// answer, once checked equal to sharedToolsGolden, refer to it.
func (g *goldenTranscript) normalise(t *testing.T, data []byte) any {
	t.Helper()
	text := uuidPattern.ReplaceAllStringFunc(string(data), func(id string) string {
		if _, ok := g.ids[id]; !ok {
			g.ids[id] = fmt.Sprintf("<id-%d>", len(g.ids)+1)
		}
		return g.ids[id]
	})
	text = durationPattern.ReplaceAllString(text, "")
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var v any
	require.NoError(t, dec.Decode(&v), text)
	result, _ := v.(map[string]any)["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	for i, tool := range tools {
		name, _ := tool.(map[string]any)["name"].(string)
		if want, ok := g.shared[name]; ok &&
			assert.JSONEq(t, string(want), string(mustJSON(t, tool)), "%s at %s differs from %s", name, g.version, sharedToolsGolden) {
			tools[i] = map[string]any{"name": name, "sameAs": sharedToolsGolden}
		}
	}
	return v
}

// goldenToolFlow stores and deploys flow id: trigger-tool "tool" (named
// name, with cfg) → action-reply "reply", and returns the published tool.
func goldenToolFlow(t *testing.T, app mcpApp, id, flowName, name string, cfg map[string]any) flow.ToolInfo {
	t.Helper()
	toolCfg := map[string]any{
		"toolName":    name,
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"door": map[string]any{"type": "string", "enum": []any{"left", "right"}}}},
	}
	maps.Copy(toolCfg, cfg)
	f := flow.NewFlow(flowName)
	f.ID = id
	f.AddNode(*newNode("tool", "trigger-tool", flow.NodeCategoryInput, toolCfg))
	f.AddNode(*newNode("reply", "action-reply", flow.NodeCategoryOutput, nil))
	f.AddConnection(*flow.NewConnection("tool", "output", "reply", "input"))
	require.NoError(t, app.store.CreateFlow(f))
	require.NoError(t, app.manager.Deploy(context.Background(), f))
	_, tools := app.manager.Tools()
	for _, ti := range tools {
		if ti.ToolName == name {
			return ti
		}
	}
	t.Fatalf("tool %s is not published", name)
	return flow.ToolInfo{}
}

// Golden raw JSON-RPC exchanges over the real router and TLS, for every
// protocol version /mcp negotiates (testdata/mcp_exchanges_<version>.golden.json;
// go test -run TestMCPProtocolGolden . -update rewrites them): the
// handshake, tools/list of a token with every scope and the privileged flag
// (author enabled, agent deploy all), flow tool calls (a reply, a tool
// error, an approval), the read, author and deploy tools' answers and
// refusals, prompts and protocol errors. Each version runs on its own app,
// so the transcripts differ only where the protocol versions do.
func TestMCPProtocolGolden(t *testing.T) {
	everything := []string{storage.ScopeCall, storage.ScopeRead, storage.ScopeAuthor, storage.ScopeDeploy}
	answers := map[string]map[string]any{} // tool answers by version
	for _, version := range mcpserver.ProtocolVersions {
		t.Run(version, func(t *testing.T) {
			app := newAuthorApp(t, true, storage.AgentDeployAll)
			garage := goldenToolFlow(t, app, "flow-garage", "Garage", "garage",
				map[string]any{"title": "Garage door", "description": "Opens the left or right garage door and reports whether it moved."})
			gate := goldenToolFlow(t, app, "flow-gate", "Gate", "gate",
				map[string]any{"title": "Front gate", "description": "Opens the front gate once the admin approves.", "confirm": "admin"})
			report := flow.NewFlow("Report")
			report.ID = "flow-report"
			report.AddNode(*manualNode("start"))
			report.AddNode(*httpNode("http", map[string]any{"url": "https://api.example.com/report", "method": "POST"}))
			report.AddConnection(*flow.NewConnection("start", "output", "http", "input"))
			require.NoError(t, app.store.CreateFlow(report))
			// The admin edits the report in the editor: version 2.
			editor := app.editor(t)
			stored := editorGet[struct {
				Flow map[string]any `json:"flow"`
			}](t, editor, "/api/flows/"+report.ID).Flow
			stored["description"] = "Posts the daily report."
			code, body := editor.do(t, http.MethodPut, "/api/flows/"+report.ID, stored)
			require.Equal(t, http.StatusOK, code, string(body))

			// Every request has a token of its own (each token may send 10
			// requests at once), all alike: every scope, privileged, both
			// tools granted.
			token := func() string {
				_, raw, err := app.store.CreateAPIToken(storage.NewAPIToken{Name: "agent", Scopes: everything, Privileged: true,
					Grants: []storage.TokenGrant{
						{FlowID: garage.FlowID, TriggerNodeID: garage.TriggerNodeID, Fingerprint: garage.Fingerprint},
						{FlowID: gate.FlowID, TriggerNodeID: gate.TriggerNodeID, Fingerprint: gate.Fingerprint},
					}}, time.Now())
				require.NoError(t, err)
				return raw
			}
			// A token drafts a change of the report's destination, which low
			// refuses to deploy.
			deployer := token()
			session, err := app.connect(t, deployer, "")
			require.NoError(t, err)
			draft := callOK[service.DraftResult](t, agentSession{session: session}, "pw_save_draft", map[string]any{
				"flowId": report.ID, "baseVersion": 2, "note": "Report to the new endpoint.",
				"ops": []any{map[string]any{"op": "update_node", "nodeId": "http", "config": map[string]any{"url": "https://exfil.example.net/report"}}},
			})

			g := newGoldenTranscript(t, app, version)
			initialize := map[string]any{"protocolVersion": version, "capabilities": map[string]any{}, "clientInfo": goldenClientInfo}
			if version >= "2026-07-28" {
				g.send(t, token(), "initialize asking for 2026-07-28 settles on the newest handshake version", "initialize", initialize)
				g.send(t, token(), "server/discover", "server/discover", nil)
			} else {
				g.send(t, token(), "initialize", "initialize", initialize)
			}
			g.send(t, token(), "tools/list: every scope and the privileged flag, author enabled, agent deploy all", "tools/list", nil)

			g.call(t, token(), "flow tool: completed", "garage", map[string]any{"door": "left"})
			g.call(t, token(), "flow tool: arguments outside its schema", "garage", map[string]any{"door": "up"})
			approver := token()
			pending := g.call(t, approver, "confirm: admin: approval required", "gate", map[string]any{"door": "right"})
			runID, _ := pending["result"].(map[string]any)["_meta"].(map[string]any)["piworker/runId"].(string)
			require.NotEmpty(t, runID, "%v", pending)
			g.call(t, approver, "pw_get_result: still waiting for the admin", "pw_get_result", map[string]any{"runId": runID})
			code, body = editor.do(t, http.MethodPost, "/api/agent/approvals/"+runID+"/approve", nil)
			require.Equal(t, http.StatusOK, code, string(body))
			g.call(t, approver, "pw_get_result: completed once the admin approved", "pw_get_result", map[string]any{"runId": runID})

			g.call(t, token(), "pw_list_flows", "pw_list_flows", map[string]any{})
			invalid := agentChain("", agentNode("start", "trigger-manual", nil), agentNode("calc", "process-nope", nil),
				agentNode("double", "process-transform", map[string]any{"expression": 42}))
			invalid["nodes"] = append(invalid["nodes"].([]any), agentNode("double", "action-log", nil))
			g.call(t, token(), "pw_validate_flow: a flow with issues", "pw_validate_flow", map[string]any{"flow": invalid})
			g.call(t, token(), "pw_save_draft: a stale baseVersion", "pw_save_draft", map[string]any{
				"flowId": report.ID, "baseVersion": 1, "note": "Report hourly.",
				"ops": []any{map[string]any{"op": "update_node", "nodeId": "http", "config": map[string]any{"method": "PUT"}}},
			})
			g.call(t, token(), "built-in tool: arguments outside its schema", "pw_list_runs",
				map[string]any{"flowId": garage.FlowID, "limit": 0, "page": 2})
			g.send(t, token(), "tools/call: invalid params (the name is not a string)", "tools/call",
				map[string]any{"name": 42, "arguments": map[string]any{}})
			g.call(t, token(), "unknown tool, with the call scope", "pw_no_such_tool", map[string]any{})

			g.send(t, token(), "prompts/list", "prompts/list", nil)
			g.send(t, token(), "prompts/get diagnose_flow", "prompts/get",
				map[string]any{"name": "diagnose_flow", "arguments": map[string]any{"flowId": garage.FlowID}})
			g.send(t, token(), "prompts/get author_flow", "prompts/get",
				map[string]any{"name": "author_flow", "arguments": map[string]any{"goal": "Post the garage door's state to the report endpoint every hour."}})
			g.send(t, token(), "prompts/get: invalid params", "prompts/get",
				map[string]any{"name": "diagnose_flow", "arguments": map[string]any{}})
			g.send(t, token(), "ping", "ping", nil)
			g.send(t, token(), "unknown method", "flows/list", nil)
			g.send(t, token(), "subscriptions/listen: lists never change", "subscriptions/listen",
				map[string]any{"notifications": map[string]any{"toolsListChanged": true}})

			require.NoError(t, app.store.SetAgentDeployLevel(storage.AgentDeployLow))
			g.call(t, deployer, "pw_deploy_flow at agent deploy low: a destination change is refused", "pw_deploy_flow",
				map[string]any{"flowId": report.ID, "draftId": draft.DraftID})

			assertGolden(t, filepath.Join("testdata", "mcp_exchanges_"+version+".golden.json"), g.exchanges)
			answers[version] = toolAnswers(t, version, g.exchanges)
		})
	}
	// Every tool call is answered alike at every version (that ran: -run
	// may pick one).
	newest := mcpserver.ProtocolVersions[0]
	for _, version := range mcpserver.ProtocolVersions[1:] {
		if answers[newest] != nil && answers[version] != nil {
			assert.Equal(t, answers[newest], answers[version], version)
		}
	}
}

// toolAnswers are the results of a transcript's tools/call exchanges, by
// exchange, without what the sessionless protocol (2026-07-28) adds to
// every result: "resultType": "complete" and the server's identity in
// _meta, checked here. (Protocol errors are not results.)
func toolAnswers(t *testing.T, version string, exchanges []goldenExchange) map[string]any {
	t.Helper()
	out := map[string]any{}
	for _, ex := range exchanges {
		answer, _ := ex.Response.(map[string]any)
		result, ok := answer["result"].(map[string]any)
		if ex.Request.(map[string]any)["method"] != "tools/call" || !ok {
			continue
		}
		res := maps.Clone(result)
		meta, _ := res["_meta"].(map[string]any)
		meta = maps.Clone(meta)
		if version >= "2026-07-28" {
			assert.Equal(t, "complete", res["resultType"], ex.Name)
			assert.Contains(t, meta, "io.modelcontextprotocol/serverInfo", ex.Name)
		}
		delete(res, "resultType")
		delete(meta, "io.modelcontextprotocol/serverInfo")
		delete(res, "_meta")
		if len(meta) > 0 {
			res["_meta"] = meta
		}
		out[ex.Name] = res
	}
	return out
}

// Versions /mcp does not negotiate are refused on the transport, and so are
// requests without a version (which the SDK would serve as 2025-03-26) and
// batches (which no negotiated version allows).
func TestMCPRefusesOtherProtocolVersionsAndBatches(t *testing.T) {
	app := newMCPApp(t, true)
	garage := app.deployTool(t, "garage", nil)
	_, raw := app.token(t, "laptop", []string{storage.ScopeCall}, garage)
	resp, body := app.post(t, raw, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, map[string]string{"Mcp-Protocol-Version": "2025-03-26"})
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, string(body))
	resp, body = app.post(t, raw, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, string(body))
	assert.JSONEq(t, `{"jsonrpc":"2.0","id":2,"error":{"code":-32600,
	  "message":"The Mcp-Protocol-Version header is required on every request but initialize (supported versions: 2026-07-28, 2025-11-25, 2025-06-18)"}}`, string(body))
	call := `{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"garage","arguments":{"door":"left"}}}`
	batch := "[" + fmt.Sprintf(call, 1) + "," + fmt.Sprintf(call, 2) + "]"
	resp, body = app.post(t, raw, batch, nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, string(body))
	assert.JSONEq(t, `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,
	  "message":"JSON-RPC batching is not supported: send one message per request"}}`, string(body))
}

// Arguments that are not a JSON object are refused by /mcp itself and
// counted in mcp_rejections like the tool service's own refusals.
func TestMCPNonObjectArgumentsAreCounted(t *testing.T) {
	app := newMCPApp(t, true)
	garage := app.deployTool(t, "garage", nil)
	tok, raw := app.token(t, "laptop", []string{storage.ScopeCall}, garage)
	resp, body := app.post(t, raw, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"garage","arguments":[1]}}`, at0618)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	assert.Contains(t, string(body), `\"code\":\"invalid_arguments\"`, "the envelope, in the text block")
	assert.Contains(t, string(body), `"isError":true`)
	assert.NotContains(t, string(body), `"structuredContent"`, "an isError result carries no structured content")
	assert.Eventually(t, func() bool {
		rows, err := app.store.ListMCPRejections(storage.MCPAuditFilter{})
		require.NoError(t, err)
		return len(rows) == 1 && rows[0].TokenID == tok.ID && rows[0].Reason == "invalid_arguments"
	}, 10*time.Second, 50*time.Millisecond)
}

// With TLS on, the plain-HTTP listener refuses /mcp instead of redirecting
// it, so an agent configured with http:// fails at once rather than
// sending its token in clear text on every request.
func TestPlainHTTPListenerRefusesMCP(t *testing.T) {
	served := false
	app := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served = true })
	h := redirectHandler("8443", app, true)
	for _, target := range []string{"/mcp", "/mcp/", "/mcp?x=1", "/./mcp"} {
		r := httptest.NewRequest(http.MethodPost, "http://piworker.local:8080"+target, strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer pwk_aaaaaaaaaaaaaaaa_"+strings.Repeat("b", 52))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code, target)
		assert.Empty(t, w.Header().Get("Location"), target)
		assert.Contains(t, w.Body.String(), "https://piworker.local:8443/mcp", target)
	}
	assert.False(t, served)

	// Everything else is still redirected.
	r := httptest.NewRequest(http.MethodGet, "http://piworker.local:8080/mcpx", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, http.StatusPermanentRedirect, w.Code)
}

// readTools are the built-in tools of the read scope.
var readTools = []string{"pw_get_flow", "pw_get_run", "pw_list_flows", "pw_list_runs", "pw_system_status"}

func toolNames(t *testing.T, s *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	list, err := s.ListTools(context.Background(), nil)
	require.NoError(t, err)
	out := map[string]*mcp.Tool{}
	for _, tool := range list.Tools {
		out[tool.Name] = tool
	}
	return out
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// checkOutputSchema validates a successful result's structured content
// against the tool's advertised output schema, as clients do.
func checkOutputSchema(t *testing.T, tool *mcp.Tool, res *mcp.CallToolResult) {
	t.Helper()
	require.NotNil(t, tool.OutputSchema, tool.Name)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(mustJSON(t, tool.OutputSchema), &schema))
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	data := mustJSON(t, res.StructuredContent)
	var v any
	require.NoError(t, json.Unmarshal(data, &v))
	assert.NoError(t, resolved.Validate(v), "%s: %s", tool.Name, data)
	require.Len(t, res.Content, 1)
	assert.JSONEq(t, string(data), res.Content[0].(*mcp.TextContent).Text, "%s: the text block is the structured content", tool.Name)
}

// Tokens with the read scope list and call the read tools (and their
// granted tools when they also have the call scope); tokens without it see
// none of them, and calling one is answered as calling a name that does
// not exist.
func TestMCPReadToolsEndToEnd(t *testing.T) {
	app := newMCPApp(t, true, recordRuns)
	garage := app.deployTool(t, "garage", nil)
	_, readRaw := app.token(t, "reader", []string{storage.ScopeRead})
	_, bothRaw := app.token(t, "laptop", []string{storage.ScopeCall, storage.ScopeRead}, garage)
	_, callRaw := app.token(t, "caller", []string{storage.ScopeCall}, garage)
	ctx := context.Background()

	both, err := app.connect(t, bothRaw, "")
	require.NoError(t, err)
	assert.ElementsMatch(t, append([]string{"garage", "pw_get_result"}, readTools...), keys(toolNames(t, both)))
	res := callTool(t, both, "garage", map[string]any{"door": "left"})
	require.False(t, res.IsError)
	runID, _ := res.Meta["piworker/runId"].(string)
	require.NotEmpty(t, runID)

	reader, err := app.connect(t, readRaw, "")
	require.NoError(t, err)
	tools := toolNames(t, reader)
	assert.ElementsMatch(t, readTools, keys(tools), "the read tools only: no flow tool, no pw_get_result")
	for _, name := range readTools {
		tool := tools[name]
		require.NotNil(t, tool.Annotations, name)
		assert.True(t, tool.Annotations.ReadOnlyHint, name)
		assert.True(t, tool.Annotations.IdempotentHint, name)
		require.NotNil(t, tool.Annotations.DestructiveHint, name)
		assert.False(t, *tool.Annotations.DestructiveHint, name)
		require.NotNil(t, tool.Annotations.OpenWorldHint, name)
		assert.False(t, *tool.Annotations.OpenWorldHint, name)
		assert.Equal(t, false, tool.InputSchema.(map[string]any)["additionalProperties"], name)
	}

	flows := callTool(t, reader, "pw_list_flows", map[string]any{})
	require.False(t, flows.IsError)
	checkOutputSchema(t, tools["pw_list_flows"], flows)
	listed := structured(t, flows)["flows"].([]any)
	require.Len(t, listed, 1)
	assert.Equal(t, garage.FlowID, listed[0].(map[string]any)["id"])
	assert.Equal(t, "running", listed[0].(map[string]any)["state"])
	assert.Equal(t, []any{"garage"}, listed[0].(map[string]any)["tools"])

	view := callTool(t, reader, "pw_get_flow", map[string]any{"flowId": garage.FlowID})
	require.False(t, view.IsError)
	checkOutputSchema(t, tools["pw_get_flow"], view)
	got := structured(t, view)
	assert.Len(t, got["nodes"], 2)
	assert.Len(t, got["connections"], 1)
	require.Len(t, got["tools"], 1)
	assert.Equal(t, garage.Fingerprint, got["tools"].([]any)[0].(map[string]any)["fingerprint"])

	// The agent's call was recorded as its run, with its token's name.
	// Wait for the recorder in the store (polling over MCP would spend the
	// token's request budget), then read it once.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		run, err := app.store.GetFlowRun(garage.FlowID, runID)
		if assert.NoError(c, err) {
			assert.Equal(c, "success", run.Status)
		}
	}, 10*time.Second, 50*time.Millisecond)
	runs := callTool(t, reader, "pw_list_runs", map[string]any{"flowId": garage.FlowID, "limit": 5})
	require.False(t, runs.IsError)
	require.Len(t, structured(t, runs)["runs"], 1)
	checkOutputSchema(t, tools["pw_list_runs"], runs)
	run := structured(t, runs)["runs"].([]any)[0].(map[string]any)
	assert.Equal(t, runID, run["id"])
	assert.Equal(t, "agent", run["actor"])
	assert.Equal(t, "laptop", run["actorToken"])

	// (Another token: each token may send 10 requests at once.)
	_, readRaw2 := app.token(t, "reader2", []string{storage.ScopeRead})
	reader2, err := app.connect(t, readRaw2, "")
	require.NoError(t, err)
	one := callTool(t, reader2, "pw_get_run", map[string]any{"flowId": garage.FlowID, "runId": runID})
	require.False(t, one.IsError)
	checkOutputSchema(t, tools["pw_get_run"], one)
	events := structured(t, one)["events"].([]any)
	require.Len(t, events, 1, "the reply (triggers record no execution)")
	assert.Equal(t, "reply", events[0].(map[string]any)["nodeId"])

	status := callTool(t, reader2, "pw_system_status", nil)
	require.False(t, status.IsError)
	checkOutputSchema(t, tools["pw_system_status"], status)
	st := structured(t, status)
	assert.Equal(t, map[string]any{"running": 1.0, "total": 1.0}, st["flows"])
	assert.Equal(t, 1.0, st["tools"])
	assert.Equal(t, "off", st["agentDeploy"])

	// Refusals are isError results with the service's codes.
	missing := callTool(t, reader2, "pw_get_flow", map[string]any{"flowId": "no-such-flow"})
	assert.True(t, missing.IsError)
	assert.Equal(t, "flow_not_found", errCode(structured(t, missing)))
	invalid := callTool(t, reader2, "pw_get_flow", map[string]any{"flowId": garage.FlowID + "\n"})
	assert.True(t, invalid.IsError)
	assert.Equal(t, "invalid_arguments", errCode(structured(t, invalid)))
	foreign := callTool(t, reader2, "pw_get_run", map[string]any{"flowId": "no-such-flow", "runId": runID})
	assert.True(t, foreign.IsError)
	assert.Equal(t, "flow_not_found", errCode(structured(t, foreign)))

	// A token without the read scope sees no read tool and no read
	// prompt; calling one is answered exactly as an unknown name.
	caller, err := app.connect(t, callRaw, "")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"garage", "pw_get_result"}, keys(toolNames(t, caller)))
	prompts, err := caller.ListPrompts(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, prompts.Prompts)
	hidden := callTool(t, caller, "pw_get_flow", map[string]any{"flowId": garage.FlowID})
	unknown := callTool(t, caller, "pw_get_flows", map[string]any{"flowId": garage.FlowID})
	assert.True(t, hidden.IsError)
	assert.Equal(t, "not_found", errCode(structured(t, hidden)))
	assert.Equal(t, strings.ReplaceAll(string(mustJSON(t, hidden)), "pw_get_flow", "X"), strings.ReplaceAll(string(mustJSON(t, unknown)), "pw_get_flows", "X"))
	_, hiddenErr := caller.GetPrompt(ctx, &mcp.GetPromptParams{Name: "diagnose_flow", Arguments: map[string]string{"flowId": garage.FlowID}})
	_, unknownErr := caller.GetPrompt(ctx, &mcp.GetPromptParams{Name: "diagnose_flows", Arguments: map[string]string{"flowId": garage.FlowID}})
	require.Error(t, hiddenErr)
	require.Error(t, unknownErr)
	assert.Equal(t, strings.ReplaceAll(hiddenErr.Error(), "diagnose_flow", "X"), strings.ReplaceAll(unknownErr.Error(), "diagnose_flows", "X"))
}

// Reads through /mcp are audited with the token, the read, the flow it
// named and the outcome; refused arguments are counted.
func TestMCPReadsAreAudited(t *testing.T) {
	app := newMCPApp(t, true)
	garage := app.deployTool(t, "garage", nil)
	tok, raw := app.token(t, "reader", []string{storage.ScopeRead})
	session, err := app.connect(t, raw, "")
	require.NoError(t, err)
	require.False(t, callTool(t, session, "pw_get_flow", map[string]any{"flowId": garage.FlowID}).IsError)
	require.True(t, callTool(t, session, "pw_get_run", map[string]any{"flowId": garage.FlowID, "runId": "no-such-run"}).IsError)
	require.True(t, callTool(t, session, "pw_get_flow", map[string]any{"flowId": garage.FlowID, "extra": 1}).IsError)
	_, err = session.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "diagnose_flow", Arguments: map[string]string{"flowId": garage.FlowID}})
	require.NoError(t, err)

	type row struct{ tool, flowID, runID, outcome string }
	want := []row{
		{"prompt:diagnose_flow", garage.FlowID, "", "completed"},
		{"pw_get_run", garage.FlowID, "no-such-run", "run_not_found"},
		{"pw_get_flow", garage.FlowID, "", "completed"},
	}
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		rows, err := app.store.ListMCPAudit(storage.MCPAuditFilter{TokenID: tok.ID})
		if !assert.NoError(c, err) {
			return
		}
		var got []row
		for _, r := range rows {
			got = append(got, row{r.Tool, r.FlowID, r.RunID, r.Outcome})
		}
		assert.Equal(c, want, got)
		rejections, err := app.store.ListMCPRejections(storage.MCPAuditFilter{TokenID: tok.ID})
		if assert.NoError(c, err) && assert.Len(c, rejections, 1) {
			assert.Equal(c, "invalid_arguments", rejections[0].Reason)
		}
	}, 10*time.Second, 100*time.Millisecond)
}

// The answer of pw_get_flow for a flow at every bound (nodes, connections,
// id lengths, config values) stays small enough for clients that cap tool
// output: the view's JSON, its text block, is at most 64 KiB, and the
// whole answer (the view twice, escaped) a few times that.
func TestMCPReadResultsAreBounded(t *testing.T) {
	app := newMCPApp(t, true)
	f := flow.NewFlow(strings.Repeat("N", 300))
	f.ID = strings.Repeat("F", 256)
	for i := range 220 {
		n := flow.NewNode("process-debug", flow.NodeCategoryProcessing)
		n.ID = fmt.Sprintf("%04d%s", i, strings.Repeat("i", 252))
		n.Config = map[string]any{"name": strings.Repeat("<v>", 3000)}
		f.AddNode(*n)
	}
	for i := range 450 {
		c := flow.NewConnection(f.Nodes[i%200].ID, strings.Repeat("o", 256), f.Nodes[(i+1)%200].ID, strings.Repeat("p", 256))
		c.ID = fmt.Sprintf("%04d%s", i, strings.Repeat("c", 252))
		f.AddConnection(*c)
	}
	require.NoError(t, app.store.CreateFlow(f))
	_, raw := app.token(t, "reader", []string{storage.ScopeRead})
	session, err := app.connect(t, raw, "")
	require.NoError(t, err)

	res := callTool(t, session, "pw_get_flow", map[string]any{"flowId": f.ID})
	require.False(t, res.IsError)
	text := res.Content[0].(*mcp.TextContent).Text
	assert.LessOrEqual(t, len(text), 64<<10, "the view's JSON")
	assert.Equal(t, true, structured(t, res)["truncated"])

	resp, body := app.post(t, raw, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"pw_get_flow","arguments":{"flowId":"`+f.ID+`"}}}`, at0618)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.LessOrEqual(t, len(body), 3*(64<<10), "the whole answer")
	for _, tool := range []string{"pw_list_flows", "pw_list_runs"} {
		args := map[string]any{"flowId": f.ID}
		if tool == "pw_list_flows" {
			args = nil
		}
		res := callTool(t, session, tool, args)
		require.False(t, res.IsError, tool)
		assert.LessOrEqual(t, len(res.Content[0].(*mcp.TextContent).Text), 64<<10, tool)
	}
}

// A flow saved by an older version with a client-chosen id (spaces, '/',
// non-ASCII) is listed and can be read back with the id the list shows.
func TestMCPReadsLegacyFlowIDs(t *testing.T) {
	app := newMCPApp(t, true)
	f := flow.NewFlow("Legacy")
	f.ID = "my flow/é"
	n := flow.NewNode("trigger-manual", flow.NodeCategoryInput)
	n.ID = "trigger"
	f.AddNode(*n)
	require.NoError(t, app.store.CreateFlow(f))
	_, raw := app.token(t, "reader", []string{storage.ScopeRead})
	session, err := app.connect(t, raw, "")
	require.NoError(t, err)

	listed := structured(t, callTool(t, session, "pw_list_flows", nil))["flows"].([]any)
	require.Len(t, listed, 1)
	id := listed[0].(map[string]any)["id"].(string)
	assert.Equal(t, f.ID, id)
	view := callTool(t, session, "pw_get_flow", map[string]any{"flowId": id})
	require.False(t, view.IsError, "%v", view.Content)
	assert.Equal(t, "Legacy", structured(t, view)["name"])
	runs := callTool(t, session, "pw_list_runs", map[string]any{"flowId": id})
	require.False(t, runs.IsError, "%v", runs.Content)
	prompt, err := session.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "diagnose_flow", Arguments: map[string]string{"flowId": id}})
	require.NoError(t, err)
	assert.Contains(t, prompt.Messages[0].Content.(*mcp.TextContent).Text, `"my flow/\u00e9"`)
}

// diagnose_flow returns instructions that use the read tools, for a flow
// the token may read.
func TestMCPDiagnoseFlowPrompt(t *testing.T) {
	app := newMCPApp(t, true)
	garage := app.deployTool(t, "garage", nil)
	_, raw := app.token(t, "reader", []string{storage.ScopeRead})
	session, err := app.connect(t, raw, "")
	require.NoError(t, err)
	ctx := context.Background()

	prompts, err := session.ListPrompts(ctx, nil)
	require.NoError(t, err)
	require.Len(t, prompts.Prompts, 1)
	assert.Equal(t, "diagnose_flow", prompts.Prompts[0].Name)
	require.Len(t, prompts.Prompts[0].Arguments, 1)
	assert.Equal(t, "flowId", prompts.Prompts[0].Arguments[0].Name)
	assert.True(t, prompts.Prompts[0].Arguments[0].Required)

	res, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "diagnose_flow", Arguments: map[string]string{"flowId": garage.FlowID}})
	require.NoError(t, err)
	require.Len(t, res.Messages, 1)
	assert.Equal(t, mcp.Role("user"), res.Messages[0].Role)
	text := res.Messages[0].Content.(*mcp.TextContent).Text
	for _, want := range []string{garage.FlowID, `"Garage"`, "pw_get_flow", "pw_list_runs", "pw_get_run", "pw_system_status"} {
		assert.Contains(t, text, want)
	}

	_, err = session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "diagnose_flow", Arguments: map[string]string{"flowId": "no-such-flow"}})
	assert.ErrorContains(t, err, "unknown flow")
	_, err = session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "diagnose_flow", Arguments: map[string]string{"flowId": "a\nb"}})
	assert.Error(t, err)
}

// The read canary through MCP: credentials and settings hidden from agents
// in the riskiest node types, and a stored secret leaked into a flow's
// name, a visible setting, a run's token name and node errors (literally
// and encoded), never appear in the raw JSON-RPC answers of the read tools
// and the prompt.
func TestMCPReadCanaries(t *testing.T) {
	app := newMCPApp(t, true)
	const secretName = "MCP_READ_CANARY"
	const secret = "Zq7SecretCanary/v+1&key=<x>"
	require.NoError(t, secrets.DefaultStore.Set(secretName, secret))
	t.Cleanup(func() { _ = secrets.DefaultStore.Delete(secretName) })

	canaries := map[string]string{} // where it was planted → canary
	plant := func(where string) string {
		c := fmt.Sprintf("CNRY%02dZ", len(canaries))
		canaries[where] = c
		return c
	}
	f := flow.NewFlow("Canary " + secret)
	add := func(id, typ string, cfg map[string]any) {
		n := flow.NewNode(typ, flow.NodeCategoryOutput)
		n.ID, n.Config = id, cfg
		f.AddNode(*n)
	}
	add("http", "action-http", map[string]any{
		"method": secret, // a visible setting holding the secret's value
		"url":    "https://" + plant("http url user") + ":" + plant("http url password") + "@api.example.com/" + plant("http url path") + "?key=" + plant("http url query"),
		"headers": map[string]any{
			"Authorization": "Bearer " + plant("http header"),
			"X-Api-Key":     "{{secret." + secretName + "}}",
		},
		"body": plant("http body"),
	})
	add("cmd", "action-command", map[string]any{"command": "curl -H 'X-Token: " + plant("command") + "' https://example.com"})
	add("mqtt", "action-mqtt", map[string]any{
		"broker": "tcp://" + plant("mqtt broker") + ":1883", "username": plant("mqtt username"),
		"password": plant("mqtt password"), "topic": plant("mqtt topic"), "payload": plant("mqtt payload"),
	})
	add("hook", "trigger-webhook", map[string]any{"path": "/" + plant("webhook path"), "token": plant("webhook token")})
	add("mail", "action-email", map[string]any{
		"host": plant("email host"), "username": plant("email username"), "password": plant("email password"),
		"from": plant("email from") + "@example.com", "to": plant("email to") + "@example.com", "subject": plant("email subject"),
	})
	add("tg", "action-telegram", map[string]any{"botToken": "123456:" + plant("telegram bot token"), "chatId": plant("telegram chat"), "message": plant("telegram message")})
	add("db", "action-database", map[string]any{"dsn": "postgres://u:" + plant("database dsn") + "@db/x"})
	// A webhook URL whose token sits in its path: the real node's error
	// names the URL, path included (the node redacts only the query).
	hook := map[string]any{"method": "POST", "url": "http://127.0.0.1:1/services/T000/B000/" + plant("webhook url path") + "?wait=true"}
	add("hook2", "action-http", hook)
	realErr := realNodeError(t, "action-http", hook)
	require.Contains(t, realErr, canaries["webhook url path"])
	require.NoError(t, app.store.CreateFlow(f))

	// A recorded run: its errors echo credentials typed into Sensitive
	// settings and the secret, literally and encoded.
	now := time.Now()
	require.NoError(t, app.store.UpsertFlowRunWithActor("run-1", f.ID, "token:bot "+secret, now))
	var records []storage.NodeEventRecord
	for i, msg := range []string{
		"401 Unauthorized for Bearer " + canaries["http header"],
		"mqtt: not authorized (password " + canaries["mqtt password"] + ")",
		"telegram: 401 for bot123456:" + canaries["telegram bot token"],
		"smtp: 535 auth failed for " + canaries["email password"],
		"webhook: bad token " + canaries["webhook token"],
		"echoed secret " + secret,
		"echoed query key=" + url.QueryEscape(secret),
		"echoed basic " + base64.StdEncoding.EncodeToString([]byte(secret)),
		`echoed payload {"apiKey":` + string(mustJSON(t, secret)) + `}`,
		realErr,
	} {
		records = append(records, storage.NodeEventRecord{RunID: "run-1", FlowID: f.ID, NodeID: "http", NodeType: "action-http",
			Phase: "error", Error: msg, CreatedAt: now.Add(time.Duration(i) * time.Millisecond)})
	}
	require.NoError(t, app.store.InsertNodeEvents(records))
	require.NoError(t, app.store.FinishFlowRun("run-1", "error", now.Add(time.Second), len(records), len(records)))

	_, raw := app.token(t, "reader", []string{storage.ScopeRead})
	call := func(name, args string) string {
		return `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`
	}
	flowArg := `{"flowId":"` + f.ID + `"}`
	requests := map[string]string{
		"pw_list_flows": call("pw_list_flows", `{}`),
		"pw_get_flow":   call("pw_get_flow", flowArg),
		"pw_list_runs":  call("pw_list_runs", flowArg),
		"pw_get_run":    call("pw_get_run", `{"flowId":"`+f.ID+`","runId":"run-1"}`),
		"diagnose_flow": `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"diagnose_flow","arguments":` + flowArg + `}}`,
	}
	secretForms := map[string]string{
		"its distinctive part": "Zq7SecretCanary",
		"base64":               base64.StdEncoding.EncodeToString([]byte(secret)),
		"base64url":            base64.URLEncoding.EncodeToString([]byte(secret)),
		"url-encoded":          url.QueryEscape(secret),
	}
	answers := map[string]string{}
	for name, body := range requests {
		resp, data := app.post(t, raw, body, at0618)
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", name, data)
		text := string(data)
		answers[name] = text
		require.NotContains(t, text, `"error":{`, "%s answered an error: %s", name, text)
		require.NotContains(t, text, `"isError":true`, "%s answered an error: %s", name, text)
		for where, c := range canaries {
			assert.NotContains(t, text, c, "%s leaked the %s", name, where)
		}
		for form, s := range secretForms {
			assert.NotContains(t, text, s, "%s leaked the secret (%s)", name, form)
		}
	}

	// The answers are real views: every node is shown with its hidden
	// values as the sentinel ("<redacted>", HTML-escaped in the JSON), and
	// the run with its redacted errors.
	const sentinel = `\u003credacted\u003e`
	for _, id := range []string{"http", "cmd", "mqtt", "hook", "mail", "tg", "db"} {
		assert.Contains(t, answers["pw_get_flow"], `{"id":"`+id+`"`)
	}
	assert.Contains(t, answers["pw_get_flow"], `"password":"`+sentinel+`"`)
	assert.Contains(t, answers["pw_get_flow"], `"name":"Canary `+sentinel+`"`)
	assert.Contains(t, answers["pw_get_run"], `"error":"401 Unauthorized for `+sentinel+`"`)
	assert.Contains(t, answers["pw_get_run"], `"error":"echoed secret `+sentinel+`"`)
	assert.Contains(t, answers["pw_get_run"], `"actorToken":"bot `+sentinel+`"`)
	assert.Contains(t, answers["pw_list_flows"], f.ID)
	assert.Contains(t, answers["diagnose_flow"], f.ID)
}

// realNodeError runs a real node of nodeType with cfg once and returns its
// error, the text the run recorder stores.
func realNodeError(t *testing.T, nodeType string, cfg map[string]any) string {
	t.Helper()
	n, err := node.DefaultRegistry.Create(nodeType, cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = n.Process(ctx, types.NewMessage(map[string]any{"x": 1}, types.DataTypeObject))
	require.Error(t, err)
	return err.Error()
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}

// ---------------------------------------------------------------------------
// Agent authoring and agent deploy (wave 4) through the real router: the
// service, the MCP adapter and the editor REST joined (docs/design/mcp.md
// §10).

var (
	authorToolNames = []string{"pw_dry_run", "pw_get_draft", "pw_list_node_types", "pw_save_draft", "pw_test_node", "pw_validate_flow"}
	deployToolNames = []string{"pw_deploy_flow", "pw_stop_flow"}
)

// newAuthorApp is the real router with /mcp, mcp.author_enabled as given and
// the agent deploy level set.
func newAuthorApp(t *testing.T, authorEnabled bool, level storage.AgentDeployLevel) mcpApp {
	t.Helper()
	app := newMCPAppWith(t, func(cfg *config.Config) { cfg.MCP.AuthorEnabled = authorEnabled })
	require.NoError(t, app.store.SetAgentDeployLevel(level))
	return app
}

// agentSession is an agent token with an open MCP session.
type agentSession struct {
	tok     *storage.APIToken
	session *mcp.ClientSession
}

// agent creates a token with scopes and connects with it.
func (a mcpApp) agent(t *testing.T, name string, scopes ...string) agentSession {
	t.Helper()
	tok, raw := a.token(t, name, scopes)
	session, err := a.connect(t, raw, "")
	require.NoError(t, err)
	return agentSession{tok: tok, session: session}
}

// call calls a tool and returns what it says and whether it is an error.
func (s agentSession) call(t *testing.T, name string, args any) (map[string]any, bool) {
	t.Helper()
	res := callTool(t, s.session, name, args)
	return structured(t, res), res.IsError
}

// callOK calls a tool that must succeed and decodes its result into T.
func callOK[T any](t *testing.T, s agentSession, name string, args any) T {
	t.Helper()
	res := callTool(t, s.session, name, args)
	require.False(t, res.IsError, "%s: %s", name, resultText(res))
	var out T
	require.NoError(t, json.Unmarshal(mustJSON(t, res.StructuredContent), &out))
	return out
}

// refused calls a tool that must be refused with code and returns the
// error envelope.
func refused(t *testing.T, s agentSession, name string, args any, code string) map[string]any {
	t.Helper()
	m, isErr := s.call(t, name, args)
	require.True(t, isErr, "%s must be refused: %v", name, m)
	require.Equal(t, code, errCode(m), "%v", m)
	return m["error"].(map[string]any)
}

func resultText(res *mcp.CallToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}
	if text, ok := res.Content[0].(*mcp.TextContent); ok {
		return text.Text
	}
	return ""
}

// listedAuthoring lists the author and deploy tools a session sees.
func (s agentSession) listedAuthoring(t *testing.T) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := s.session.ListTools(ctx, nil)
	require.NoError(t, err)
	var names []string
	for _, tool := range res.Tools {
		if slices.Contains(authorToolNames, tool.Name) || slices.Contains(deployToolNames, tool.Name) {
			names = append(names, tool.Name)
		}
	}
	slices.Sort(names)
	return names
}

// The author tools are served only with mcp.author_enabled, the deploy
// tools only while agent_deploy is above off, each to tokens with its scope.
func TestMCPAuthorAndDeployToolsAreGated(t *testing.T) {
	all := slices.Sorted(slices.Values(append(slices.Clone(authorToolNames), deployToolNames...)))
	// With the call scope, a hidden tool is an unknown one (not_found);
	// without it, the endpoint refuses calls of unknown names (403).
	everything := []string{storage.ScopeCall, storage.ScopeRead, storage.ScopeAuthor, storage.ScopeDeploy}
	for _, tc := range []struct {
		name   string
		author bool
		level  storage.AgentDeployLevel
		scopes []string
		want   []string
	}{
		{"author disabled, deploy off", false, storage.AgentDeployOff, everything, nil},
		{"author enabled, deploy off", true, storage.AgentDeployOff, everything, authorToolNames},
		{"author disabled, deploy low", false, storage.AgentDeployLow, everything, deployToolNames},
		{"author enabled, deploy all", true, storage.AgentDeployAll, everything, all},
		{"no deploy scope", true, storage.AgentDeployAll, []string{storage.ScopeCall, storage.ScopeRead, storage.ScopeAuthor}, authorToolNames},
		{"no author scope", true, storage.AgentDeployAll, []string{storage.ScopeCall, storage.ScopeRead, storage.ScopeDeploy}, deployToolNames},
		{"call and read scopes", true, storage.AgentDeployAll, []string{storage.ScopeCall, storage.ScopeRead}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newAuthorApp(t, tc.author, tc.level)
			agent := app.agent(t, "agent", tc.scopes...)
			assert.Equal(t, tc.want, agent.listedAuthoring(t))
			for _, name := range all {
				if !slices.Contains(tc.want, name) {
					refused(t, agent, name, map[string]any{}, "not_found")
				}
			}
		})
	}
}

// The raw tools/list of an author+deploy token on the real endpoint is the
// contract the adapter's golden file pins (internal/mcpserver/testdata).
func TestMCPAuthorDeployToolsListGolden(t *testing.T) {
	app := newAuthorApp(t, true, storage.AgentDeployAll)
	_, raw := app.token(t, "agent", []string{storage.ScopeAuthor, storage.ScopeDeploy})
	resp, body := app.post(t, raw, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25",`+
		`"capabilities":{},"clientInfo":{"name":"golden","version":"1"}}}`, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	resp, body = app.post(t, raw, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, map[string]string{"Mcp-Protocol-Version": "2025-11-25"})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var msg struct {
		Result struct {
			Tools json.RawMessage `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &msg), string(body))
	// The golden file is encoded as the adapter's test encodes it: keys
	// sorted, indented.
	var tools any
	require.NoError(t, json.Unmarshal(msg.Result.Tools, &tools))
	var pretty bytes.Buffer
	require.NoError(t, json.Indent(&pretty, mustJSON(t, tools), "", "  "))
	pretty.WriteByte('\n')
	want, err := os.ReadFile(filepath.Join("internal", "mcpserver", "testdata", "tools_author_deploy.golden.json"))
	require.NoError(t, err)
	assert.Equal(t, string(want), pretty.String())
}

// Flows as agents send them (pw_validate_flow, pw_save_draft, pw_dry_run).

func agentNode(id, typ string, cfg map[string]any) map[string]any {
	n := map[string]any{"id": id, "type": typ}
	if cfg != nil {
		n["config"] = cfg
	}
	return n
}

// agentChain is a flow whose nodes are connected in order, output → input.
func agentChain(name string, nodes ...map[string]any) map[string]any {
	conns := []any{}
	for i := 1; i < len(nodes); i++ {
		from, to := nodes[i-1]["id"].(string), nodes[i]["id"].(string)
		conns = append(conns, map[string]any{"id": from + "-" + to, "sourceNode": from, "sourcePort": "output", "targetNode": to, "targetPort": "input"})
	}
	list := make([]any, len(nodes))
	for i, n := range nodes {
		list[i] = n
	}
	f := map[string]any{"nodes": list, "connections": conns}
	if name != "" {
		f["name"] = name
	}
	return f
}

// adminChain stores a flow the admin wrote (version 1, admin-approved):
// nodes connected in order, output → input.
func (a mcpApp) adminChain(t *testing.T, name string, nodes ...*flow.Node) *flow.Flow {
	t.Helper()
	f := flow.NewFlow(name)
	for i, n := range nodes {
		f.AddNode(*n)
		if i > 0 {
			f.AddConnection(*flow.NewConnection(nodes[i-1].ID, "output", n.ID, "input"))
		}
	}
	require.NoError(t, a.store.CreateFlow(f))
	return f
}

func newNode(id, typ string, category flow.NodeCategory, cfg map[string]any) *flow.Node {
	n := flow.NewNode(typ, category)
	n.ID, n.Config = id, cfg
	return n
}

func manualNode(id string) *flow.Node {
	return newNode(id, "trigger-manual", flow.NodeCategoryInput, nil)
}

func transformNode(id, expr string) *flow.Node {
	return newNode(id, "process-transform", flow.NodeCategoryProcessing, map[string]any{"expression": expr})
}

func httpNode(id string, cfg map[string]any) *flow.Node {
	return newNode(id, "action-http", flow.NodeCategoryOutput, cfg)
}

// editorSession is the admin's cookie session on the app's REST API.
type editorSession struct {
	app     mcpApp
	headers map[string]string
}

func (a mcpApp) editor(t *testing.T) editorSession {
	t.Helper()
	return editorSession{app: a, headers: a.login(t)}
}

// do sends a REST request (body encoded as JSON unless nil) and returns
// the status and the body.
func (e editorSession) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var raw, contentType string
	if body != nil {
		raw, contentType = string(mustJSON(t, body)), "application/json"
	}
	w := e.app.do(method, path, contentType, raw, e.headers)
	return w.Code, w.Body.Bytes()
}

// get decodes the data of a successful GET.
func editorGet[T any](t *testing.T, e editorSession, path string) T {
	t.Helper()
	code, body := e.do(t, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, code, string(body))
	return appData[T](t, body)
}

// flowMarks is what the editor's flow list and view show of a flow.
type flowMarks struct {
	ID           string                  `json:"id"`
	Version      int                     `json:"version"`
	Deployment   *service.DeploymentView `json:"deployment"`
	AgentChanged bool                    `json:"agentChanged"`
}

func (e editorSession) flow(t *testing.T, id string) flowMarks {
	t.Helper()
	return editorGet[struct {
		Flow flowMarks `json:"flow"`
	}](t, e, "/api/flows/"+id).Flow
}

func (e editorSession) drafts(t *testing.T, status string) []service.DraftSummary {
	t.Helper()
	return editorGet[service.DraftList](t, e, "/api/agent/drafts?status="+status).Drafts
}

func issueCodes(issues []flow.Issue) []string {
	var codes []string
	for _, is := range issues {
		codes = append(codes, is.Code)
	}
	return codes
}

func alertKinds(alerts []flow.Alert) []string {
	var kinds []string
	for _, al := range alerts {
		kinds = append(kinds, al.Kind)
	}
	return kinds
}

// flowIssueCodes are the codes of a refusal's error.flowIssues.
func flowIssueCodes(t *testing.T, e map[string]any) []string {
	t.Helper()
	var issues []flow.Issue
	require.NoError(t, json.Unmarshal(mustJSON(t, e["flowIssues"]), &issues))
	return issueCodes(issues)
}

// pw_list_node_types lists the registry with the schema markers agents
// author by.
func TestMCPListNodeTypes(t *testing.T) {
	app := newAuthorApp(t, true, storage.AgentDeployOff)
	agent := app.agent(t, "agent", storage.ScopeAuthor)

	list := callOK[mcpserver.NodeTypeList](t, agent, "pw_list_node_types", map[string]any{})
	require.NotEmpty(t, list.Types)
	assert.Equal(t, len(node.DefaultRegistry.List()), list.Total)
	assert.Equal(t, "input", list.Types[0].Category, "triggers first")
	byType := map[string]mcpserver.NodeTypeView{}
	for _, nt := range list.Types {
		byType[nt.Type] = nt
	}
	require.Contains(t, byType, "trigger-tool", "listed for reference")
	http := byType["action-http"]
	assert.Equal(t, "output", http.Category)
	assert.Equal(t, node.RiskExternal, http.Risk)
	props := http.Config.Properties
	assert.True(t, props["url"].Destination)
	assert.True(t, props["headers"].Sensitive)
	assert.True(t, props["method"].Operation)
	assert.True(t, props["method"].AgentVisible)
	assert.True(t, props["allowPrivate"].RiskSwitch)
	assert.True(t, byType["trigger-interval"].Config.Properties["interval"].Rate)

	one := callOK[mcpserver.NodeTypeList](t, agent, "pw_list_node_types", map[string]any{"type": "action-http"})
	require.Len(t, one.Types, 1)
	assert.NotEmpty(t, one.Types[0].Documentation)
	refused(t, agent, "pw_list_node_types", map[string]any{"type": "action-nope"}, "invalid_arguments")
}

// pw_validate_flow reports every problem deploy would refuse, with the
// flow's risk; nothing is stored.
func TestMCPValidateFlow(t *testing.T) {
	app := newAuthorApp(t, true, storage.AgentDeployOff)
	agent := app.agent(t, "agent", storage.ScopeAuthor)
	start := agentNode("start", "trigger-manual", nil)
	calc := agentNode("calc", "process-transform", map[string]any{"expression": "payload"})
	for _, tc := range []struct {
		name  string
		flow  map[string]any
		valid bool
		risk  node.Risk
		codes []string
	}{
		{"valid", agentChain("", start, calc), true, node.RiskSafe, nil},
		{"external", agentChain("", start, agentNode("http", "action-http", map[string]any{"url": "https://api.example.com/x"})),
			true, node.RiskExternal, nil},
		// Agents send nodes without ports: a disabled node's come from its
		// type too, so its connections are valid.
		{"disabled node without ports", agentChain("", start, map[string]any{"id": "calc", "type": "process-transform",
			"enabled": false, "config": map[string]any{"expression": "payload"}}, agentNode("log", "action-log", nil)),
			true, node.RiskSafe, nil},
		// A type the registry does not know counts as privileged.
		{"unknown type", agentChain("", start, agentNode("calc", "process-nope", nil)), false, node.RiskPrivileged,
			[]string{flow.IssueUnknownType}},
		{"duplicate node", agentChain("", start, calc, agentNode("calc", "process-transform", map[string]any{"expression": "1"})),
			false, node.RiskSafe, []string{flow.IssueDuplicateNodeID}},
		{"config typing", agentChain("", start, agentNode("calc", "process-transform", map[string]any{"expression": 42})),
			false, node.RiskSafe, []string{flow.IssueConfigType}},
		{"sensitive literal", agentChain("", start, agentNode("http", "action-http", map[string]any{
			"url": "https://api.example.com/x", "headers": map[string]any{"Authorization": "Bearer pw-literal-5d1"}})),
			false, node.RiskExternal, []string{service.IssueSensitiveLiteral}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := callOK[service.ValidateResult](t, agent, "pw_validate_flow", map[string]any{"flow": tc.flow})
			assert.Equal(t, tc.valid, res.Valid, "%+v", res.Issues)
			assert.Equal(t, tc.risk, res.Risk)
			for _, code := range tc.codes {
				assert.Contains(t, issueCodes(res.Issues), code)
			}
			assert.NotContains(t, string(mustJSON(t, res)), "pw-literal-5d1", "a sensitive literal is never echoed")
		})
	}
	assert.Empty(t, app.editor(t).drafts(t, "all"), "validating stores nothing")
}

// An agent drafts a new flow and a change of it; the admin reviews the
// drafts over REST (with their alerts) and approves them, saving
// admin-approved versions. Drafts are private to their token.
func TestMCPDraftsReviewedAndApprovedByTheAdmin(t *testing.T) {
	app := newAuthorApp(t, true, storage.AgentDeployOff)
	agent := app.agent(t, "claude", storage.ScopeRead, storage.ScopeAuthor)
	other := app.agent(t, "other", storage.ScopeRead, storage.ScopeAuthor)
	editor := app.editor(t)

	// A new flow.
	proposal := agentChain("Weather to API",
		agentNode("start", "trigger-manual", nil),
		agentNode("calc", "process-transform", map[string]any{"expression": "payload"}),
		agentNode("http", "action-http", map[string]any{"url": "https://api.example.com/v1/report", "method": "POST"}))
	saved := callOK[service.DraftResult](t, agent, "pw_save_draft", map[string]any{"flow": proposal, "note": "Posts the weather."})
	assert.Equal(t, storage.DraftPending, saved.Status)
	assert.Equal(t, node.RiskExternal, saved.Risk)
	assert.Empty(t, saved.FlowID)
	assert.Contains(t, alertKinds(saved.Alerts), flow.AlertNewHost)

	// Only its author sees it.
	view := callOK[service.DraftView](t, agent, "pw_get_draft", map[string]any{"draftId": saved.DraftID})
	assert.Equal(t, "Posts the weather.", view.Note)
	assert.Equal(t, "Weather to API", view.Flow.Name)
	refused(t, other, "pw_get_draft", map[string]any{"draftId": saved.DraftID}, "not_found")

	// The admin's list and review page.
	assert.Equal(t, 1, editorGet[api.PendingApprovalsCount](t, editor, api.PendingApprovalsCountPath).Drafts)
	listed := editor.drafts(t, "pending")
	require.Len(t, listed, 1)
	assert.True(t, listed[0].IsNew)
	assert.Equal(t, "claude", listed[0].TokenName)
	assert.Equal(t, len(saved.Alerts), listed[0].AlertCount)
	detail := editorGet[service.DraftDetail](t, editor, "/api/agent/drafts/"+saved.DraftID)
	assert.Equal(t, node.RiskSafe, detail.Diff.RiskBefore)
	assert.Equal(t, node.RiskExternal, detail.Diff.RiskAfter)
	assert.Contains(t, alertKinds(detail.Diff.Alerts), flow.AlertDestinationChanged)
	require.NotNil(t, detail.NameChange)
	assert.Equal(t, "Weather to API", detail.NameChange.After)

	// Approving saves it as an admin-approved version 1.
	code, body := editor.do(t, http.MethodPost, "/api/agent/drafts/"+saved.DraftID+"/approve", map[string]any{"deploy": false})
	require.Equal(t, http.StatusOK, code, string(body))
	applied := appData[service.ApplyResult](t, body)
	assert.Equal(t, 1, applied.Version)
	assert.False(t, applied.Deployed)
	flowID := applied.FlowID
	versions := editorGet[service.VersionList](t, editor, "/api/flows/"+flowID+"/versions")
	assert.Equal(t, 1, versions.LastAdminApproved)
	require.Len(t, versions.Versions, 1)
	assert.Equal(t, saved.DraftID, versions.Versions[0].DraftID)
	assert.False(t, editor.flow(t, flowID).AgentChanged)
	view = callOK[service.DraftView](t, agent, "pw_get_draft", map[string]any{"draftId": saved.DraftID})
	assert.Equal(t, storage.DraftApproved, view.Status)
	assert.Equal(t, 1, view.AppliedVersion)

	// The agent reads the flow (its version) and drafts a change with ops.
	read := callOK[service.FlowView](t, agent, "pw_get_flow", map[string]any{"flowId": flowID})
	require.Equal(t, 1, read.Version)
	change := callOK[service.DraftResult](t, agent, "pw_save_draft", map[string]any{
		"flowId": flowID, "baseVersion": read.Version, "note": "Rounds the value.",
		"ops": []any{map[string]any{"op": "update_node", "nodeId": "calc", "config": map[string]any{"expression": "payload * 2"}}},
	})
	assert.Equal(t, flowID, change.FlowID)
	assert.Equal(t, 1, change.BaseVersion)
	assert.Empty(t, change.Alerts, "a processing change raises no alert")
	detail = editorGet[service.DraftDetail](t, editor, "/api/agent/drafts/"+change.DraftID)
	require.Len(t, detail.Diff.Nodes, 1)
	assert.Equal(t, "calc", detail.Diff.Nodes[0].NodeID)

	code, body = editor.do(t, http.MethodPost, "/api/agent/drafts/"+change.DraftID+"/approve", map[string]any{"deploy": false})
	require.Equal(t, http.StatusOK, code, string(body))
	assert.Equal(t, 2, appData[service.ApplyResult](t, body).Version)
	stored, err := app.store.GetFlow(flowID)
	require.NoError(t, err)
	assert.Equal(t, 2, stored.Version)
	assert.Equal(t, "payload * 2", stored.GetNode("calc").Config["expression"])
	versions = editorGet[service.VersionList](t, editor, "/api/flows/"+flowID+"/versions")
	assert.Equal(t, 2, versions.LastAdminApproved)
	assert.Empty(t, editor.drafts(t, "pending"))

	// Approving again: not pending.
	code, _ = editor.do(t, http.MethodPost, "/api/agent/drafts/"+change.DraftID+"/approve", map[string]any{"deploy": false})
	assert.Equal(t, http.StatusConflict, code)
}

// pw_save_draft refuses what the admin could not approve or the author may
// not change, saying why and never echoing hidden values; a refused draft
// is not stored.
func TestMCPSaveDraftRefusals(t *testing.T) {
	app := newAuthorApp(t, true, storage.AgentDeployOff)
	agent := app.agent(t, "claude", storage.ScopeRead, storage.ScopeAuthor)
	editor := app.editor(t)
	const literal = "pw-hidden-literal-77c"

	plain := app.adminChain(t, "Plain", manualNode("start"), transformNode("calc", "payload"),
		httpNode("http", map[string]any{"url": "https://api.example.com/x", "headers": map[string]any{"X-Key": "{{secret.pw_e2e_key}}"}}))
	tool := saveToolFlow(t, app.testApp, nil)
	// The editor saves the plain flow again: version 2.
	current := editorGet[struct {
		Flow map[string]any `json:"flow"`
	}](t, editor, "/api/flows/"+plain.ID).Flow
	current["description"] = "edited"
	code, body := editor.do(t, http.MethodPut, "/api/flows/"+plain.ID, current)
	require.Equal(t, http.StatusOK, code, string(body))

	updateCalc := []any{map[string]any{"op": "update_node", "nodeId": "calc", "config": map[string]any{"expression": "1"}}}
	for _, tc := range []struct {
		name   string
		args   map[string]any
		code   string
		issues []string // error.flowIssues codes
		check  func(t *testing.T, e map[string]any)
	}{
		{name: "stale base version", code: "conflict",
			args: map[string]any{"flowId": plain.ID, "baseVersion": 1, "note": "n", "ops": updateCalc},
			check: func(t *testing.T, e map[string]any) {
				assert.EqualValues(t, 2, e["currentVersion"])
			}},
		{name: "unknown flow", code: "flow_not_found",
			args: map[string]any{"flowId": "no-such-flow", "baseVersion": 1, "note": "n", "ops": updateCalc}},
		{name: "sensitive literal", code: "invalid_arguments", issues: []string{service.IssueSensitiveLiteral},
			args: map[string]any{"flowId": plain.ID, "baseVersion": 2, "note": "n", "ops": []any{
				map[string]any{"op": "update_node", "nodeId": "http", "config": map[string]any{"headers": map[string]any{"X-Key": literal}}},
			}}},
		{name: "changed trigger-tool", code: "forbidden",
			args: map[string]any{"flowId": tool.ID, "baseVersion": 1, "note": "n", "ops": []any{
				map[string]any{"op": "update_node", "nodeId": "tool", "config": map[string]any{"description": "Opens every door."}},
			}},
			check: func(t *testing.T, e map[string]any) {
				assert.Contains(t, e["message"], "/nodes/tool")
			}},
		{name: "removed action-reply", code: "forbidden",
			args: map[string]any{"flowId": tool.ID, "baseVersion": 1, "note": "n", "ops": []any{
				map[string]any{"op": "remove_node", "nodeId": "reply"},
			}}},
		{name: "risk switch turned on", code: "forbidden",
			args: map[string]any{"flowId": plain.ID, "baseVersion": 2, "note": "n", "ops": []any{
				map[string]any{"op": "update_node", "nodeId": "http", "config": map[string]any{"allowPrivate": true}},
			}}},
		{name: "lint errors (H2)", code: "invalid_arguments", issues: []string{flow.IssueUnknownType, flow.IssueDuplicateNodeID},
			args: map[string]any{"name": "Broken", "note": "n", "flow": agentChain("",
				agentNode("start", "trigger-manual", nil), agentNode("x", "process-nope", nil), agentNode("x", "process-debug", nil))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := refused(t, agent, "pw_save_draft", tc.args, tc.code)
			for _, code := range tc.issues {
				assert.Contains(t, flowIssueCodes(t, e), code)
			}
			if tc.check != nil {
				tc.check(t, e)
			}
			assert.NotContains(t, string(mustJSON(t, e)), literal)
		})
	}
	assert.Empty(t, editor.drafts(t, "all"), "nothing refused is stored")

	// The two invalid_arguments refusals (the sensitive literal and the
	// Lint errors) are counted in mcp_rejections once each, not by both the
	// service and the endpoint.
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		rows, err := app.store.ListMCPRejections(storage.MCPAuditFilter{TokenID: agent.tok.ID})
		if !assert.NoError(c, err) {
			return
		}
		counts := map[string]int{}
		for _, r := range rows {
			counts[r.Reason] += r.Count
		}
		assert.Equal(c, map[string]int{"invalid_arguments": 2}, counts)
	}, 10*time.Second, 100*time.Millisecond)
}

// hitCounter is an HTTP server that must never be reached.
func hitCounter(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, `{"status":"reached"}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// defineSecret defines a secret in the process-wide store for the test.
func defineSecret(t *testing.T, name, value string) {
	t.Helper()
	require.NoError(t, secrets.DefaultStore.Set(name, value))
	t.Cleanup(func() { _ = secrets.DefaultStore.Delete(name) })
}

func traceStep(res flow.DryRunResult, nodeID string) *flow.TraceStep {
	for i := range res.Trace {
		if res.Trace[i].NodeID == nodeID {
			return &res.Trace[i]
		}
	}
	return nil
}

// pw_dry_run never reaches outside PiWorker: a node that is not safe does
// not run (its mock continues the branch), whether the flow is sent
// inline, stored or drafted. Settings the agent may not read are hidden
// from a stored flow's run, and a node they leave invalid (an expression,
// a headers object) is stubbed and mockable too; what the agent wrote in a
// draft runs. pw_test_node runs one safe processing node.
func TestMCPDryRunAndTestNode(t *testing.T) {
	app := newAuthorApp(t, true, storage.AgentDeployOff)
	agent := app.agent(t, "claude", storage.ScopeRead, storage.ScopeAuthor)
	srv, hits := hitCounter(t)
	const secretValue = "pw-dry-run-secret-9e1"
	defineSecret(t, "pw_e2e_dry_key", secretValue)
	httpCfg := map[string]any{"url": srv.URL + "/hook", "method": "POST", "allowPrivate": true}
	inline := agentChain("", agentNode("start", "trigger-manual", nil), agentNode("http", "action-http", httpCfg),
		agentNode("calc", "process-transform", map[string]any{"expression": `payload.status + "!"`}))
	stored := app.adminChain(t, "Stored", manualNode("start"),
		httpNode("http", map[string]any{"url": srv.URL + "/hook", "allowPrivate": true, "headers": map[string]any{"X-Key": "{{secret.pw_e2e_dry_key}}"}}),
		transformNode("calc", `payload.status + "!"`))
	draft := callOK[service.DraftResult](t, agent, "pw_save_draft", map[string]any{
		"flowId": stored.ID, "baseVersion": 1, "note": "Louder.",
		"ops": []any{map[string]any{"op": "update_node", "nodeId": "calc", "config": map[string]any{"expression": `payload.status + "!!"`}}},
	})

	httpMock := map[string]any{"http": map[string]any{"status": "mocked"}}
	for _, tc := range []struct {
		name    string
		args    map[string]any
		mocks   map[string]any
		stubbed []string // wouldExecute, in order
		hidden  []string // the hidden_settings warnings' paths
		want    string   // calc's output
	}{
		{"inline flow", map[string]any{"flow": inline}, httpMock, []string{"http"}, nil, `"mocked!"`},
		// Every setting of the stored flow agents may not read is hidden:
		// the headers object and the expression leave their nodes invalid.
		{"stored flow", map[string]any{"flowId": stored.ID},
			map[string]any{"http": map[string]any{"status": "mocked"}, "calc": "calc mocked"},
			[]string{"http", "calc"}, []string{"/nodes/http", "/nodes/calc"}, `"calc mocked"`},
		// The agent wrote calc's expression: it runs.
		{"draft", map[string]any{"draftId": draft.DraftID}, httpMock, []string{"http"}, []string{"/nodes/http"}, `"mocked!!"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"triggerNodeId": "start", "payload": map[string]any{"x": 1}, "mocks": tc.mocks}
			maps.Copy(args, tc.args)
			res := callOK[flow.DryRunResult](t, agent, "pw_dry_run", args)
			require.Empty(t, flow.BlockingIssues(res.Issues), "%+v", res.Issues)
			var hidden, stubbed []string
			for _, is := range res.Issues {
				if is.Code == flow.IssueHiddenSettings {
					hidden = append(hidden, is.Path)
				}
			}
			for _, w := range res.WouldExecute {
				stubbed = append(stubbed, w.NodeID)
			}
			assert.Equal(t, tc.hidden, hidden)
			assert.Equal(t, tc.stubbed, stubbed)
			calc := traceStep(res, "calc")
			require.NotNil(t, calc, "the mock continues downstream")
			require.Len(t, calc.Outputs, 1)
			assert.Equal(t, tc.want, calc.Outputs[0].Preview)
			assert.NotContains(t, string(mustJSON(t, res)), secretValue)
		})
	}
	assert.Zero(t, hits.Load(), "a dry-run never reaches the HTTP server")

	out := callOK[flow.TestNodeResult](t, agent, "pw_test_node", map[string]any{
		"type": "process-transform", "config": map[string]any{"expression": "payload.n * 2"}, "input": map[string]any{"n": 21}})
	require.Len(t, out.Outputs, 1, "%+v", out)
	assert.Equal(t, "42", out.Outputs[0].Preview)
	refused(t, agent, "pw_test_node", map[string]any{"type": "action-http", "config": httpCfg}, "forbidden")
	assert.Zero(t, hits.Load())
}

// No guessing oracle (§10): what an agent sends comes back as it sent it,
// even when it equals a secret's value (a PIN), through every authoring
// tool that echoes it; hiding it would confirm the guess.
func TestMCPEchoesOfSecretValuesComeBackAsSent(t *testing.T) {
	app := newAuthorApp(t, true, storage.AgentDeployOff)
	agent := app.agent(t, "claude", storage.ScopeRead, storage.ScopeAuthor)
	const pin = "48213"
	defineSecret(t, "pw_e2e_door_pin", pin)

	out := callOK[flow.TestNodeResult](t, agent, "pw_test_node", map[string]any{
		"type": "process-transform", "config": map[string]any{"expression": "payload"}, "input": pin})
	require.Len(t, out.Outputs, 1, "%+v", out)
	assert.Equal(t, `"`+pin+`"`, out.Outputs[0].Preview)

	res := callOK[flow.DryRunResult](t, agent, "pw_dry_run", map[string]any{"payload": pin,
		"flow": agentChain("", agentNode("start", "trigger-manual", nil), agentNode("calc", "process-transform", map[string]any{"expression": "payload"}))})
	calc := traceStep(res, "calc")
	require.NotNil(t, calc)
	require.Len(t, calc.Outputs, 1)
	assert.Equal(t, `"`+pin+`"`, calc.Outputs[0].Preview)

	verdict := callOK[service.ValidateResult](t, agent, "pw_validate_flow", map[string]any{
		"flow": agentChain("", agentNode("cron", "trigger-cron", map[string]any{"expression": pin}))})
	assert.False(t, verdict.Valid)
	assert.Contains(t, string(mustJSON(t, verdict.Issues)), `invalid cron expression \"`+pin+`\"`)
}

// The agent deploy level decides what an agent may deploy: low allows a
// safe flow and refuses destination changes (H1: and a credential used for
// another request) and privileged flows; all allows them. The editor sees
// who deployed what and rolls back to the version it approved.
func TestMCPAgentDeployPolicy(t *testing.T) {
	app := newAuthorApp(t, true, storage.AgentDeployLow)
	agent := app.agent(t, "claude", storage.ScopeRead, storage.ScopeAuthor, storage.ScopeDeploy)
	editor := app.editor(t)
	defineSecret(t, "pw_e2e_policy_key", "pw-policy-secret-31c")

	safe := app.adminChain(t, "Safe", manualNode("start"), transformNode("calc", "payload"))
	api1 := app.adminChain(t, "Report", manualNode("start"), httpNode("http", map[string]any{"url": "https://api.example.com/report"}))
	keyed := app.adminChain(t, "Keyed", manualNode("start"), httpNode("http", map[string]any{
		"url": "https://api.example.com/read", "method": "GET", "headers": map[string]any{"X-Key": "{{secret.pw_e2e_policy_key}}"}}))
	privileged := app.adminChain(t, "Command", manualNode("start"),
		newNode("cmd", "action-command", flow.NodeCategoryOutput, map[string]any{"command": "echo hi"}))

	running := func(id string) bool {
		rt, ok := app.manager.GetRuntime(id)
		return ok && rt.IsRunning()
	}
	draftOf := func(f *flow.Flow, nodeID string, cfg map[string]any) string {
		return callOK[service.DraftResult](t, agent, "pw_save_draft", map[string]any{"flowId": f.ID, "baseVersion": 1, "note": "change",
			"ops": []any{map[string]any{"op": "update_node", "nodeId": nodeID, "config": cfg}}}).DraftID
	}

	// low: a safe flow deploys and stops; the editor sees the agent did it.
	deployed := callOK[service.DeployResult](t, agent, "pw_deploy_flow", map[string]any{"flowId": safe.ID})
	assert.True(t, deployed.Running)
	assert.True(t, running(safe.ID))
	marks := editor.flow(t, safe.ID)
	require.NotNil(t, marks.Deployment)
	assert.Equal(t, storage.VersionActorAgent, marks.Deployment.ActorKind)
	assert.Equal(t, "claude", marks.Deployment.TokenName)
	assert.False(t, marks.AgentChanged)
	stopped := callOK[service.DeployResult](t, agent, "pw_stop_flow", map[string]any{"flowId": safe.ID})
	assert.False(t, stopped.Running)
	assert.False(t, running(safe.ID))

	// low refuses, naming the alert kinds and never the values.
	newHost := draftOf(api1, "http", map[string]any{"url": "https://exfil.example.net/report"})
	post := draftOf(keyed, "http", map[string]any{"method": "POST"})
	for _, tc := range []struct {
		name  string
		args  map[string]any
		kinds []string
	}{
		{"destination change", map[string]any{"flowId": api1.ID, "draftId": newHost},
			[]string{flow.AlertDestinationChanged, flow.AlertNewHost}},
		{"credentialed request changed (H1)", map[string]any{"flowId": keyed.ID, "draftId": post},
			[]string{flow.AlertCredentialedRequestChanged}},
		{"privileged flow", map[string]any{"flowId": privileged.ID}, []string{"privileged"}},
	} {
		t.Run("low refuses "+tc.name, func(t *testing.T) {
			e := refused(t, agent, "pw_deploy_flow", tc.args, "forbidden")
			for _, kind := range tc.kinds {
				assert.Contains(t, e["message"], kind)
			}
			assert.NotContains(t, e["message"], "exfil.example.net")
			assert.False(t, running(tc.args["flowId"].(string)))
		})
	}
	assert.Len(t, editor.drafts(t, "pending"), 2, "refused drafts stay pending")

	// all: the same deploys go through; an applied draft is an agent version.
	require.NoError(t, app.store.SetAgentDeployLevel(storage.AgentDeployAll))
	callOK[service.DeployResult](t, agent, "pw_deploy_flow", map[string]any{"flowId": privileged.ID})
	assert.True(t, running(privileged.ID))
	res := callOK[service.DeployResult](t, agent, "pw_deploy_flow", map[string]any{"flowId": api1.ID, "draftId": newHost})
	assert.Equal(t, 2, res.Version)
	assert.True(t, running(api1.ID))
	// The applied draft reads as the agent's deploy, not an approval the
	// admin gave (the agent's pw_get_draft says so too: service tests).
	code, body := editor.do(t, http.MethodGet, "/api/agent/drafts/"+newHost, nil)
	require.Equal(t, http.StatusOK, code, string(body))
	detail := appData[service.DraftDetail](t, body)
	assert.Equal(t, storage.DraftApproved, detail.Status)
	assert.Equal(t, 2, detail.AppliedVersion)
	assert.Equal(t, storage.DraftApplierAgent, detail.AppliedBy)
	marks = editor.flow(t, api1.ID)
	assert.True(t, marks.AgentChanged)
	require.NotNil(t, marks.Deployment)
	assert.Equal(t, storage.VersionActorAgent, marks.Deployment.ActorKind)
	assert.Equal(t, 2, marks.Deployment.Version)

	// The admin rolls back to the version it approved: saved as version 3
	// and redeployed by the editor.
	code, body = editor.do(t, http.MethodPost, "/api/flows/"+api1.ID+"/rollback", nil)
	require.Equal(t, http.StatusOK, code, string(body))
	rolled := appData[service.ApplyResult](t, body)
	assert.Equal(t, 3, rolled.Version)
	assert.True(t, rolled.Deployed)
	marks = editor.flow(t, api1.ID)
	assert.False(t, marks.AgentChanged)
	require.NotNil(t, marks.Deployment)
	assert.Equal(t, storage.VersionActorEditor, marks.Deployment.ActorKind)
	stored, err := app.store.GetFlow(api1.ID)
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com/report", stored.GetNode("http").Config["url"])
	code, _ = editor.do(t, http.MethodPost, "/api/flows/"+api1.ID+"/rollback", nil)
	assert.Equal(t, http.StatusConflict, code, "nothing left to roll back")
}

// A secret a strict setting needs must be defined before the flow runs:
// the draft is stored with the issue, approving it with a deploy is
// refused (H2) and so is an agent deploy (H8), until the admin defines it.
func TestMCPUndefinedSecretsBlockDeploys(t *testing.T) {
	app := newAuthorApp(t, true, storage.AgentDeployAll)
	agent := app.agent(t, "claude", storage.ScopeRead, storage.ScopeAuthor, storage.ScopeDeploy)
	editor := app.editor(t)
	const secretName = "pw_e2e_bot_token"

	saved := callOK[service.DraftResult](t, agent, "pw_save_draft", map[string]any{"name": "Alerts", "note": "Sends alerts.",
		"flow": agentChain("", agentNode("start", "trigger-manual", nil), agentNode("tg", "action-telegram", map[string]any{
			"botToken": "{{secret." + secretName + "}}", "chatId": "42", "message": "hi"}))})
	assert.Contains(t, issueCodes(saved.Issues), flow.IssueUndefinedSecret)

	code, body := editor.do(t, http.MethodPost, "/api/agent/drafts/"+saved.DraftID+"/approve", map[string]any{"deploy": true})
	require.Equal(t, http.StatusBadRequest, code, string(body))
	assert.Contains(t, string(body), flow.IssueUndefinedSecret)
	assert.Len(t, editor.drafts(t, "pending"), 1, "a refused approval leaves the draft pending")

	code, body = editor.do(t, http.MethodPost, "/api/agent/drafts/"+saved.DraftID+"/approve", map[string]any{"deploy": false})
	require.Equal(t, http.StatusOK, code, string(body))
	flowID := appData[service.ApplyResult](t, body).FlowID

	e := refused(t, agent, "pw_deploy_flow", map[string]any{"flowId": flowID}, "invalid_arguments")
	assert.Contains(t, flowIssueCodes(t, e), flow.IssueUndefinedSecret)
	defineSecret(t, secretName, "123:pw-bot-token")
	callOK[service.DeployResult](t, agent, "pw_deploy_flow", map[string]any{"flowId": flowID})
}

// waitForLifecycleWaiter waits until a goroutine is blocked on the
// lifecycle lock (FlowsHandler.WithLock).
func waitForLifecycleWaiter(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		buf := make([]byte, 4<<20)
		buf = buf[:runtime.Stack(buf, true)]
		for g := range strings.SplitSeq(string(buf), "\n\n") {
			if strings.Contains(g, "(*FlowsHandler).WithLock") && strings.Contains(g, "Mutex).lockSlow") {
				return true
			}
		}
		return false
	}, 10*time.Second, 5*time.Millisecond, "the deploy never waited for the lifecycle lock")
}

// H3: an agent deploy evaluated before something changed is checked again
// under the lifecycle lock and deploys nothing: a revoked token, a flow
// saved meanwhile, an agent deploy level turned off.
func TestMCPAgentDeployRechecksUnderTheLifecycleLock(t *testing.T) {
	for _, tc := range []struct {
		name      string
		meanwhile func(t *testing.T, app mcpApp, editor editorSession, tok *storage.APIToken, f *flow.Flow)
		code      string
	}{
		{"token revoked", func(t *testing.T, _ mcpApp, editor editorSession, tok *storage.APIToken, _ *flow.Flow) {
			code, body := editor.do(t, http.MethodDelete, "/api/agent/tokens/"+tok.ID, nil)
			require.Equal(t, http.StatusOK, code, string(body))
		}, "forbidden"},
		{"flow saved meanwhile", func(t *testing.T, app mcpApp, _ editorSession, _ *storage.APIToken, f *flow.Flow) {
			// The editor's own save waits for the lock: save in storage.
			f.Description = "changed"
			require.NoError(t, app.store.UpdateFlow(f))
		}, "conflict"},
		{"agent deploy turned off", func(t *testing.T, app mcpApp, _ editorSession, _ *storage.APIToken, _ *flow.Flow) {
			require.NoError(t, app.store.SetAgentDeployLevel(storage.AgentDeployOff))
		}, "forbidden"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newAuthorApp(t, true, storage.AgentDeployAll)
			agent := app.agent(t, "claude", storage.ScopeDeploy)
			editor := app.editor(t)
			f := app.adminChain(t, "Safe", manualNode("start"), transformNode("calc", "payload"))

			var res *mcp.CallToolResult
			var callErr error
			done := make(chan struct{})
			require.NoError(t, app.flows.WithLock(t.Context(), func(service.LockedLifecycle) error {
				go func() {
					defer close(done)
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					res, callErr = agent.session.CallTool(ctx, &mcp.CallToolParams{Name: "pw_deploy_flow", Arguments: map[string]any{"flowId": f.ID}})
				}()
				waitForLifecycleWaiter(t)
				tc.meanwhile(t, app, editor, agent.tok, f)
				return nil
			}))
			<-done
			require.NoError(t, callErr)
			require.True(t, res.IsError, resultText(res))
			assert.Equal(t, tc.code, errCode(structured(t, res)))
			rt, ok := app.manager.GetRuntime(f.ID)
			assert.False(t, ok && rt.IsRunning(), "nothing was deployed")
			assert.Nil(t, editor.flow(t, f.ID).Deployment)
		})
	}
}

// Revoking a token voids its pending drafts in the same transaction; other
// tokens' drafts stay.
func TestMCPRevokingATokenVoidsItsDrafts(t *testing.T) {
	app := newAuthorApp(t, true, storage.AgentDeployOff)
	agent := app.agent(t, "claude", storage.ScopeAuthor)
	other := app.agent(t, "other", storage.ScopeAuthor)
	editor := app.editor(t)
	draft := func(s agentSession, name string) string {
		return callOK[service.DraftResult](t, s, "pw_save_draft", map[string]any{"name": name, "note": "n",
			"flow": agentChain("", agentNode("start", "trigger-manual", nil), agentNode("calc", "process-transform", map[string]any{"expression": "1"}))}).DraftID
	}
	mine := []string{draft(agent, "one"), draft(agent, "two")}
	theirs := draft(other, "three")

	code, body := editor.do(t, http.MethodDelete, "/api/agent/tokens/"+agent.tok.ID, nil)
	require.Equal(t, http.StatusOK, code, string(body))

	pending := editor.drafts(t, "pending")
	require.Len(t, pending, 1)
	assert.Equal(t, theirs, pending[0].ID)
	assert.Equal(t, 1, editorGet[api.PendingApprovalsCount](t, editor, api.PendingApprovalsCountPath).Drafts)
	for _, id := range mine {
		d := editorGet[service.DraftDetail](t, editor, "/api/agent/drafts/"+id)
		assert.Equal(t, storage.DraftVoid, d.Status)
		assert.NotEmpty(t, d.Reason)
	}
	code, _ = editor.do(t, http.MethodPost, "/api/agent/drafts/"+mine[0]+"/approve", map[string]any{"deploy": false})
	assert.Equal(t, http.StatusConflict, code, "a void draft cannot be approved")
}
