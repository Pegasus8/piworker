package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/api"
	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/events"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/tlsca"
	"github.com/Pegasus8/piworker/internal/webhook"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tlsEnv is a running PiWorker (real router, listeners and certificates) on
// loopback ports.
type tlsEnv struct {
	cfg       config.Config
	src       tlsSource
	servers   *serverSet
	users     *storage.SQLiteUserStore
	flows     *api.FlowsHandler
	manager   *flow.RuntimeManager
	httpsBase string // https://127.0.0.1:<port>
	httpBase  string // http://127.0.0.1:<port>, empty when disabled

	shutdownOnce sync.Once
	shutdownErr  error
}

// shutdown runs the real graceful shutdown once (tests and cleanup share it).
func (e *tlsEnv) shutdown() error {
	e.shutdownOnce.Do(func() { e.shutdownErr = gracefulShutdown(e.servers.all(), e.flows, e.manager, 5*time.Second) })
	return e.shutdownErr
}

func startTLSEnv(t *testing.T, mutate func(*config.Config), beforeServe ...func(*serverSet)) *tlsEnv {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DBPath = filepath.Join(dir, "app.db")
	cfg.Addr = "127.0.0.1:0"
	cfg.HTTPAddr = "127.0.0.1:0"
	if mutate != nil {
		mutate(&cfg)
	}
	require.NoError(t, cfg.Validate())

	store, err := storage.NewSQLiteStore(cfg.DBPath)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	users, err := storage.NewSQLiteUserStoreWithDB(store.DB())
	require.NoError(t, err)
	manager := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())

	src, err := newTLSSource(cfg)
	require.NoError(t, err)
	router, flows := buildRouter(cfg, store, manager, node.DefaultRegistry, users, tlsRoutes(cfg.TLS.Mode, src))

	var tlsConf *tls.Config
	if src != nil {
		tlsConf = src.TLSConfig()
	}
	servers, err := listen(cfg, router, tlsConf)
	require.NoError(t, err)
	for _, f := range beforeServe {
		f(servers)
	}
	errCh := make(chan error, 2)
	servers.serve(errCh)
	env := &tlsEnv{cfg: cfg, src: src, servers: servers, users: users, flows: flows, manager: manager}
	t.Cleanup(func() {
		require.NoError(t, env.shutdown())
		select {
		case err := <-errCh:
			t.Errorf("server error: %v", err)
		default:
		}
	})

	scheme := "http"
	if src != nil {
		scheme = "https"
	}
	env.httpsBase = scheme + "://" + servers.mainLn.Addr().String()
	if servers.redirectLn != nil {
		env.httpBase = "http://" + servers.redirectLn.Addr().String()
	}
	return env
}

func (e *tlsEnv) port(base string) string {
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://"))
	return port
}

// client trusts only the generated CA, speaks HTTP/2 like a browser, keeps
// cookies and does not follow redirects.
func (e *tlsEnv) client(t *testing.T) *http.Client {
	t.Helper()
	pool := x509.NewCertPool()
	if e.src != nil && e.src.CACertPEM() != nil {
		require.True(t, pool.AppendCertsFromPEM(e.src.CACertPEM()))
	}
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	c := &http.Client{
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}, ForceAttemptHTTP2: true},
		Jar:           jar,
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	// The transport may pre-dial a spare connection that never sends a
	// request; Shutdown waits up to 5s for such connections. Cleanups run in
	// reverse order, so this happens before the server shuts down.
	t.Cleanup(c.CloseIdleConnections)
	return c
}

func (e *tlsEnv) post(t *testing.T, c *http.Client, path, origin string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, e.httpsBase+path, bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PiWorker-Request", "1")
	req.Header.Set("Origin", origin)
	resp, err := c.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func get(t *testing.T, c *http.Client, url string) *http.Response {
	t.Helper()
	resp, err := c.Get(url)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func sessionCookieHeader(resp *http.Response) string {
	for _, h := range resp.Header.Values("Set-Cookie") {
		if strings.HasPrefix(h, "piworker_session=") {
			return h
		}
	}
	return ""
}

func TestHTTPSSetupRecoveryLoginAndLiveEvents(t *testing.T) {
	// A short write timeout proves that live streams outlive it over HTTP/2.
	env := startTLSEnv(t, nil, func(s *serverSet) { s.main.WriteTimeout = time.Second })
	c := env.client(t)
	origin := env.httpsBase

	status := get(t, c, env.httpsBase+"/api/auth/status")
	require.Equal(t, http.StatusOK, status.StatusCode)
	assert.Equal(t, "HTTP/2.0", status.Proto, "browsers use HTTP/2 over TLS")
	require.NotNil(t, status.TLS)

	// First-installation setup with a one-use code.
	code, err := env.users.NewAuthCode("setup", "", time.Now())
	require.NoError(t, err)
	resp := env.post(t, c, "/api/auth/setup", origin, map[string]string{"username": "owner", "password": "first-password", "code": code})
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Account recovery.
	code, err = env.users.NewAuthCode("recovery", "owner", time.Now())
	require.NoError(t, err)
	resp = env.post(t, c, "/api/auth/recover", origin, map[string]string{"username": "owner", "password": "second-password", "code": code})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Login: the session cookie is Secure, HttpOnly and SameSite=Strict.
	resp = env.post(t, c, "/api/auth/login", origin, map[string]string{"username": "owner", "password": "second-password"})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	cookie := sessionCookieHeader(resp)
	require.NotEmpty(t, cookie)
	for _, attr := range []string{"Secure", "HttpOnly", "SameSite=Strict", "Path=/api"} {
		assert.Contains(t, cookie, attr)
	}
	require.Equal(t, http.StatusOK, get(t, c, env.httpsBase+"/api/auth/session").StatusCode)

	// Origin checks: a downgraded (http) or foreign origin cannot write.
	for _, bad := range []string{"http://" + strings.TrimPrefix(env.httpsBase, "https://"), "https://evil.example"} {
		resp = env.post(t, c, "/api/flows", bad, map[string]string{"name": "x"})
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, bad)
	}
	resp = env.post(t, c, "/api/flows", origin, map[string]string{"name": "ok"})
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	// TLS status is authenticated.
	assert.Equal(t, http.StatusUnauthorized, get(t, env.client(t), env.httpsBase+"/api/tls").StatusCode)
	var tlsBody struct {
		Data api.TLSInfo `json:"data"`
	}
	resp = get(t, c, env.httpsBase+"/api/tls")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tlsBody))
	assert.Equal(t, "auto", tlsBody.Data.Mode)
	assert.Equal(t, env.src.Status().CAFingerprint, tlsBody.Data.CAFingerprint)
	assert.Contains(t, tlsBody.Data.SANs, "127.0.0.1")
	require.NotNil(t, tlsBody.Data.NotAfter)

	// Live events (SSE) stream over HTTPS.
	streamResp := get(t, c, env.httpsBase+"/api/flows/tls-sse-flow/events")
	require.Equal(t, http.StatusOK, streamResp.StatusCode)
	assert.Equal(t, "text/event-stream", streamResp.Header.Get("Content-Type"))
	require.Eventually(t, func() bool { return events.DefaultHub.HasSubscribers("tls-sse-flow") }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(1500 * time.Millisecond) // past the write timeout
	events.DefaultHub.Publish(events.Event{FlowID: "tls-sse-flow", NodeID: "node-over-https", Phase: "success"})

	lines := make(chan string, 1)
	go func() {
		r := bufio.NewReader(streamResp.Body)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				close(lines)
				return
			}
			if strings.HasPrefix(line, "data: ") {
				lines <- line
				return
			}
		}
	}()
	select {
	case line, ok := <-lines:
		require.True(t, ok, "the stream ended before delivering the event")
		assert.Contains(t, line, "node-over-https")
	case <-time.After(5 * time.Second):
		t.Fatal("no live event received over HTTPS")
	}
}

func TestSessionCookieSecureFollowsTLSMode(t *testing.T) {
	for mode, secure := range map[string]bool{config.TLSModeAuto: true, config.TLSModeOff: false} {
		t.Run(mode, func(t *testing.T) {
			app := newTestApp(t, true)
			cfg := config.Default()
			cfg.TLS.Mode = mode
			router, _ := buildRouter(cfg, app.store, app.manager, node.DefaultRegistry, app.users)
			// A plain-HTTP request: the flag comes from the configuration, not r.TLS.
			w := testApp{handler: router}.do("POST", "/api/auth/login", "application/json", `{"username":"admin","password":"test-password"}`, editorHeaders)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.NotEmpty(t, w.Result().Cookies())
			assert.Equal(t, secure, w.Result().Cookies()[0].Secure)
		})
	}
}

func TestHTTPListenerRedirectsToHTTPS(t *testing.T) {
	env := startTLSEnv(t, nil)
	c := env.client(t)
	httpsPort := env.port(env.httpsBase)

	cases := map[string]struct{ host, path, want string }{
		"path and query":  {"", "/editor/abc?tab=debug", "https://127.0.0.1:" + httpsPort + "/editor/abc?tab=debug"},
		"named host":      {"raspberrypi.local:8080", "/", "https://raspberrypi.local:" + httpsPort + "/"},
		"ipv6 host":       {"[fd00::10]:8080", "/login", "https://[fd00::10]:" + httpsPort + "/login"},
		"api":             {"", "/api/auth/status", "https://127.0.0.1:" + httpsPort + "/api/auth/status"},
		"webhook escape":  {"", "/webhooks/../api/flows", "https://127.0.0.1:" + httpsPort + "/webhooks/../api/flows"},
		"ca prefix trick": {"", "/piworker-ca.crt/../api/tls", "https://127.0.0.1:" + httpsPort + "/piworker-ca.crt/../api/tls"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, env.httpBase+tc.path, nil)
			require.NoError(t, err) // Go's client sends dot segments unchanged
			if tc.host != "" {
				req.Host = tc.host
			}
			resp, err := c.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusPermanentRedirect, resp.StatusCode)
			assert.Equal(t, tc.want, resp.Header.Get("Location"))
		})
	}

	// Credentials are never accepted over plain HTTP: the POST is redirected
	// (308 keeps the method, so the browser repeats it over HTTPS).
	req, err := http.NewRequest(http.MethodPost, env.httpBase+"/api/auth/login", strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("X-PiWorker-Request", "1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusPermanentRedirect, resp.StatusCode)
	assert.Empty(t, resp.Header.Values("Set-Cookie"))
}

func TestCACertificateOnBothListeners(t *testing.T) {
	env := startTLSEnv(t, nil)
	c := env.client(t)
	for _, base := range []string{env.httpsBase, env.httpBase} {
		resp := get(t, c, base+api.CACertPath)
		require.Equal(t, http.StatusOK, resp.StatusCode, base)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, env.src.CACertPEM(), body)
		block, _ := pem.Decode(body)
		require.NotNil(t, block)
		assert.Equal(t, env.src.Status().CAFingerprint, tlsca.Fingerprint(block.Bytes))
	}
}

func TestWebhooksOverHTTP(t *testing.T) {
	got := make(chan any, 4)
	require.NoError(t, webhook.DefaultHub.Register("tls-http-hook", func(r webhook.Request) webhook.Result {
		got <- r.Body
		return webhook.Accepted
	}))
	t.Cleanup(func() { webhook.DefaultHub.Unregister("tls-http-hook") })

	post := func(c *http.Client, url string) *http.Response {
		resp, err := c.Post(url, "text/plain", strings.NewReader("ping"))
		require.NoError(t, err)
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

	env := startTLSEnv(t, nil)
	c := env.client(t)
	require.Equal(t, http.StatusOK, post(c, env.httpBase+"/webhooks/tls-http-hook").StatusCode)
	assert.Equal(t, "ping", <-got)
	require.Equal(t, http.StatusOK, post(c, env.httpsBase+"/webhooks/tls-http-hook").StatusCode, "webhooks also work over HTTPS")
	assert.Equal(t, "ping", <-got)

	strict := startTLSEnv(t, func(cfg *config.Config) { cfg.WebhooksHTTP = false })
	resp := post(strict.client(t), strict.httpBase+"/webhooks/tls-http-hook")
	assert.Equal(t, http.StatusPermanentRedirect, resp.StatusCode)
	assert.Empty(t, got, "with webhooks_http = false, webhooks are only served over HTTPS")
}

func TestTLSOffServesPlainHTTPOnAddr(t *testing.T) {
	env := startTLSEnv(t, func(cfg *config.Config) { cfg.TLS.Mode = config.TLSModeOff })
	assert.Nil(t, env.src)
	assert.Nil(t, env.servers.redirect, "http_addr is unused when TLS is off")
	assert.Len(t, env.servers.all(), 1)
	c := env.client(t)
	require.True(t, strings.HasPrefix(env.httpsBase, "http://"))
	assert.Equal(t, http.StatusOK, get(t, c, env.httpsBase+"/api/auth/status").StatusCode)
	assert.Equal(t, http.StatusNotFound, get(t, c, env.httpsBase+api.CACertPath).StatusCode)
}

func TestEmptyHTTPAddrDisablesRedirectListener(t *testing.T) {
	env := startTLSEnv(t, func(cfg *config.Config) { cfg.HTTPAddr = "" })
	assert.Nil(t, env.servers.redirect)
	assert.Len(t, env.servers.all(), 1)
	assert.Equal(t, http.StatusOK, get(t, env.client(t), env.httpsBase+"/api/auth/status").StatusCode)
}

func TestCustomCertificateMode(t *testing.T) {
	// An administrator's own certificate, here issued by a separate CA.
	ownCA := filepath.Join(t.TempDir(), "own-ca")
	other, err := tlsca.NewManager(tlsca.Options{Dir: ownCA})
	require.NoError(t, err)
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	for from, to := range map[string]string{tlsca.ServerCertFile: certFile, tlsca.ServerKeyFile: keyFile} {
		data, err := os.ReadFile(filepath.Join(ownCA, from))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(to, data, 0o600))
	}

	env := startTLSEnv(t, func(cfg *config.Config) {
		cfg.TLS = config.TLSConfig{Mode: config.TLSModeCustom, CertFile: certFile, KeyFile: keyFile}
	})
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(other.CACertPEM()))
	c := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}, Timeout: 10 * time.Second}
	t.Cleanup(c.CloseIdleConnections)
	assert.Equal(t, http.StatusOK, get(t, c, env.httpsBase+"/api/auth/status").StatusCode)
	assert.Equal(t, http.StatusNotFound, get(t, c, env.httpsBase+api.CACertPath).StatusCode, "no local CA in custom mode")
	_, statErr := os.Stat(env.cfg.TLSDir())
	assert.True(t, os.IsNotExist(statErr), "custom mode does not create a local CA")
}

func TestListenFailsCleanlyWhenHTTPPortIsTaken(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer busy.Close()
	cfg := config.Default()
	cfg.DBPath = filepath.Join(t.TempDir(), "app.db")
	cfg.Addr = "127.0.0.1:0"
	cfg.HTTPAddr = busy.Addr().String()
	src, err := newTLSSource(cfg)
	require.NoError(t, err)
	_, err = listen(cfg, http.NotFoundHandler(), src.TLSConfig())
	require.Error(t, err)
}

func TestRegenerateCACommand(t *testing.T) {
	cfg := config.Default()
	cfg.DBPath = filepath.Join(t.TempDir(), "app.db")
	src, err := newTLSSource(cfg)
	require.NoError(t, err)
	before := src.Status().CAFingerprint

	fingerprint, err := regenerateCA(cfg)
	require.NoError(t, err)
	assert.NotEqual(t, before, fingerprint)
	src, err = newTLSSource(cfg)
	require.NoError(t, err)
	assert.Equal(t, fingerprint, src.Status().CAFingerprint)
}

func TestPublicURL(t *testing.T) {
	assert.Equal(t, "https://raspberrypi.local:8443", publicURL("https", "raspberrypi", "8443"))
	assert.Equal(t, "https://pi.local", publicURL("https", "pi.example.lan", "443"))
	assert.Equal(t, "http://localhost:8080", publicURL("http", "", "8080"))
	assert.Equal(t, "https://pi.local:8443", publicURL("https", "PI.local", "8443"))
}

func TestGracefulShutdownClosesBothListeners(t *testing.T) {
	env := startTLSEnv(t, nil)
	require.Len(t, env.servers.all(), 2)
	require.NoError(t, env.shutdown())
	// Dialing a closed loopback port can self-connect, so inspect the
	// listeners: Accept on a closed listener fails with net.ErrClosed.
	for _, ln := range []net.Listener{env.servers.mainLn, env.servers.redirectLn} {
		err := ln.(*net.TCPListener).SetDeadline(time.Now().Add(100 * time.Millisecond))
		if err == nil {
			_, err = ln.Accept()
		}
		assert.ErrorIs(t, err, net.ErrClosed, "%s is still open after shutdown", ln.Addr())
	}
}

func TestRedirectOmitsDefaultHTTPSPort(t *testing.T) {
	h := redirectHandler("443", http.NotFoundHandler(), true)
	for host, want := range map[string]string{"pi.local": "https://pi.local/x?a=1", "[fd00::1]:80": "https://[fd00::1]/x?a=1"} {
		r, err := http.NewRequest(http.MethodGet, "http://"+host+"/x?a=1", nil)
		require.NoError(t, err)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		assert.Equal(t, http.StatusPermanentRedirect, w.Code)
		assert.Equal(t, want, w.Header().Get("Location"))
	}
}
