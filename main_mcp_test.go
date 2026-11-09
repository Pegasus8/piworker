package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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

// Golden shapes of the protocol: initialize (or server/discover) and
// tools/list as raw JSON-RPC, for every protocol version /mcp negotiates.
func TestMCPProtocolGolden(t *testing.T) {
	app := newMCPApp(t, true)
	garage := app.deployTool(t, "garage", nil)
	_, raw := app.token(t, "laptop", []string{storage.ScopeCall}, garage)

	const serverInfo = `{"name":"piworker","title":"PiWorker","version":"dev"}`
	const tools = `[
	  {"name":"garage","title":"Garage door",
	   "description":"Opens the left or right garage door and reports whether it moved.",
	   "inputSchema":{"type":"object","additionalProperties":false,
	     "properties":{"door":{"type":"string","enum":["left","right"],"maxLength":256}}},
	   "annotations":{"readOnlyHint":true,"destructiveHint":false,"idempotentHint":false,"openWorldHint":false}},
	  {"name":"pw_get_result","title":"Get the result of a tool call",
	   "description":"Returns the result of an earlier tool call of this token that answered status \"running\" or \"approval_required\", by its runId. A running call is waited for up to 10 seconds; call again while the status is still \"running\" or \"approval_required\".",
	   "inputSchema":{"type":"object","additionalProperties":false,"required":["runId"],
	     "properties":{"runId":{"type":"string","minLength":1,"maxLength":128,"description":"The runId of the call, from its result."}}},
	   "annotations":{"readOnlyHint":true,"destructiveHint":false,"idempotentHint":true,"openWorldHint":false}}]`

	// golden checks a 200 JSON answer against want, once the instructions
	// (prose, checked for presence only) are removed.
	golden := func(t *testing.T, resp *http.Response, body []byte, want string) {
		t.Helper()
		require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
		var msg map[string]any
		require.NoError(t, json.Unmarshal(body, &msg), string(body))
		if result, ok := msg["result"].(map[string]any); ok {
			if instr, ok := result["instructions"]; ok {
				assert.True(t, strings.HasPrefix(instr.(string), "PiWorker runs automations"), instr)
				delete(result, "instructions")
			}
		}
		got, err := json.Marshal(msg)
		require.NoError(t, err)
		assert.JSONEq(t, want, string(got))
	}
	initialize := func(version string) string {
		return `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + version +
			`","capabilities":{},"clientInfo":{"name":"golden","version":"1"}}}`
	}

	for _, version := range []string{"2025-06-18", "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			resp, body := app.post(t, raw, initialize(version), nil)
			golden(t, resp, body, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"`+version+`",
			  "capabilities":{"prompts":{},"tools":{}},"serverInfo":`+serverInfo+`}}`)
			resp, body = app.post(t, raw, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, map[string]string{"Mcp-Protocol-Version": version})
			golden(t, resp, body, `{"jsonrpc":"2.0","id":2,"result":{"ttlMs":5000,"cacheScope":"private","tools":`+tools+`}}`)
		})
	}

	t.Run("2026-07-28", func(t *testing.T) {
		// The legacy handshake cannot negotiate into the sessionless
		// protocol: it settles on the newest handshake version.
		resp, body := app.post(t, raw, initialize("2026-07-28"), nil)
		golden(t, resp, body, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25",
		  "capabilities":{"prompts":{},"tools":{}},"serverInfo":`+serverInfo+`}}`)

		meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
			`"io.modelcontextprotocol/clientInfo":{"name":"golden","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}`
		headers := func(method string) map[string]string {
			return map[string]string{"Mcp-Protocol-Version": "2026-07-28", "Mcp-Method": method}
		}
		resp, body = app.post(t, raw, `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{`+meta+`}}`, headers("server/discover"))
		golden(t, resp, body, `{"jsonrpc":"2.0","id":1,"result":{"resultType":"complete",
		  "_meta":{"io.modelcontextprotocol/serverInfo":`+serverInfo+`},"ttlMs":5000,"cacheScope":"private",
		  "supportedVersions":["2026-07-28","2025-11-25","2025-06-18"],"capabilities":{"prompts":{},"tools":{}}}}`)
		resp, body = app.post(t, raw, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{`+meta+`}}`, headers("tools/list"))
		golden(t, resp, body, `{"jsonrpc":"2.0","id":2,"result":{"resultType":"complete",
		  "_meta":{"io.modelcontextprotocol/serverInfo":`+serverInfo+`},"ttlMs":5000,"cacheScope":"private","tools":`+tools+`}}`)
	})

	// Versions /mcp does not negotiate are refused on the transport, and so
	// are requests without a version (which the SDK would serve as
	// 2025-03-26) and batches (which no negotiated version allows).
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
	marks = editor.flow(t, api1.ID)
	assert.True(t, marks.AgentChanged)
	require.NotNil(t, marks.Deployment)
	assert.Equal(t, storage.VersionActorAgent, marks.Deployment.ActorKind)
	assert.Equal(t, 2, marks.Deployment.Version)

	// The admin rolls back to the version it approved: saved as version 3
	// and redeployed by the editor.
	code, body := editor.do(t, http.MethodPost, "/api/flows/"+api1.ID+"/rollback", nil)
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
