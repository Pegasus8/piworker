package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Login throttling. The goal is to slow password guessing without handing an
// attacker an easy way to lock the single administrator out:
//
//   - Per peer IP: at most ipMaxAttempts unauthenticated attempts per window.
//     The table is bounded; when full, expired and then the oldest entries are
//     evicted, so new clients are never refused just because it is full.
//   - Global: once globalFailureBudget failures happen in a window, only IPs
//     that themselves failed recently are refused; a clean IP is unaffected.
//   - Per username: after usernameFreeFailures consecutive failures, further
//     attempts back off exponentially (1s, 2s, 4s ... usernameMaxBackoff). A
//     (username, IP) pair that has signed in successfully before is trusted and
//     skips this backoff, so hammering the admin's name from elsewhere cannot
//     lock out the admin's own device.
//
// Peers are identified by the socket address, never by forwarded headers.
const (
	ipWindow              = time.Minute
	ipMaxAttempts         = 10
	maxTrackedIPs         = 2048
	globalFailureBudget   = 100
	maxTrackedUsers       = 2048
	usernameFreeFailures  = 10
	usernameMaxBackoff    = 5 * time.Minute
	usernameFailureMemory = 15 * time.Minute
	maxTrustedPairs       = 256
	trustedPairLifetime   = 30 * 24 * time.Hour
)

type ipAttempts struct {
	attempts int // reserved attempts in the current window (refunded on success)
	failures int
	until    time.Time
}

type userFailures struct {
	failures    int
	last        time.Time
	lockedUntil time.Time
}

type loginLimiter struct {
	mu             sync.Mutex
	now            func() time.Time
	ips            map[string]*ipAttempts
	users          map[string]*userFailures
	trusted        map[string]time.Time
	globalFailures int
	globalUntil    time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		now:     time.Now,
		ips:     make(map[string]*ipAttempts),
		users:   make(map[string]*userFailures),
		trusted: make(map[string]time.Time),
	}
}

func peerIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func trustKey(user, ip string) string { return user + "\x00" + ip }

// allow reserves an attempt for ip against user. When refused it returns how
// long the caller should wait.
func (l *loginLimiter) allow(ip, user string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if !now.Before(l.globalUntil) {
		l.globalFailures = 0
		l.globalUntil = now.Add(ipWindow)
		l.pruneLocked(now)
	}
	s := l.ips[ip]
	if s != nil && !now.Before(s.until) {
		*s = ipAttempts{until: now.Add(ipWindow)}
	}
	if s != nil && s.attempts >= ipMaxAttempts {
		return false, s.until.Sub(now)
	}
	if s != nil && s.failures > 0 && l.globalFailures >= globalFailureBudget {
		return false, l.globalUntil.Sub(now)
	}
	if exp, ok := l.trusted[trustKey(user, ip)]; !ok || !now.Before(exp) {
		if u := l.users[user]; u != nil && now.Before(u.lockedUntil) {
			return false, u.lockedUntil.Sub(now)
		}
	}
	if s == nil {
		if len(l.ips) >= maxTrackedIPs {
			evictOldest(l.ips, func(v *ipAttempts) time.Time { return v.until })
		}
		s = &ipAttempts{until: now.Add(ipWindow)}
		l.ips[ip] = s
	}
	s.attempts++
	return true, 0
}

// failure records a rejected credential for ip and user.
func (l *loginLimiter) failure(ip, user string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if s := l.ips[ip]; s != nil {
		s.failures++
	}
	l.globalFailures++
	u := l.users[user]
	if u == nil || now.Sub(u.last) > usernameFailureMemory {
		if u == nil && len(l.users) >= maxTrackedUsers {
			evictOldest(l.users, func(v *userFailures) time.Time { return v.last })
		}
		u = &userFailures{}
		l.users[user] = u
	}
	u.failures++
	u.last = now
	if n := u.failures - usernameFreeFailures; n >= 0 {
		backoff := usernameMaxBackoff
		if n < 20 {
			backoff = min(time.Second<<n, usernameMaxBackoff)
		}
		u.lockedUntil = now.Add(backoff)
	}
}

// success refunds the reserved attempt, clears the username's failures and
// trusts this (user, ip) pair for later backoff decisions.
func (l *loginLimiter) success(ip, user string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if s := l.ips[ip]; s != nil && s.attempts > 0 {
		s.attempts--
	}
	delete(l.users, user)
	key := trustKey(user, ip)
	if _, ok := l.trusted[key]; !ok && len(l.trusted) >= maxTrustedPairs {
		evictOldest(l.trusted, func(v time.Time) time.Time { return v })
	}
	l.trusted[key] = now.Add(trustedPairLifetime)
}

func (l *loginLimiter) pruneLocked(now time.Time) {
	for k, v := range l.ips {
		if !now.Before(v.until) {
			delete(l.ips, k)
		}
	}
	for k, v := range l.users {
		if !now.Before(v.lockedUntil) && now.Sub(v.last) > usernameFailureMemory {
			delete(l.users, k)
		}
	}
	for k, exp := range l.trusted {
		if !now.Before(exp) {
			delete(l.trusted, k)
		}
	}
}

func evictOldest[V any](m map[string]V, at func(V) time.Time) {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, v := range m {
		if t := at(v); first || t.Before(oldest) {
			oldestKey, oldest, first = k, t, false
		}
	}
	if !first {
		delete(m, oldestKey)
	}
}
