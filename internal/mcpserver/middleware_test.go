package mcpserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	// A batch is checked request by request.
	batch := `[` + listToolsBody + `,` + callBody("garage", `{"door":"left"}`) + `]`
	requireJSONError(t, e.serve(withToken(rpc(batch), readToken)), http.StatusForbidden)
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
