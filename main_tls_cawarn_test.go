package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/tlsca"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The CA expiry warning is logged when it appears and when its text changes
// (it counts the days left, so about once a day), not on every 10-minute
// refresh.
func TestCAExpiryWarningIsLoggedOncePerChange(t *testing.T) {
	var buf bytes.Buffer
	w := caWarner{log: zerolog.New(&buf)}
	w.check(tlsca.Status{})
	w.check(tlsca.Status{Warning: "expires in 3 days"})
	w.check(tlsca.Status{Warning: "expires in 3 days"})
	w.check(tlsca.Status{Warning: "expires in 2 days"})
	w.check(tlsca.Status{})
	w.check(tlsca.Status{Warning: "expires in 2 days"})
	out := buf.String()
	assert.Equal(t, 3, strings.Count(out, `"level":"warn"`), out)
	assert.Equal(t, 1, strings.Count(out, "expires in 3 days"))
}

// refreshTLS reports the warning of a CA close to expiry.
func TestRefreshTLSLogsTheCAExpiryWarning(t *testing.T) {
	dir := t.TempDir()
	ifaces := &fakeInterfaces{}
	ifaces.set("192.168.1.10")
	opts := tlsca.Options{Dir: dir, Interfaces: ifaces.get, Hostname: func() (string, error) { return "watchpi", nil }}
	m, err := tlsca.NewManager(opts)
	require.NoError(t, err)
	caEnd := m.Status().CANotAfter
	opts.Now = func() time.Time { return caEnd.Add(-30 * 24 * time.Hour) }
	m, err = tlsca.NewManager(opts)
	require.NoError(t, err)

	var buf bytes.Buffer
	w := caWarner{log: zerolog.New(&buf)}
	refreshTLS(m, &w)
	refreshTLS(m, &w)
	assert.Equal(t, 1, strings.Count(buf.String(), "The local CA expires on "+caEnd.UTC().Format(time.DateOnly)), buf.String())
}
