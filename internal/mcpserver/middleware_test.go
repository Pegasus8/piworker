package mcpserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireJSONError checks a refusal: status, a JSON body with the PiWorker
// error envelope and no caching.
func requireJSONError(t *testing.T, w *httptest.ResponseRecorder, status int) string {
	t.Helper()
	require.Equal(t, status, w.Code, w.Body.String())
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var body struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	require.NotNil(t, body.Success)
	assert.False(t, *body.Success)
	assert.NotEmpty(t, body.Error)
	return body.Error
}

// (a) Without TLS, /mcp serves loopback clients only, unless lan_access.
func TestLoopbackOnlyWhenTLSIsOff(t *testing.T) {
	e := newTestEnv(t, func(o *Options) { o.TLS = false })
	lan := withToken(rpc(listToolsBody), callToken)
	requireJSONError(t, e.serve(lan), http.StatusForbidden)
	assert.Zero(t, e.verifier.calls.Load(), "refused before the token is looked at")

	for _, addr := range []string{"127.0.0.1:4000", "[::1]:4000"} {
		r := withToken(rpc(listToolsBody), callToken)
		r.RemoteAddr = addr
		_, rpcErr := rpcResult(t, e.serve(r))
		assert.Nil(t, rpcErr, addr)
	}

	lanAccess := newTestEnv(t, func(o *Options) { o.TLS, o.LANAccess = false, true })
	_, rpcErr := rpcResult(t, lanAccess.serve(withToken(rpc(listToolsBody), callToken)))
	assert.Nil(t, rpcErr)

	tlsOn := newTestEnv(t)
	_, rpcErr = rpcResult(t, tlsOn.serve(withToken(rpc(listToolsBody), callToken)))
	assert.Nil(t, rpcErr, "with TLS, LAN clients are served")
}

// (b) The Host must be a name of this server (DNS rebinding).
func TestHostAllowlist(t *testing.T) {
	e := newTestEnv(t)
	r := withToken(rpc(listToolsBody), callToken)
	r.Host = "attacker.example"
	msg := requireJSONError(t, e.serve(r), http.StatusForbidden)
	assert.Contains(t, msg, "allowed_hosts")
	assert.Zero(t, e.verifier.calls.Load())
}

// (c) A browser's Origin must be this server or an allowlisted origin.
func TestOriginCheck(t *testing.T) {
	e := newTestEnv(t, func(o *Options) { o.AllowedOrigins = []string{"http://localhost:3000"} })
	for origin, ok := range map[string]bool{
		"":                              true,
		"https://piworker.local:8443":   true,
		"https://PIWORKER.local:8443":   true,
		"http://localhost:3000":         true,
		"https://evil.example":          false,
		"https://piworker.local":        false, // another port
		"http://piworker.local:8443":    false, // another scheme over TLS
		"null":                          false,
		"https://piworker.local:8443/x": false,
	} {
		r := withToken(rpc(listToolsBody), callToken)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := e.serve(r)
		if ok {
			_, rpcErr := rpcResult(t, w)
			assert.Nil(t, rpcErr, origin)
		} else {
			requireJSONError(t, w, http.StatusForbidden)
		}
	}

	// Without TLS (e.g. behind an HTTPS reverse proxy) both schemes of the
	// same host are this server.
	plain := newTestEnv(t, func(o *Options) { o.TLS, o.LANAccess = false, true })
	for _, origin := range []string{"http://piworker.local:8443", "https://piworker.local:8443"} {
		r := withToken(rpc(listToolsBody), callToken)
		r.URL.Scheme, r.TLS = "http", nil
		r.Header.Set("Origin", origin)
		_, rpcErr := rpcResult(t, plain.serve(r))
		assert.Nil(t, rpcErr, origin)
	}
}

// (d) Tool calls may take 30 s: the write deadline is extended past the
// server's WriteTimeout.
func TestWriteDeadlineIsExtended(t *testing.T) {
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		io.WriteString(w, "done")
	})
	for name, tc := range map[string]struct {
		handler http.Handler
		want    bool
	}{
		"without": {slow, false},
		"with":    {extendWriteDeadline(2*time.Second, slow), true},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewUnstartedServer(tc.handler)
			srv.Config.WriteTimeout = 100 * time.Millisecond
			srv.StartTLS()
			defer srv.Close()
			resp, err := srv.Client().Get(srv.URL)
			if !tc.want {
				if err == nil {
					body, readErr := io.ReadAll(resp.Body)
					resp.Body.Close()
					assert.True(t, readErr != nil || string(body) != "done", "the server's WriteTimeout cuts the answer")
				}
				return
			}
			require.NoError(t, err)
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, "done", string(body))
		})
	}
}

// (e) Bearer tokens only: 401 with a challenge and a JSON body otherwise.
func TestBearerAuthentication(t *testing.T) {
	e := newTestEnv(t)
	cases := map[string]func(*http.Request){
		"no credentials":   func(*http.Request) {},
		"a session cookie": func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "piworker_session", Value: "s3ss10n"}) },
		"basic auth":       func(r *http.Request) { r.SetBasicAuth("admin", "pw") },
		"empty bearer":     func(r *http.Request) { r.Header.Set("Authorization", "Bearer ") },
		"bad token":        func(r *http.Request) { withToken(r, badToken) },
		"revoked token": func(r *http.Request) {
			e.verifier.revoke(otherToken)
			withToken(r, otherToken)
		},
		"token in the query": func(r *http.Request) { r.URL.RawQuery = "access_token=" + callToken },
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			r := rpc(listToolsBody)
			r.RemoteAddr = fmt.Sprintf("192.168.1.%d:1000", 30+len(name)) // keep clear of the failure limit
			prepare(r)
			w := e.serve(r)
			requireJSONError(t, w, http.StatusUnauthorized)
			challenge := w.Header().Get("WWW-Authenticate")
			assert.True(t, strings.HasPrefix(challenge, `Bearer realm="piworker"`), challenge)
		})
	}
	w := e.serve(withToken(rpc(listToolsBody), badToken))
	assert.Contains(t, w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)

	// The scheme is case-insensitive.
	r := rpc(listToolsBody)
	r.Header.Set("Authorization", "bearer "+callToken)
	_, rpcErr := rpcResult(t, e.serve(r))
	assert.Nil(t, rpcErr)
}

// A store failure is not the client's fault: 503, not 401.
func TestVerifierFailureIsUnavailable(t *testing.T) {
	e := newTestEnv(t)
	w := e.serve(withToken(rpc(listToolsBody), brokenToken))
	requireJSONError(t, w, http.StatusServiceUnavailable)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
}

// Failed authentications are limited per client address (not by the login
// limiter): after 10 a minute the client gets 429 until the window frees,
// even with a good token, and the verifier is not consulted.
func TestAuthFailureLimiter(t *testing.T) {
	e := newTestEnv(t)
	for i := range 10 {
		requireJSONError(t, e.serve(withToken(rpc(listToolsBody), badToken)), http.StatusUnauthorized)
		_ = i
	}
	before := e.verifier.calls.Load()
	w := e.serve(withToken(rpc(listToolsBody), callToken))
	requireJSONError(t, w, http.StatusTooManyRequests)
	assert.Equal(t, "6", w.Header().Get("Retry-After"))
	assert.Equal(t, before, e.verifier.calls.Load(), "blocked clients never reach the token store")

	other := withToken(rpc(listToolsBody), callToken)
	other.RemoteAddr = "192.168.1.99:1"
	_, rpcErr := rpcResult(t, e.serve(other))
	assert.Nil(t, rpcErr, "other clients are not affected")

	e.clock.Advance(6 * time.Second)
	_, rpcErr = rpcResult(t, e.serve(withToken(rpc(listToolsBody), callToken)))
	assert.Nil(t, rpcErr)

	// Requests without credentials are not guesses and do not count.
	e2 := newTestEnv(t)
	for range 20 {
		requireJSONError(t, e2.serve(rpc(listToolsBody)), http.StatusUnauthorized)
	}
	_, rpcErr = rpcResult(t, e2.serve(withToken(rpc(listToolsBody), callToken)))
	assert.Nil(t, rpcErr)
}

// A client past the failure limit cannot lock out agents sharing its key
// (a whole IPv6 /64, every loopback client): tokens the verifier already
// holds in its cache are served without a store lookup, so only lookups are
// refused while the client is limited.
func TestAuthFailureLimiterKeepsServingKnownTokens(t *testing.T) {
	e := newTestEnv(t)
	from := func(r *http.Request, addr string) *http.Request { r.RemoteAddr = addr; return r }
	const agent, attacker = "[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:4000", "[2001:db8:1:2::bad]:5000"
	_, rpcErr := rpcResult(t, e.serve(from(withToken(rpc(listToolsBody), callToken), agent)))
	require.Nil(t, rpcErr)

	for range 10 {
		requireJSONError(t, e.serve(from(withToken(rpc(listToolsBody), badToken), attacker)), http.StatusUnauthorized)
	}
	requireJSONError(t, e.serve(from(withToken(rpc(listToolsBody), badToken), attacker)), http.StatusTooManyRequests)

	_, rpcErr = rpcResult(t, e.serve(from(withToken(rpc(listToolsBody), callToken), agent)))
	assert.Nil(t, rpcErr, "a known token is served while its /64 is limited")
	calls := e.verifier.calls.Load()
	requireJSONError(t, e.serve(from(withToken(rpc(listToolsBody), otherToken), agent)), http.StatusTooManyRequests)
	assert.Equal(t, calls, e.verifier.calls.Load(), "an unknown one needs a lookup, which is refused")
}

// The failure budget is reserved before a lookup, so concurrent lookups
// from one client cannot all pass the check before any of them fails.
func TestAuthFailureLimiterHoldsUnderConcurrency(t *testing.T) {
	e := newTestEnv(t)
	e.verifier.delay = 20 * time.Millisecond // a SQLite read
	var wg sync.WaitGroup
	var unauthorized, limited atomic.Int32
	for range 100 {
		wg.Go(func() {
			switch e.serve(withToken(rpc(listToolsBody), badToken)).Code {
			case http.StatusUnauthorized:
				unauthorized.Add(1)
			case http.StatusTooManyRequests:
				limited.Add(1)
			}
		})
	}
	wg.Wait()
	assert.EqualValues(t, DefaultAuthFailuresPerMinute, e.verifier.calls.Load(), "lookups")
	assert.EqualValues(t, DefaultAuthFailuresPerMinute, unauthorized.Load())
	assert.EqualValues(t, 100-DefaultAuthFailuresPerMinute, limited.Load())
}

// Lookups that succeed, or fail on the store, give their reservation back:
// only bad tokens count.
func TestAuthFailureLimiterCountsOnlyBadTokens(t *testing.T) {
	e := newTestEnv(t)
	for i := range 15 {
		raw := e.verifier.add(fmt.Sprintf("agent%011d", i))
		_, rpcErr := rpcResult(t, e.serve(withToken(rpc(listToolsBody), raw)))
		require.Nil(t, rpcErr, i)
		requireJSONError(t, e.serve(withToken(rpc(listToolsBody), brokenToken)), http.StatusServiceUnavailable)
	}
	requireJSONError(t, e.serve(withToken(rpc(listToolsBody), badToken)), http.StatusUnauthorized)
}

// Tokens never reach the log, whatever happens to the request.
func TestTokensAreNeverLogged(t *testing.T) {
	e := newTestEnv(t)
	for _, raw := range []string{badToken, brokenToken, callToken, "not-a-token-" + strings.Repeat("z", 40)} {
		e.serve(withToken(rpc(listToolsBody), raw))
	}
	for range 12 {
		e.serve(withToken(rpc(listToolsBody), badToken))
	}
	logs := e.logs.String()
	assert.NotEmpty(t, logs, "failures are logged")
	for _, raw := range []string{badToken, brokenToken, callToken} {
		_, secret, _ := strings.Cut(strings.TrimPrefix(raw, "pwk_"), "_")
		assert.NotContains(t, logs, secret)
	}
	assert.NotContains(t, logs, strings.Repeat("z", 40))
}

// (f) Each token may send 30 requests a minute, in bursts of 10.
func TestPerTokenRateLimit(t *testing.T) {
	e := newTestEnv(t)
	for i := range 10 {
		_, rpcErr := rpcResult(t, e.serve(withToken(rpc(listToolsBody), callToken)))
		require.Nil(t, rpcErr, "request %d", i)
	}
	w := e.serve(withToken(rpc(listToolsBody), callToken))
	msg := requireJSONError(t, w, http.StatusTooManyRequests)
	assert.Contains(t, msg, "rate")
	assert.Equal(t, "2", w.Header().Get("Retry-After"))

	_, rpcErr := rpcResult(t, e.serve(withToken(rpc(listToolsBody), otherToken)))
	assert.Nil(t, rpcErr, "each token has its own budget")
	e.clock.Advance(2 * time.Second)
	_, rpcErr = rpcResult(t, e.serve(withToken(rpc(listToolsBody), callToken)))
	assert.Nil(t, rpcErr)
}

// (g) A token needs a scope /mcp serves, and a tool call needs the scope of
// the tool it names: call for flow tools (any name that is not a built-in
// tool, so hidden and unknown tools are alike).
func TestScopeCheck(t *testing.T) {
	e := newTestEnv(t)
	e.tools.setTools(2, testTool(t, "garage"))

	msg := requireJSONError(t, e.serve(withToken(rpc(listToolsBody), authorToken)), http.StatusForbidden)
	assert.Contains(t, msg, "scope")

	_, rpcErr := rpcResult(t, e.serve(withToken(rpc(listToolsBody), readToken)))
	assert.Nil(t, rpcErr, "read is served")

	for _, name := range []string{"garage", "no_such_tool", "pw_get_result"} {
		w := e.serve(withToken(rpc(callBody(name, `{"door":"left"}`)), readToken))
		msg := requireJSONError(t, w, http.StatusForbidden)
		assert.Contains(t, msg, "call scope", name)
	}
	assert.Zero(t, e.tools.calls.Load())

	res, rpcErr := rpcResult(t, e.serve(withToken(rpc(callBody("garage", `{"door":"left"}`)), callToken)))
	require.Nil(t, rpcErr)
	assert.NotEqual(t, true, res["isError"])
}

// Only a body that is not JSON (or has duplicate names) is a parse error
// (-32700, id null). Valid JSON that is not an object is an invalid request
// (-32600, id null), and so is an object whose method is not a string
// (-32600, with the request's id); params of the wrong shape (not an
// object, a tool or prompt name that is not a string) are the SDK's invalid
// params (-32602, with the request's id), like any other params of the
// wrong type. The scope check reads such a call as one of an unknown tool.
func TestOnlyInvalidJSONIsAParseError(t *testing.T) {
	e := newTestEnv(t)
	n := 0
	serve := func(raw, body string) *httptest.ResponseRecorder {
		n++
		if raw == "" {
			raw = e.verifier.add(fmt.Sprintf("parse%011d", n))
		}
		return e.serve(withToken(rpc(body), raw))
	}
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"garage","arguments":{},"_meta":5}}`, // the SDK's own
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":42}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":["garage"]}`,
		`{"jsonrpc":"2.0","id":5,"method":"prompts/get","params":{"name":{"x":1}}}`,
	} {
		id, code, _ := rpcRefusal(t, serve("", body), http.StatusOK)
		assert.EqualValues(t, 5, id, body)
		assert.EqualValues(t, -32602, code, body)
	}
	id, code, _ := rpcRefusal(t, serve("", `{"jsonrpc":"2.0","id":5,"method":5}`), http.StatusBadRequest)
	assert.EqualValues(t, 5, id)
	assert.EqualValues(t, -32600, code)
	for _, body := range []string{`5`, ` "tools/list" `, `null`, `true`} {
		id, code, msg := rpcRefusal(t, serve("", body), http.StatusBadRequest)
		assert.Nil(t, id, body)
		assert.EqualValues(t, -32600, code, body)
		assert.Contains(t, msg, "object", body)
	}
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":5,"method":"tools/call",`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"garage","name":"pw_deploy_flow"}}`,
	} {
		id, code, _ := rpcRefusal(t, serve("", body), http.StatusBadRequest)
		assert.Nil(t, id, body)
		assert.EqualValues(t, -32700, code, body)
	}
	// Without the call scope, a call naming no tool is refused like one
	// naming an unknown tool.
	requireJSONError(t, serve(readToken, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":42}}`), http.StatusForbidden)
	assert.Zero(t, e.tools.calls.Load())
}

// Bodies are bounded before they are parsed.
func TestRequestBodyLimit(t *testing.T) {
	e := newTestEnv(t, func(o *Options) { o.MaxRequestBodyBytes = 1024 })
	big := callBody("garage", `{"door":"`+strings.Repeat("x", 2048)+`"}`)
	w := e.serve(withToken(rpc(big), callToken))
	requireJSONError(t, w, http.StatusRequestEntityTooLarge)
}

// Batches are refused before the SDK sees them: no protocol version /mcp
// negotiates allows them, and one request carrying many calls would get
// around the per-token rate limit (and hold every answer in memory).
func TestBatchesAreRefused(t *testing.T) {
	e := newTestEnv(t)
	e.tools.setTools(2, testTool(t, "garage"))
	calls := `[` + callBody("garage", `{"door":"left"}`) + `,` + callBody("garage", `{"door":"right"}`) + `]`
	for name, tc := range map[string]struct{ body, version string }{
		"calls without a version header": {calls, ""},
		"calls at 2025-06-18":            {calls, "2025-06-18"},
		"one list":                       {`[` + listToolsBody + `]`, ""},
		"empty":                          {` [ ] `, ""},
	} {
		t.Run(name, func(t *testing.T) {
			r := withToken(rpc(tc.body), otherToken)
			r.Header.Del("Mcp-Protocol-Version")
			if tc.version != "" {
				r.Header.Set("Mcp-Protocol-Version", tc.version)
			}
			id, code, msg := rpcRefusal(t, e.serve(r), http.StatusBadRequest)
			assert.Nil(t, id)
			assert.EqualValues(t, -32600, code)
			assert.Contains(t, msg, "batch")
		})
	}
	assert.Zero(t, e.tools.calls.Load(), "no call of a batch ran")
}

// Every request but initialize names its protocol version in the
// Mcp-Protocol-Version header, as the versions /mcp negotiates require:
// without it the SDK would serve the request as 2025-03-26, which /mcp does
// not negotiate.
func TestProtocolVersionHeaderIsRequired(t *testing.T) {
	e := newTestEnv(t)
	e.tools.setTools(2, testTool(t, "garage"))
	r := withToken(rpc(callBody("garage", `{"door":"left"}`)), callToken)
	r.Header.Del("Mcp-Protocol-Version")
	id, code, msg := rpcRefusal(t, e.serve(r), http.StatusBadRequest)
	assert.EqualValues(t, 3, id)
	assert.EqualValues(t, -32600, code)
	assert.Contains(t, msg, "Mcp-Protocol-Version")
	assert.Zero(t, e.tools.calls.Load())

	// initialize carries its version in its parameters.
	r = withToken(rpc(initializeBody), callToken)
	r.Header.Del("Mcp-Protocol-Version")
	_, rpcErr := rpcResult(t, e.serve(r))
	assert.Nil(t, rpcErr)

	// Notifications get no answer either way.
	r = withToken(rpc(`{"jsonrpc":"2.0","method":"notifications/initialized"}`), callToken)
	r.Header.Del("Mcp-Protocol-Version")
	assert.Equal(t, http.StatusAccepted, e.serve(r).Code)

	// Versions /mcp does not negotiate are refused by the SDK.
	r = withToken(rpc(listToolsBody), callToken)
	r.Header.Set("Mcp-Protocol-Version", "2025-03-26")
	assert.Equal(t, http.StatusBadRequest, e.serve(r).Code)
}

// A request of a method the SDK does not know gets a JSON-RPC error
// (-32601) at every version, like a method the SDK knows but does not
// serve: before 2026-07-28 the SDK would answer it with a plain-text HTTP
// 400, which no JSON-RPC client can read as the answer to its request.
func TestUnknownMethodsAreJSONRPCErrors(t *testing.T) {
	e := newTestEnv(t)
	token := func(n int) string { return e.verifier.add(fmt.Sprintf("method%09d", n)) }
	request := func(version, method string) *http.Request {
		r := rpc(`{"jsonrpc":"2.0","id":5,"method":"` + method + `","params":{}}`)
		r.Header.Set("Mcp-Protocol-Version", version)
		return r
	}
	for i, version := range []string{"2025-06-18", "2025-11-25"} {
		// resources/subscribe: the SDK knows it, PiWorker serves no resources.
		_, code, unserved := rpcRefusal(t, e.serve(withToken(request(version, "resources/subscribe"), token(2*i))), http.StatusOK)
		require.EqualValues(t, -32601, code)
		id, code, msg := rpcRefusal(t, e.serve(withToken(request(version, "flows/list"), token(2*i+1))), http.StatusOK)
		assert.EqualValues(t, 5, id, version)
		assert.EqualValues(t, -32601, code, version)
		assert.Equal(t, strings.ReplaceAll(unserved, "resources/subscribe", "flows/list"), msg, version)
	}
	// 2026-07-28: the SDK answers it (404, -32601).
	_, code, _ := rpcRefusal(t, e.serve(withToken(rpc2026(t, "flows/list", nil), token(9))), http.StatusNotFound)
	assert.EqualValues(t, -32601, code)
	// The name is echoed shortened.
	_, _, msg := rpcRefusal(t, e.serve(withToken(request("2025-06-18", strings.Repeat("x", 100_000)), token(10))), http.StatusOK)
	assert.Less(t, len(msg), 300)
	// A notification gets no answer: HTTP 400 is the transport's refusal.
	r := withToken(rpc(`{"jsonrpc":"2.0","method":"notifications/nothing"}`), token(11))
	assert.Equal(t, http.StatusBadRequest, e.serve(r).Code)
}

// A message that is neither a request (no method, or an empty one) nor a
// response (no result or error) is an invalid request (-32600, HTTP 400,
// with its id) at every version, not a request of an unknown method. A
// response is not answered: the SDK accepts it (202), as the transport
// requires, though the stateless server sends no requests to answer.
func TestMessagesWithoutAMethod(t *testing.T) {
	e := newTestEnv(t)
	n := 0
	serve := func(version, body string) *httptest.ResponseRecorder {
		n++
		r := rpc(body)
		r.Header.Set("Mcp-Protocol-Version", version)
		return e.serve(withToken(r, e.verifier.add(fmt.Sprintf("nomethod%08d", n))))
	}
	for _, version := range ProtocolVersions {
		for _, body := range []string{
			`{"jsonrpc":"2.0","id":5}`,
			`{"jsonrpc":"2.0","id":5,"params":{}}`,
			`{"jsonrpc":"2.0","id":5,"method":"","params":{}}`,
		} {
			id, code, msg := rpcRefusal(t, serve(version, body), http.StatusBadRequest)
			assert.EqualValues(t, 5, id, "%s %s", version, body)
			assert.EqualValues(t, -32600, code, "%s %s", version, body)
			assert.Contains(t, msg, "method", "%s %s", version, body)
		}
		for _, body := range []string{
			`{"jsonrpc":"2.0","id":5,"result":{}}`,
			`{"jsonrpc":"2.0","id":"r1","error":{"code":-32601,"message":"no such method"}}`,
		} {
			w := serve(version, body)
			assert.Equal(t, http.StatusAccepted, w.Code, "%s %s: %s", version, body, w.Body.String())
			assert.Empty(t, w.Body.String(), "%s %s", version, body)
		}
	}
	assert.Zero(t, e.tools.calls.Load())
}

// sdkMethods lists only methods the SDK dispatches: none of them is
// answered as unknown to the SDK, as a request or as a notification.
func TestSDKMethodsAreDispatched(t *testing.T) {
	e := newTestEnv(t)
	n := 0
	for method := range sdkMethods {
		for _, body := range []string{`{"jsonrpc":"2.0","id":5,"method":"` + method + `","params":{}}`, `{"jsonrpc":"2.0","method":"` + method + `","params":{}}`} {
			n++
			r := rpc(body)
			r.Header.Set("Mcp-Protocol-Version", "2025-11-25")
			w := e.serve(withToken(r, e.verifier.add(fmt.Sprintf("sdk%013d", n))))
			assert.NotContains(t, w.Body.String(), "not handled", "%s: %s", method, body)
			assert.NotContains(t, w.Body.String(), "unsupported", "%s: %s", method, body)
		}
	}
	assert.Greater(t, n, 30)
}
