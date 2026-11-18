package main

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// httpSeries counts the series of the HTTP request metrics.
func httpSeries(t *testing.T) int {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	n := 0
	for _, mf := range families {
		switch mf.GetName() {
		case "piworker_http_requests_total", "piworker_http_request_duration_seconds":
			n += len(mf.GetMetric())
		}
	}
	return n
}

// captureLog sends the process log, at info level, to a buffer while the
// test runs (the router takes log.Logger when it is built).
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := log.Logger
	log.Logger = zerolog.New(&buf).Level(zerolog.InfoLevel)
	t.Cleanup(func() { log.Logger = old })
	return &buf
}

// TestInventedMethodsAreBounded: someone on the LAN without credentials
// sent requests with invented methods (any token, up to the header limit)
// and a foreign Host. Each distinct method created two Prometheus series
// for good (about 1.9 KB each, or the whole request line for a long one),
// refused requests included, and the access log wrote the method and path
// untruncated at info level. Now the method label takes a fixed set of
// values, the access log cuts long fields, and refusals for an unknown
// Host are logged at debug level only.
func TestInventedMethodsAreBounded(t *testing.T) {
	logs := captureLog(t)
	app := newTestApp(t, true)
	// Warm up the series the known methods use.
	for _, m := range []string{"GET", "POST"} {
		for _, host := range []string{"attacker.example", "piworker.local"} {
			r := httptest.NewRequest(m, "http://"+host+"/piworker-ca.crt", nil)
			app.handler.ServeHTTP(httptest.NewRecorder(), r)
			r = httptest.NewRequest(m, "http://"+host+"/api/flows", nil)
			app.handler.ServeHTTP(httptest.NewRecorder(), r)
		}
	}
	before := httpSeries(t)
	logs.Reset()

	long := strings.Repeat("A", 64<<10)
	codes := map[int]int{}
	const n = 400
	for i := range n {
		method := fmt.Sprintf("M%d", i)
		if i%4 == 0 {
			method = long + method
		}
		path := "/api/flows"
		if i%2 == 0 {
			path = "/piworker-ca.crt" // exempt from the Host guard: served
		}
		r := httptest.NewRequest(http.MethodGet, "http://attacker.example"+path, nil)
		r.Method = method
		if i%8 == 1 {
			r.URL.Path = "/" + long // unknown Host: refused
		}
		w := httptest.NewRecorder()
		app.handler.ServeHTTP(w, r)
		codes[w.Code]++
	}
	t.Logf("status codes: %v", codes)
	assert.LessOrEqual(t, httpSeries(t)-before, 6, "a fixed set of method labels")
	assert.Less(t, logs.Len(), n/2*1024, "about one short line per served request, none per refusal")
	assert.NotContains(t, logs.String(), long, "long fields are cut")
	for line := range strings.SplitSeq(strings.TrimSpace(logs.String()), "\n") {
		assert.Contains(t, line, "piworker-ca.crt", "only the served requests are logged at info")
	}
}

// TestServerHeaderLimit: both listeners refuse request lines and headers
// over 64 KiB (Go's default is 1 MiB) before any handler runs.
func TestServerHeaderLimit(t *testing.T) {
	logs := captureLog(t)
	app := newTestApp(t, true)
	for name, handler := range map[string]http.Handler{
		"https":    app.handler,
		"redirect": redirectHandler("8443", app.handler, true),
	} {
		t.Run(name, func(t *testing.T) {
			srv := newHTTPServer("", handler)
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			go func() { _ = srv.Serve(ln) }()
			t.Cleanup(func() { _ = srv.Close() })

			send := func(method string) *http.Response {
				conn, err := net.Dial("tcp", ln.Addr().String())
				require.NoError(t, err)
				defer conn.Close()
				require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
				_, err = fmt.Fprintf(conn, "%s /piworker-ca.crt HTTP/1.1\r\nHost: attacker.example\r\nConnection: close\r\n\r\n", method)
				require.NoError(t, err)
				res, err := http.ReadResponse(bufio.NewReader(conn), nil)
				require.NoError(t, err)
				res.Body.Close()
				return res
			}
			assert.Equal(t, http.StatusOK, send("GET").StatusCode)
			logs.Reset()
			res := send(strings.Repeat("A", 100<<10))
			assert.Equal(t, http.StatusRequestHeaderFieldsTooLarge, res.StatusCode)
			assert.Zero(t, logs.Len(), "refused before the router")
		})
	}
}
