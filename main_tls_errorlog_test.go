package main

import (
	"bytes"
	"crypto/tls"
	"io"
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
	now = now.Add(serverLogWindow)
	logger.Printf("http: TLS handshake error from 10.0.0.1:6000: EOF")
	out = buf.String()
	assert.Contains(t, out, `"suppressed":5`, "the next window reports how many were dropped")
	assert.Contains(t, out, "10.0.0.1:6000")

	buf.Reset()
	logger.Printf("http: Accept error: too many open files; retrying in 5ms")
	logger.Printf("http: Accept error: too many open files; retrying in 10ms")
	out = buf.String()
	assert.Equal(t, 2, countLevel(out, "warn"), "other server errors stay warnings")
	assert.NotContains(t, out, "\\n\"", "no trailing newline in messages")
}

// A LAN scanner that completes the handshake (scanners do not verify the
// certificate) with ALPN h2 and then sends a non-h2 preface makes net/http log
// "http2: server: error reading preface" on every connection: the same noise
// as a failed handshake, so it shares its debug level and rate limit.
func TestServerErrorLogTreatsBadHTTP2PrefacesAsHandshakeNoise(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	w := newServerErrorWriter(zerolog.New(&buf).Level(zerolog.DebugLevel), func() time.Time { return now })
	logger := w.logger()

	for i := range handshakeLogBurst {
		logger.Printf("http: TLS handshake error from 192.168.1.%d:5000: EOF", i)
	}
	logger.Printf(`http2: server: error reading preface from client 192.168.1.99:5000: bogus greeting "GET / HTTP/1.1\r\nHost: pi"`)
	out := buf.String()
	assert.Equal(t, 0, countLevel(out, "warn"), out)
	assert.Equal(t, handshakeLogBurst, countLevel(out, "debug"), out)
	assert.NotContains(t, out, "192.168.1.99", "the preface error counts against the handshake burst")

	buf.Reset()
	now = now.Add(serverLogWindow)
	logger.Printf(`http2: server: error reading preface from client 192.168.1.100:5000: bogus greeting "SSH-2.0-scan"`)
	out = buf.String()
	assert.Contains(t, out, `"suppressed":1`)
	assert.Contains(t, out, "192.168.1.100")
	assert.Equal(t, 0, countLevel(out, "warn"), out)
}

// Every other server error line is a warning, but rate limited too: a peer
// that can trigger one on every connection must not flood the SD card.
func TestServerErrorLogRateLimitsOtherErrors(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	w := newServerErrorWriter(zerolog.New(&buf).Level(zerolog.DebugLevel), func() time.Time { return now })
	logger := w.logger()

	for i := range serverErrorLogBurst + 7 {
		logger.Printf("http2: server connection error from 192.168.1.%d:5000: PROTOCOL_ERROR", i)
	}
	// Handshake noise has its own budget: a flood of other errors does not
	// silence it, and it does not use theirs.
	logger.Printf("http: TLS handshake error from 10.0.0.9:7000: EOF")
	out := buf.String()
	assert.Equal(t, serverErrorLogBurst, countLevel(out, "warn"), out)
	assert.Equal(t, 1, countLevel(out, "debug"), out)
	assert.Contains(t, out, "10.0.0.9:7000")

	buf.Reset()
	now = now.Add(serverLogWindow)
	logger.Printf("http: Accept error: too many open files; retrying in 5ms")
	out = buf.String()
	assert.Contains(t, out, `"suppressed":7`, "the next window reports how many were dropped")
	assert.Equal(t, 2, countLevel(out, "warn"), "the report and the new line are warnings: %s", out)
	assert.Contains(t, out, "too many open files")
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

// The preface filter matches what net/http really writes when a peer
// negotiates h2 and then speaks HTTP/1, as LAN scanners do.
func TestServerErrorLogCatchesRealHTTP2PrefaceErrors(t *testing.T) {
	var buf syncBuffer
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.EnableHTTP2 = true
	srv.Config.ErrorLog = newServerErrorWriter(zerolog.New(&buf).Level(zerolog.DebugLevel), time.Now).logger()
	srv.StartTLS()
	defer srv.Close()

	// A scanner does not verify the certificate: skipping verification is
	// what this test simulates, not something PiWorker does.
	conn, err := tls.Dial("tcp", srv.Listener.Addr().String(), &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"h2"}}) //nolint:gosec // simulates a scanner
	require.NoError(t, err)
	defer conn.Close()
	require.Equal(t, "h2", conn.ConnectionState().NegotiatedProtocol)
	_, err = conn.Write([]byte("GET / HTTP/1.1\r\nHost: pi\r\n\r\n"))
	require.NoError(t, err)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _ = io.Copy(io.Discard, conn)

	require.Eventually(t, func() bool { return strings.Contains(buf.String(), "error reading preface") }, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, 0, countLevel(buf.String(), "warn"), buf.String())
	assert.GreaterOrEqual(t, countLevel(buf.String(), "debug"), 1)
}

func TestHTTPServersUseTheFilteredErrorLog(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	require.NotNil(t, srv.ErrorLog, "nil means net/http's default stderr logger")
}
