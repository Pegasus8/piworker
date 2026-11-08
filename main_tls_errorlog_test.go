package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncBuffer is a bytes.Buffer safe for the server's goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func countLevel(out, level string) int { return strings.Count(out, `"level":"`+level+`"`) }

// Handshake errors (browsers that do not trust the CA yet, scanners, probes)
// are routine on a LAN device: they go to debug level, rate limited, instead
// of net/http's default stderr logger. Other server errors stay warnings.
func TestServerErrorLogRoutesHandshakeErrorsToDebugWithRateLimit(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	w := newServerErrorWriter(zerolog.New(&buf).Level(zerolog.DebugLevel), func() time.Time { return now })
	logger := w.logger()

	for i := range handshakeLogBurst + 5 {
		logger.Printf("http: TLS handshake error from 192.168.1.%d:5000: remote error: tls: unknown certificate authority", i)
	}
	out := buf.String()
	assert.Equal(t, handshakeLogBurst, countLevel(out, "debug"), out)
	assert.Equal(t, 0, countLevel(out, "warn"))
	assert.NotContains(t, out, "192.168.1.14:", "lines past the burst are dropped")

	buf.Reset()
	now = now.Add(handshakeLogWindow)
	logger.Printf("http: TLS handshake error from 10.0.0.1:6000: EOF")
	out = buf.String()
	assert.Contains(t, out, `"suppressed":5`, "the next window reports how many were dropped")
	assert.Contains(t, out, "10.0.0.1:6000")

	buf.Reset()
	logger.Printf("http: Accept error: too many open files; retrying in 5ms")
	logger.Printf("http: Accept error: too many open files; retrying in 10ms")
	out = buf.String()
	assert.Equal(t, 2, countLevel(out, "warn"), "other server errors are not rate limited")
	assert.NotContains(t, out, "\\n\"", "no trailing newline in messages")
}

// The filter matches what net/http really writes for a failed handshake.
func TestServerErrorLogCatchesRealHandshakeFailures(t *testing.T) {
	var buf syncBuffer
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Config.ErrorLog = newServerErrorWriter(zerolog.New(&buf).Level(zerolog.DebugLevel), time.Now).logger()
	srv.StartTLS()
	defer srv.Close()

	// A client that does not trust the server's certificate aborts the
	// handshake, like a browser before the CA is trusted.
	_, err := (&http.Client{Timeout: 5 * time.Second}).Get(srv.URL)
	require.Error(t, err)
	require.Eventually(t, func() bool { return strings.Contains(buf.String(), "TLS handshake error") }, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, 0, countLevel(buf.String(), "warn"), buf.String())
	assert.GreaterOrEqual(t, countLevel(buf.String(), "debug"), 1)
}

func TestHTTPServersUseTheFilteredErrorLog(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	require.NotNil(t, srv.ErrorLog, "nil means net/http's default stderr logger")
}
