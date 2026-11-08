package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/api"
	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
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

func newMCPApp(t *testing.T, enabled bool) mcpApp {
	t.Helper()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "app.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	manager := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	t.Cleanup(manager.StopAll)
	cfg := config.Default()
	cfg.Auth = true
	cfg.MCP.Enabled = enabled
	cfg.AllowedHosts = []string{"piworker.local"}
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
	return res
}

// structured decodes a result's structured content.
func structured(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	data, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
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
	assert.Contains(t, string(body), `"invalid_arguments"`)
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
