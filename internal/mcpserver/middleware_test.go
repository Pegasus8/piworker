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

	"github.com/Pegasus8/piworker/internal/storage"
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
// limiter): after 10 a minute the client's lookups of made-up tokens get 429
// until the window frees, and the verifier is not consulted.
func TestAuthFailureLimiter(t *testing.T) {
	e := newTestEnv(t)
	for i := range 10 {
		requireJSONError(t, e.serve(withToken(rpc(listToolsBody), madeUpToken(i))), http.StatusUnauthorized)
	}
	before := e.verifier.calls.Load()
	w := e.serve(withToken(rpc(listToolsBody), badToken))
	requireJSONError(t, w, http.StatusTooManyRequests)
	assert.Equal(t, "6", w.Header().Get("Retry-After"))
	requireJSONError(t, e.serve(withToken(rpc(listToolsBody), "pwk_malformed")), http.StatusTooManyRequests)
	assert.Equal(t, before, e.verifier.calls.Load(), "blocked clients never reach the token store")

	other := withToken(rpc(listToolsBody), badToken)
	other.RemoteAddr = "192.168.1.99:1"
	requireJSONError(t, e.serve(other), http.StatusUnauthorized) // "other clients are not affected"

	e.clock.Advance(6 * time.Second)
	requireJSONError(t, e.serve(withToken(rpc(listToolsBody), badToken)), http.StatusUnauthorized)
	_, rpcErr := rpcResult(t, e.serve(withToken(rpc(listToolsBody), callToken)))
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
	_, rpcErr = rpcResult(t, e.serve(from(withToken(rpc(listToolsBody), otherToken), agent)))
	assert.Nil(t, rpcErr, "an uncached real token is looked up all the same")
}

// What a client sends from an address shared with agents (every request
// through a local reverse proxy comes from 127.0.0.1; IPv6 clients share a
// /64) never holds back the lookups of real tokens: their ids have a failure
// budget of their own per address. After a restart, a revocation or a grant
// change empties the verifier's cache, and new tokens were never in it, so
// agents need a lookup then. Made-up tokens keep sharing the address's
// budget, so they cost at most 10 lookups a minute.
func TestAuthFailureLimiterNeverLocksOutRealTokens(t *testing.T) {
	e := newTestEnv(t, func(o *Options) { o.TLS = false })
	proxied := func(r *http.Request) *http.Request {
		r.RemoteAddr = "127.0.0.1:40000"
		r.Host = "piworker.local"
		return r
	}
	// The attacker spends the address's budget, then what refills.
	limit := func(n int) {
		for i := range n {
			requireJSONError(t, e.serve(proxied(withToken(rpc(listToolsBody), madeUpToken(i)))), http.StatusUnauthorized)
		}
		requireJSONError(t, e.serve(proxied(withToken(rpc(listToolsBody), badToken))), http.StatusTooManyRequests)
	}
	limit(10)
	for _, raw := range []string{callToken, otherToken, e.verifier.add("newnewnewnewnewn")} {
		_, rpcErr := rpcResult(t, e.serve(proxied(withToken(rpc(listToolsBody), raw))))
		assert.Nil(t, rpcErr, "a real token never verified before is served")
	}
	e.verifier.forget() // an epoch change
	for range 20 {
		e.clock.Advance(6 * time.Second)
		limit(1) // the attacker keeps the address limited
		_, rpcErr := rpcResult(t, e.serve(proxied(withToken(rpc(listToolsBody), callToken))))
		require.Nil(t, rpcErr, "served after the cache was cleared")
		e.verifier.forget()
	}
	calls := e.verifier.calls.Load()
	requireJSONError(t, e.serve(proxied(withToken(rpc(listToolsBody), madeUpToken(99)))), http.StatusTooManyRequests)
	assert.Equal(t, calls, e.verifier.calls.Load(), "made-up tokens are still not looked up")
}

// Wrong secrets for a real token's id are limited for that id and address:
// they hold back neither other tokens from that address nor that token from
// other addresses.
func TestAuthFailuresOfARealIDAreLimitedForThatID(t *testing.T) {
	e := newTestEnv(t)
	id, _, ok := storage.ParseAPIToken(callToken)
	require.True(t, ok)
	wrong := "pwk_" + id + "_" + strings.Repeat("x", 52)
	for range 10 {
		requireJSONError(t, e.serve(withToken(rpc(listToolsBody), wrong)), http.StatusUnauthorized)
	}
	requireJSONError(t, e.serve(withToken(rpc(listToolsBody), wrong)), http.StatusTooManyRequests)
	// An attacker who knows the id (shown in the editor and the logs) can
	// hold back that token's lookups from the address it sends from.
	requireJSONError(t, e.serve(withToken(rpc(listToolsBody), callToken)), http.StatusTooManyRequests)

	_, rpcErr := rpcResult(t, e.serve(withToken(rpc(listToolsBody), otherToken)))
	assert.Nil(t, rpcErr, "other tokens from the address")
	elsewhere := withToken(rpc(listToolsBody), callToken)
	elsewhere.RemoteAddr = "192.168.1.99:1"
	_, rpcErr = rpcResult(t, e.serve(elsewhere))
	assert.Nil(t, rpcErr, "the token from another address")
	requireJSONError(t, e.serve(withToken(rpc(listToolsBody), madeUpToken(1))), http.StatusUnauthorized)
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
