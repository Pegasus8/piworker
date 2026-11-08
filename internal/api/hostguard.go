package api

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/webhook"
	"github.com/rs/zerolog/log"
)

// DefaultHostCacheTTL is how long the hostname and interface addresses are
// cached before being read again.
const DefaultHostCacheTTL = 30 * time.Second

// HostGuardOptions configures a HostGuard. Zero values use the real machine.
type HostGuardOptions struct {
	// AllowedHosts are extra accepted names (allowed_hosts); a port and a
	// trailing dot are ignored, and matching is case-insensitive.
	AllowedHosts []string
	// Hostname returns the machine's name (default os.Hostname).
	Hostname func() (string, error)
	// Interfaces returns the machine's addresses (default: every interface).
	Interfaces func() ([]net.IP, error)
	// Now is the clock used for the cache (default time.Now).
	Now func() time.Time
	// CacheTTL is how long Hostname/Interfaces results are reused
	// (default DefaultHostCacheTTL).
	CacheTTL time.Duration
}

// HostGuard rejects requests whose Host header is not a name of this server.
// It defeats DNS rebinding: a malicious page whose DNS name is re-pointed at
// PiWorker still sends its own name ("Host: attacker.example"), so it cannot
// use the browser to reach the API, the MCP endpoint or the web UI.
//
// Accepted: localhost, the hostname, <hostname>.local (also for the first
// label of a dotted hostname), the current interface addresses, the configured
// AllowedHosts, and any IP literal (rebinding needs a DNS name, and a page
// served from an IP address is same-origin with it anyway).
type HostGuard struct {
	extra      map[string]struct{}
	hostname   func() (string, error)
	interfaces func() ([]net.IP, error)
	now        func() time.Time
	ttl        time.Duration

	mu       sync.Mutex
	cached   map[string]struct{}
	cachedAt time.Time
}

// NewHostGuard builds a HostGuard. Nothing is read from the system until the
// first request needs it.
func NewHostGuard(opts HostGuardOptions) *HostGuard {
	g := &HostGuard{
		extra:      make(map[string]struct{}, len(opts.AllowedHosts)),
		hostname:   opts.Hostname,
		interfaces: opts.Interfaces,
		now:        opts.Now,
		ttl:        opts.CacheTTL,
	}
	for _, h := range opts.AllowedHosts {
		if n := normalizeHost(h); n != "" {
			g.extra[n] = struct{}{}
		}
	}
	if g.hostname == nil {
		g.hostname = os.Hostname
	}
	if g.interfaces == nil {
		g.interfaces = interfaceIPs
	}
	if g.now == nil {
		g.now = time.Now
	}
	if g.ttl <= 0 {
		g.ttl = DefaultHostCacheTTL
	}
	return g
}

// Allowed reports whether host (a Host header value, with or without port)
// names this server.
func (g *HostGuard) Allowed(host string) bool {
	h := normalizeHost(host)
	if h == "" {
		return false
	}
	if _, err := netip.ParseAddr(h); err == nil {
		return true
	}
	if _, ok := g.extra[h]; ok {
		return true
	}
	_, ok := g.defaults()[h]
	return ok
}

// Names returns every accepted name (besides arbitrary IP literals), sorted;
// useful for logging what the guard accepts.
func (g *HostGuard) Names() []string {
	seen := make(map[string]struct{})
	for h := range g.extra {
		seen[h] = struct{}{}
	}
	for h := range g.defaults() {
		seen[h] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for h := range seen {
		names = append(names, h)
	}
	sort.Strings(names)
	return names
}

// Middleware rejects requests with an unknown Host with a 403 JSON error,
// except on the webhook endpoints and the CA certificate download, which
// external devices reach under any name.
func (g *HostGuard) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hostGuardExempt(r.URL.Path) || g.Allowed(r.Host) {
			next.ServeHTTP(w, r)
			return
		}
		shown := r.Host
		if len(shown) > 100 {
			shown = shown[:100] + "…"
		}
		log.Debug().Str("host", shown).Str("path", r.URL.Path).Msg("Rejected request for an unknown Host")
		writeError(w, http.StatusForbidden, fmt.Sprintf(
			"Host %q is not allowed. To reach PiWorker by this name, add it to allowed_hosts (PIWORKER_ALLOWED_HOSTS or -allowed-hosts).", shown))
	})
}

// hostGuardExempt reports whether a path is served under any Host name. The
// path is cleaned first so "/webhooks/../api/…" is not exempt.
func hostGuardExempt(p string) bool {
	clean := path.Clean("/" + p)
	return strings.HasPrefix(clean, webhook.PathPrefix) || clean == CACertPath
}

// defaults returns the cached machine names, re-reading them after the TTL.
func (g *HostGuard) defaults() map[string]struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if g.cached != nil && now.Sub(g.cachedAt) < g.ttl {
		return g.cached
	}
	set := map[string]struct{}{"localhost": {}}
	if name, err := g.hostname(); err == nil {
		if h := normalizeHost(name); h != "" {
			set[h] = struct{}{}
			set[h+".local"] = struct{}{}
			if short, _, dotted := strings.Cut(h, "."); dotted && short != "" {
				set[short] = struct{}{}
				set[short+".local"] = struct{}{}
			}
		}
	}
	if ips, err := g.interfaces(); err == nil {
		for _, ip := range ips {
			if ip != nil {
				set[strings.ToLower(ip.String())] = struct{}{}
			}
		}
	}
	g.cached, g.cachedAt = set, now
	return set
}

// normalizeHost lower-cases a host name and drops a port, IPv6 brackets and a
// trailing dot. It returns "" for an empty name.
func normalizeHost(h string) string {
	h = strings.TrimSpace(h)
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	} else if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}
	return strings.ToLower(strings.TrimSuffix(h, "."))
}

// interfaceIPs lists the addresses of every network interface.
func interfaceIPs() ([]net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		switch v := a.(type) {
		case *net.IPNet:
			ips = append(ips, v.IP)
		case *net.IPAddr:
			ips = append(ips, v.IP)
		}
	}
	return ips, nil
}
