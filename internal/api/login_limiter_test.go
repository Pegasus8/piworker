package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestLimiter() (*loginLimiter, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := newLoginLimiter()
	l.now = clock.now
	return l, clock
}

func TestLimiterAttackerIPDoesNotLockOutOtherIPs(t *testing.T) {
	l, _ := newTestLimiter()
	for i := 0; i < 50; i++ {
		if ok, _ := l.allow("10.0.0.66", "nobody"); ok {
			l.failure("10.0.0.66", "nobody")
		}
	}
	ok, _ := l.allow("10.0.0.66", "nobody")
	require.False(t, ok, "the attacking IP is limited")
	ok, _ = l.allow("10.0.0.2", "admin")
	require.True(t, ok, "an unrelated IP with no failures must not be blocked")
}

func TestLimiterEvictsInsteadOfRefusingWhenFull(t *testing.T) {
	l, _ := newTestLimiter()
	for i := 0; i < maxTrackedIPs+100; i++ {
		ip := fmt.Sprintf("10.%d.%d.%d", i>>16&255, i>>8&255, i&255)
		ok, _ := l.allow(ip, fmt.Sprintf("u%d", i))
		require.True(t, ok)
		l.failure(ip, fmt.Sprintf("u%d", i))
	}
	require.LessOrEqual(t, len(l.ips), maxTrackedIPs)
	require.LessOrEqual(t, len(l.users), maxTrackedUsers)
	ok, _ := l.allow("192.168.1.10", "admin")
	require.True(t, ok, "a full table must evict old entries, not refuse new clients")
}

func TestLimiterGlobalCapOnlyAffectsFailingIPs(t *testing.T) {
	l, _ := newTestLimiter()
	for i := 0; i < globalFailureBudget+50; i++ {
		ip := fmt.Sprintf("10.1.%d.%d", i>>8&255, i&255)
		if ok, _ := l.allow(ip, "x"+ip); ok {
			l.failure(ip, "x"+ip)
		}
	}
	// A previously failing IP is now refused while the global budget is spent...
	ok, _ := l.allow("10.1.0.1", "y")
	require.False(t, ok)
	// ...but the admin's clean IP is not.
	ok, _ = l.allow("192.168.1.10", "admin")
	require.True(t, ok)
}

func TestLimiterPerUsernameExponentialBackoff(t *testing.T) {
	l, clock := newTestLimiter()
	// Spread over many IPs so the per-IP budget is never the limit.
	ip := func(i int) string { return fmt.Sprintf("10.2.0.%d", i) }
	for i := 0; i < usernameFreeFailures; i++ {
		ok, _ := l.allow(ip(i), "admin")
		require.True(t, ok)
		l.failure(ip(i), "admin")
	}
	ok, retry := l.allow(ip(200), "admin")
	require.False(t, ok, "after the free failures the username backs off for every untrusted IP")
	require.Equal(t, time.Second, retry)

	clock.advance(time.Second)
	ok, _ = l.allow(ip(200), "admin")
	require.True(t, ok)
	l.failure(ip(200), "admin")
	_, retry = l.allow(ip(201), "admin")
	require.Equal(t, 2*time.Second, retry, "each further failure doubles the delay")

	for i := 0; i < 40; i++ {
		clock.advance(usernameMaxBackoff)
		if ok, _ := l.allow(ip(202), "admin"); ok {
			l.failure(ip(202), "admin")
		}
	}
	_, retry = l.allow(ip(203), "admin")
	require.LessOrEqual(t, retry, usernameMaxBackoff, "the delay is capped")
	// Other usernames are unaffected.
	ok, _ = l.allow(ip(204), "someone-else")
	require.True(t, ok)
}

func TestLimiterTrustedIPBypassesUsernameBackoff(t *testing.T) {
	l, _ := newTestLimiter()
	ok, _ := l.allow("192.168.1.10", "admin")
	require.True(t, ok)
	l.success("192.168.1.10", "admin")
	for i := 0; i < 100; i++ {
		ip := fmt.Sprintf("10.3.%d.%d", i>>8&255, i&255)
		if ok, _ := l.allow(ip, "admin"); ok {
			l.failure(ip, "admin")
		}
	}
	ok, _ = l.allow("192.168.1.10", "admin")
	require.True(t, ok, "an attacker hammering the username must not lock out the admin's known device")
}

func loginFrom(router http.Handler, ip, username, password string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://piworker.local/api/auth/login",
		strings.NewReader(fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)))
	r.RemoteAddr = ip + ":40000"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-PiWorker-Request", "1")
	r.Header.Set("Origin", "http://piworker.local")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func TestLoginBruteForceFromOneIPDoesNotLockOutAdmin(t *testing.T) {
	router, _, _ := sessionTestRouter(t, true)
	require.Equal(t, 200, loginFrom(router, "192.168.1.10", "admin", "test-password").Code)
	codes := map[int]int{}
	for i := 0; i < 150; i++ {
		codes[loginFrom(router, "192.168.1.66", "admin", "guess").Code]++
	}
	require.Positive(t, codes[429], "the attacker is throttled")
	// Previously a 100/min global budget let the attacker lock everyone out.
	require.Equal(t, 200, loginFrom(router, "192.168.1.10", "admin", "test-password").Code)
}

func TestPasswordChangeDoesNotConsumeLoginBudget(t *testing.T) {
	router, _, _ := sessionTestRouter(t, true)
	cookie := loginCookie(t, router)
	for i := 0; i < 12; i++ {
		authRequest(router, "POST", "/api/auth/password", map[string]string{"currentPassword": "wrong-password", "password": "new-password-1"}, cookie)
	}
	require.Equal(t, 200, authRequest(router, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "test-password"}, nil).Code)
}
