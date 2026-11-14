package builtin

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsBlockedIPReservedRanges(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254",
		"0.0.0.0", "0.1.2.3", // "this network": 0.x reaches the local host on Linux
		"100.64.0.1", "100.127.255.254", // CGNAT / Tailscale
		"192.0.0.1", "192.0.2.1", "198.18.0.1", "198.51.100.1", "203.0.113.1",
		"240.0.0.1", "255.255.255.255", "224.0.0.1",
		"::1", "::", "fe80::1", "fc00::1", "fd12::1", "ff02::1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1",
		"64:ff9b::7f00:1", "64:ff9b::a9fe:a9fe", // NAT64 of loopback / metadata
		"64:ff9b:1::1",
		"2002:7f00:1::", "2002:c0a8:101::", // 6to4 wrapping 127.0.0.1 / 192.168.1.1
		"2001:db8::1", "2001::1", // documentation, Teredo
	}
	for _, s := range blocked {
		assert.True(t, isBlockedIP(net.ParseIP(s)), "%s must be blocked", s)
	}
	allowed := []string{"1.1.1.1", "8.8.8.8", "100.63.255.255", "100.128.0.0", "2606:4700:4700::1111", "2002:808:808::"}
	for _, s := range allowed {
		assert.False(t, isBlockedIP(net.ParseIP(s)), "%s must be allowed", s)
	}
}

// An HTTP(S)_PROXY would make the dial-time IP check moot: the client dials
// the proxy, and the proxy resolves and connects to the internal target.
func TestGuardedClientIgnoresProxyUnlessPrivateAllowed(t *testing.T) {
	tr := newGuardedHTTPClient(time.Second, false).Transport.(*http.Transport)
	assert.Nil(t, tr.Proxy)

	tr = newGuardedHTTPClient(time.Second, true).Transport.(*http.Transport)
	require.NotNil(t, tr.Proxy)
}
