package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHostEnv supplies a controllable hostname, interface list and clock.
type fakeHostEnv struct {
	hostname   atomic.Value // string
	hostCalls  atomic.Int32
	ifaceCalls atomic.Int32
	now        atomic.Value // time.Time
}

func newFakeHostEnv(hostname string) *fakeHostEnv {
	e := &fakeHostEnv{}
	e.hostname.Store(hostname)
	e.now.Store(time.Unix(1_700_000_000, 0))
	return e
}

func (e *fakeHostEnv) options(extra ...string) HostGuardOptions {
	return HostGuardOptions{
		AllowedHosts: extra,
		Hostname: func() (string, error) {
			e.hostCalls.Add(1)
			return e.hostname.Load().(string), nil
		},
		Interfaces: func() ([]net.IP, error) {
			e.ifaceCalls.Add(1)
			return []net.IP{net.ParseIP("192.168.1.50"), net.ParseIP("fe80::1")}, nil
		},
		Now:      func() time.Time { return e.now.Load().(time.Time) },
		CacheTTL: 30 * time.Second,
	}
}

func (e *fakeHostEnv) advance(d time.Duration) {
	e.now.Store(e.now.Load().(time.Time).Add(d))
}

// guarded serves 200 "ok" behind the guard.
func guarded(g *HostGuard) http.Handler {
	return g.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
}

func requestWithHost(method, path, host string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.Host = host
	return r
}

func TestHostGuardScenarios(t *testing.T) {
	env := newFakeHostEnv("mypi")
	h := guarded(NewHostGuard(env.options("backend")))

	for _, tc := range []struct {
		host string
		want int
	}{
		{"backend:8080", http.StatusOK},      // configured (dev compose: the Vite proxy's target)
		{"192.168.1.50:8080", http.StatusOK}, // LAN IP
		{"localhost:8080", http.StatusOK},
		{"attacker.example", http.StatusForbidden}, // DNS rebinding
		{"attacker.example:8080", http.StatusForbidden},
	} {
		t.Run(tc.host, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, requestWithHost("GET", "/api/flows", tc.host))
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestHostGuardRejectsWithJSON(t *testing.T) {
	h := guarded(NewHostGuard(newFakeHostEnv("mypi").options()))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, requestWithHost("POST", "/api/flows", "attacker.example"))

	require.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	var body APIResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.False(t, body.Success)
	assert.Contains(t, body.Error, "allowed_hosts")
	assert.Contains(t, body.Error, "attacker.example")
}

func TestHostGuardDefaultNames(t *testing.T) {
	env := newFakeHostEnv("MyPi")
	g := NewHostGuard(env.options())
	for _, host := range []string{
		"localhost", "LOCALHOST:8080", "localhost.", "mypi", "mypi:8443", "MyPi.local", "mypi.local.:80",
		"127.0.0.1", "[::1]:8080", "[::1]", "10.0.0.7:1", "[2001:db8::5]:443", "203.0.113.9",
	} {
		assert.Truef(t, g.Allowed(host), "%q must be allowed", host)
	}
	for _, host := range []string{
		"", ":8080", "attacker.example", "mypi.attacker.example", "localhost.attacker.example", "evil-mypi", "mypi.localx",
	} {
		assert.Falsef(t, g.Allowed(host), "%q must be rejected", host)
	}
}

func TestHostGuardFQDNHostname(t *testing.T) {
	g := NewHostGuard(newFakeHostEnv("pi.example.com").options())
	for _, host := range []string{"pi.example.com", "pi", "pi.local"} {
		assert.Truef(t, g.Allowed(host), "%q", host)
	}
	assert.False(t, g.Allowed("example.com"))
}

func TestHostGuardConfiguredHostsAreNormalised(t *testing.T) {
	g := NewHostGuard(newFakeHostEnv("mypi").options(" Pi.Example.COM:8443 ", "proxy.lan.", ""))
	assert.True(t, g.Allowed("pi.example.com"))
	assert.True(t, g.Allowed("PI.EXAMPLE.COM:443"))
	assert.True(t, g.Allowed("proxy.lan:8080"))
	assert.False(t, g.Allowed("example.com"))
}

// Webhooks are called by external devices under any name, and the CA
// certificate must be downloadable before a name is configured.
func TestHostGuardExemptPaths(t *testing.T) {
	h := guarded(NewHostGuard(newFakeHostEnv("mypi").options()))
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/webhooks/github-push", http.StatusOK},
		{"/piworker-ca.crt", http.StatusOK},
		{"/api/flows", http.StatusForbidden},
		{"/mcp", http.StatusForbidden},
		{"/", http.StatusForbidden},
		{"/index.html", http.StatusForbidden},
		{"/webhooksx", http.StatusForbidden},
		{"/webhooks/../api/flows", http.StatusForbidden},
		{"/piworker-ca.crt.bak", http.StatusForbidden},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, requestWithHost("GET", tc.path, "attacker.example"))
		assert.Equalf(t, tc.want, w.Code, "path %s", tc.path)
	}
}

// The hostname and interface IPs are read lazily and cached briefly, so a
// renamed host or a new address is picked up without a restart.
func TestHostGuardRereadsNamesLazily(t *testing.T) {
	env := newFakeHostEnv("old")
	g := NewHostGuard(env.options())
	assert.Zero(t, env.hostCalls.Load(), "nothing is read at construction")
	assert.Zero(t, env.ifaceCalls.Load())

	assert.True(t, g.Allowed("old.local"))
	assert.False(t, g.Allowed("new.local"))
	assert.Equal(t, int32(1), env.hostCalls.Load(), "cached within the TTL")
	assert.Equal(t, int32(1), env.ifaceCalls.Load())

	env.hostname.Store("new")
	env.advance(10 * time.Second)
	assert.False(t, g.Allowed("new.local"), "still cached")

	env.advance(30 * time.Second)
	assert.True(t, g.Allowed("new.local"))
	assert.False(t, g.Allowed("old.local"))
	assert.Equal(t, int32(2), env.hostCalls.Load())
}

func TestHostGuardSurvivesLookupErrors(t *testing.T) {
	opts := HostGuardOptions{
		AllowedHosts: []string{"backend"},
		Hostname:     func() (string, error) { return "", errors.New("no hostname") },
		Interfaces:   func() ([]net.IP, error) { return nil, errors.New("no interfaces") },
	}
	g := NewHostGuard(opts)
	assert.True(t, g.Allowed("localhost"))
	assert.True(t, g.Allowed("backend"))
	assert.True(t, g.Allowed("192.168.1.50"))
	assert.False(t, g.Allowed(".local"))
	assert.False(t, g.Allowed("attacker.example"))
}

func TestHostGuardNamesListsDefaults(t *testing.T) {
	g := NewHostGuard(newFakeHostEnv("mypi").options("backend"))
	names := g.Names()
	for _, want := range []string{"localhost", "mypi", "mypi.local", "backend", "192.168.1.50", "fe80::1"} {
		assert.Contains(t, names, want)
	}
}

func TestHostGuardDefaultOptionsUseTheRealMachine(t *testing.T) {
	g := NewHostGuard(HostGuardOptions{})
	assert.True(t, g.Allowed("localhost:8080"))
	assert.False(t, g.Allowed("attacker.example"))
}
