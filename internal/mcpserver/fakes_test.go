package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/toolschema"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Raw tokens of the fake verifier (shaped like real ones, so a leak in the
// logs would be recognisable).
const (
	callToken   = "pwk_callcallcallcall_" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	readToken   = "pwk_readreadreadread_" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	authorToken = "pwk_authorauthorauth_" + "cccccccccccccccccccccccccccccccccccccccccccccccccccc"
	otherToken  = "pwk_otherotherotherr_" + "dddddddddddddddddddddddddddddddddddddddddddddddddddd"
	badToken    = "pwk_badbadbadbadbadb_" + "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	brokenToken = "pwk_brokenbrokenbrok_" + "ffffffffffffffffffffffffffffffffffffffffffffffffffff"
)

// fakeVerifier verifies the raw tokens of its map. Like the real one it
// caches the tokens it verified: VerifyCached knows only those. calls
// counts Verify (the store lookups of the real one).
type fakeVerifier struct {
	mu     sync.Mutex
	tokens map[string]agentauth.TokenInfo
	cached map[string]bool
	epoch  int64
	delay  time.Duration
	calls  atomic.Int32
}

func newFakeVerifier() *fakeVerifier {
	return &fakeVerifier{cached: map[string]bool{}, tokens: map[string]agentauth.TokenInfo{
		callToken:   {ID: "callcallcallcall", Name: "laptop", Scopes: []string{storage.ScopeCall}, AllTools: true},
		readToken:   {ID: "readreadreadread", Name: "reader", Scopes: []string{storage.ScopeRead}},
		authorToken: {ID: "authorauthorauth", Name: "author", Scopes: []string{storage.ScopeAuthor}},
		otherToken:  {ID: "otherotherotherr", Name: "desk", Scopes: []string{storage.ScopeCall, storage.ScopeRead}, AllTools: true},
	}}
}

func (v *fakeVerifier) Verify(raw string) (agentauth.TokenInfo, error) {
	v.calls.Add(1)
	v.mu.Lock()
	delay := v.delay
	v.mu.Unlock()
	time.Sleep(delay)
	if raw == brokenToken {
		return agentauth.TokenInfo{}, errStoreDown
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	info, ok := v.tokens[raw]
	if !ok {
		return agentauth.TokenInfo{}, agentauth.ErrInvalidToken
	}
	v.cached[raw] = true
	return info, nil
}

func (v *fakeVerifier) VerifyCached(raw string) (agentauth.TokenInfo, bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	info, ok := v.tokens[raw]
	if !ok || !v.cached[raw] {
		return agentauth.TokenInfo{}, false, nil
	}
	return info, true, nil
}

func (v *fakeVerifier) Epoch() int64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.epoch
}

func (v *fakeVerifier) set(raw string, fn func(*agentauth.TokenInfo)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	info := v.tokens[raw]
	fn(&info)
	v.tokens[raw] = info
}

func (v *fakeVerifier) revoke(raw string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.tokens, raw)
	delete(v.cached, raw)
}

// add registers another call-scope token and returns it.
func (v *fakeVerifier) add(id string) string {
	raw := "pwk_" + id + "_" + strings.Repeat("a", 52)
	v.mu.Lock()
	defer v.mu.Unlock()
	v.tokens[raw] = agentauth.TokenInfo{ID: id, Name: id, Scopes: []string{storage.ScopeCall}}
	return raw
}

func (v *fakeVerifier) bumpEpoch() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.epoch++
}

var errStoreDown = io.ErrUnexpectedEOF

// fakeTools lists every tool to agents with the call scope and answers calls
// with call (default: completed, echoing the arguments).
type fakeTools struct {
	mu      sync.Mutex
	version uint64
	tools   []flow.ToolInfo
	call    func(ctx context.Context, actor service.Actor, ref service.ToolRef, args map[string]any) (flow.CallResult, error)
	result  func(ctx context.Context, actor service.Actor, runID string) (flow.CallResult, error)
	lists   atomic.Int32
	calls   atomic.Int32
}

func (f *fakeTools) List(actor service.Actor) (uint64, []flow.ToolInfo) {
	f.lists.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if !actor.HasScope(storage.ScopeCall) {
		return f.version, nil
	}
	return f.version, append([]flow.ToolInfo(nil), f.tools...)
}

func (f *fakeTools) Call(ctx context.Context, actor service.Actor, ref service.ToolRef, args map[string]any) (flow.CallResult, error) {
	f.calls.Add(1)
	f.mu.Lock()
	call := f.call
	f.mu.Unlock()
	if call != nil {
		return call(ctx, actor, ref, args)
	}
	data, _ := json.Marshal(args)
	return flow.CallResult{Status: flow.CallStatusCompleted, RunID: "run-1", Result: data}, nil
}

func (f *fakeTools) Result(ctx context.Context, actor service.Actor, runID string) (flow.CallResult, error) {
	f.mu.Lock()
	result := f.result
	f.mu.Unlock()
	if result != nil {
		return result(ctx, actor, runID)
	}
	return flow.CallResult{}, &service.ToolError{Kind: service.ErrRunNotFound, Message: "unknown run"}
}

func (f *fakeTools) setTools(version uint64, tools ...flow.ToolInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.version, f.tools = version, tools
}

// testTool is a published tool taking {"door": "left"|"right"}.
func testTool(t *testing.T, name string) flow.ToolInfo {
	t.Helper()
	schema, err := toolschema.Compile(json.RawMessage(`{"type":"object","properties":{"door":{"type":"string","enum":["left","right"]}},"required":["door"]}`))
	require.NoError(t, err)
	return flow.ToolInfo{
		FlowID: "flow-" + name, TriggerNodeID: "tool", ToolName: name, Title: "Garage " + name,
		Description: "Opens the left or right garage door and reports whether it moved.",
		Schema:      schema, Risk: node.RiskSafe,
		Annotations: flow.Annotations{OpenWorldHint: false, ReadOnlyHint: false, IdempotentHint: true},
		Fingerprint: "sha256:" + strings.Repeat("a", 64), Confirm: node.ToolConfirmNone,
	}
}

type testEnv struct {
	t        *testing.T
	h        *Handler
	verifier *fakeVerifier
	tools    *fakeTools
	clock    *fakeClock
	logs     *syncBuffer
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newTestEnv(t *testing.T, customize ...func(*Options)) *testEnv {
	t.Helper()
	e := &testEnv{t: t, verifier: newFakeVerifier(), tools: &fakeTools{version: 1}, clock: newClock(), logs: &syncBuffer{}}
	opts := Options{
		Tools:       e.tools,
		Tokens:      e.verifier,
		TLS:         true,
		HostAllowed: func(host string) bool { return normalizeHostname(host) == "piworker.local" },
		Version:     "test",
		Now:         e.clock.Now,
		Logger:      zerolog.New(e.logs),
	}
	for _, c := range customize {
		c(&opts)
	}
	h, err := New(opts)
	require.NoError(t, err)
	e.h = h
	return e
}

// rpc builds a JSON-RPC POST to /mcp from a LAN client over TLS, at
// protocol version 2025-06-18.
func rpc(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "https://piworker.local:8443/mcp", strings.NewReader(body))
	r.RemoteAddr = "192.168.1.20:50000"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	return r
}

func withToken(r *http.Request, raw string) *http.Request {
	r.Header.Set("Authorization", "Bearer "+raw)
	return r
}

const (
	initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	listToolsBody  = `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
)

func callBody(name string, args string) string {
	return `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`
}

func (e *testEnv) serve(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

// rpcRefusal decodes a JSON-RPC error answered with HTTP status.
func rpcRefusal(t *testing.T, w *httptest.ResponseRecorder, status int) (id any, code float64, msg string) {
	t.Helper()
	require.Equal(t, status, w.Code, w.Body.String())
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	var resp struct {
		ID    any `json:"id"`
		Error struct {
			Code    float64 `json:"code"`
			Message string  `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	return resp.ID, resp.Error.Code, resp.Error.Message
}

// rpcResult decodes a JSON-RPC response body.
func rpcResult(t *testing.T, w *httptest.ResponseRecorder) (result map[string]any, rpcErr map[string]any) {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		Result map[string]any `json:"result"`
		Error  map[string]any `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	return resp.Result, resp.Error
}
