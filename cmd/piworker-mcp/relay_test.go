package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pipeBridge runs the relay between in-memory pipes.
type pipeBridge struct {
	stdin  *io.PipeWriter // the client's side of the bridge's stdin
	stdout *io.PipeReader // the client's side of the bridge's stdout
	stderr *syncBuffer
	done   chan struct{} // closed when serve returned

	mu  sync.Mutex
	err error
}

func (b *pipeBridge) result() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

func startBridgeCtx(t *testing.T, ctx context.Context, cfg relayConfig) *pipeBridge {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	b := &pipeBridge{stdin: inW, stdout: outR, stderr: &syncBuffer{}, done: make(chan struct{})}
	if cfg.Log == nil {
		cfg.Log = log.New(b.stderr, "", 0)
	}
	r := newRelay(cfg)
	go func() {
		err := r.serve(ctx, inR, outW)
		b.mu.Lock()
		b.err = err
		b.mu.Unlock()
		_ = outW.Close()
		close(b.done)
	}()
	t.Cleanup(func() {
		_ = inW.Close()
		_ = outR.Close()
		select {
		case <-b.done:
		case <-time.After(10 * time.Second):
			t.Error("the bridge did not exit")
		}
	})
	return b
}

func startBridge(t *testing.T, cfg relayConfig) *pipeBridge {
	return startBridgeCtx(t, context.Background(), cfg)
}

// rawBridge exchanges raw lines with the relay.
type rawBridge struct {
	*pipeBridge
	out chan string
}

func startRaw(t *testing.T, cfg relayConfig) *rawBridge {
	t.Helper()
	return wrapRaw(startBridge(t, cfg))
}

func wrapRaw(b *pipeBridge) *rawBridge {
	out := make(chan string, 256)
	go func() {
		sc := bufio.NewScanner(b.stdout)
		sc.Buffer(make([]byte, 64<<10), 64<<20)
		for sc.Scan() {
			out <- sc.Text()
		}
		close(out)
	}()
	return &rawBridge{pipeBridge: b, out: out}
}

func (b *rawBridge) send(t *testing.T, line string) {
	t.Helper()
	_, err := io.WriteString(b.stdin, line+"\n")
	require.NoError(t, err)
}

func (b *rawBridge) recvLine(t *testing.T) string {
	t.Helper()
	select {
	case line, ok := <-b.out:
		require.True(t, ok, "stdout closed")
		return line
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for a message on stdout")
		return ""
	}
}

func (b *rawBridge) recv(t *testing.T) map[string]any {
	t.Helper()
	line := b.recvLine(t)
	var msg map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &msg), line)
	return msg
}

// rest closes stdin and returns every remaining message until stdout closes.
func (b *rawBridge) rest(t *testing.T) []map[string]any {
	t.Helper()
	require.NoError(t, b.stdin.Close())
	var msgs []map[string]any
	for {
		select {
		case line, ok := <-b.out:
			if !ok {
				return msgs
			}
			var msg map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &msg), line)
			msgs = append(msgs, msg)
		case <-time.After(10 * time.Second):
			t.Fatal("stdout did not close")
		}
	}
}

func rpcErr(t *testing.T, msg map[string]any) (code float64, message string, data map[string]any) {
	t.Helper()
	e, ok := msg["error"].(map[string]any)
	require.True(t, ok, "expected an error response, got %v", msg)
	data, _ = e["data"].(map[string]any)
	return e["code"].(float64), e["message"].(string), data
}

// plainServer is a hand-written /mcp endpoint on plain http to 127.0.0.1.
func plainServer(t *testing.T, h http.HandlerFunc) (relayConfig, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(rec.wrap(h))
	t.Cleanup(srv.Close)
	endpoint := mustURL(t, srv.URL+"/mcp")
	c, err := newConnector(endpoint, trustOptions{})
	require.NoError(t, err)
	return relayConfig{Endpoint: endpoint, Token: testToken, Connector: c}, rec
}

// respond replies to the JSON-RPC request in r with result.
func respond(w http.ResponseWriter, r *http.Request, result string) {
	var req struct {
		ID json.RawMessage `json:"id"`
	}
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &req)
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, req.ID, result)
}

func echoServer(t *testing.T) (relayConfig, *recorder) {
	return plainServer(t, func(w http.ResponseWriter, r *http.Request) { respond(w, r, `{"ok":true}`) })
}

func TestRelayForwardsTheMessageVerbatim(t *testing.T) {
	cfg, rec := echoServer(t)
	b := startRaw(t, cfg)
	line := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"cursor":"a b"}}`
	b.send(t, "  "+line+"\r")

	msg := b.recv(t)
	assert.Equal(t, float64(1), msg["id"])
	assert.Equal(t, map[string]any{"ok": true}, msg["result"])
	seen := rec.requests()
	require.Len(t, seen, 1)
	assert.Equal(t, line, seen[0].Body)
	assert.Equal(t, "POST", seen[0].Method)
	assert.Equal(t, "/mcp", seen[0].Path)
	assert.Equal(t, "Bearer "+testToken, seen[0].Authorization)
	assert.Equal(t, "application/json", seen[0].ContentType)
	assert.Equal(t, "application/json, text/event-stream", seen[0].Accept)
	assert.Equal(t, "piworker-mcp/"+version, seen[0].UserAgent)
	assert.Empty(t, seen[0].ProtocolVersion, "nothing negotiated yet")
}

func TestRelayAnswersAreOneLineEach(t *testing.T) {
	cfg, _ := plainServer(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, r, "{\n  \"text\": \"a\\nb\",\n  \"n\": 1\n}\n")
	})
	b := startRaw(t, cfg)
	b.send(t, `{"jsonrpc":"2.0","id":"x","method":"ping"}`)
	assert.Equal(t, `{"jsonrpc":"2.0","id":"x","result":{"text":"a\nb","n":1}}`, b.recvLine(t))
}

func TestRelayUsesTheNegotiatedProtocolVersion(t *testing.T) {
	cfg, rec := plainServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mcp-Protocol-Version") == "" {
			respond(w, r, `{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"pw","version":"1"}}`)
			return
		}
		respond(w, r, `{}`)
	})
	b := startRaw(t, cfg)
	b.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	b.recv(t)
	b.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	b.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"garage_open"}}`)
	b.recv(t)

	// The notification is forwarded concurrently with the call and gets no
	// answer, so it may reach the server after the call's response arrived.
	require.Eventually(t, func() bool { return len(rec.requests()) == 3 }, 5*time.Second, 5*time.Millisecond)
	seen := rec.requests()
	require.Len(t, seen, 3)
	versions := map[string]string{}
	for _, r := range seen {
		versions[r.BodyMethod] = r.ProtocolVersion
		assert.Empty(t, r.MCPMethod, "standard headers start with 2026-07-28")
	}
	assert.Equal(t, map[string]string{"initialize": "", "notifications/initialized": "2025-06-18", "tools/call": "2025-06-18"}, versions)
}

func TestRelayIgnoresAMalformedNegotiatedVersion(t *testing.T) {
	cfg, rec := plainServer(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, r, `{"protocolVersion":"2025-06-18\r\nX-Evil: 1"}`)
	})
	b := startRaw(t, cfg)
	b.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	b.recv(t)
	b.send(t, `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	b.recv(t)
	seen := rec.requests()
	require.Len(t, seen, 2)
	assert.Empty(t, seen[1].ProtocolVersion)
}

func TestRelayTakesThePerRequestVersionAndSetsStandardHeaders(t *testing.T) {
	cfg, rec := echoServer(t)
	b := startRaw(t, cfg)
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}`
	b.send(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"garage_open",`+meta+`}}`)
	b.send(t, `{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"diagnose_flow",`+meta+`}}`)
	b.send(t, `{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"piworker://x",`+meta+`}}`)
	b.send(t, `{"jsonrpc":"2.0","id":4,"method":"tools/list","params":{`+meta+`}}`)
	for range 4 {
		b.recv(t)
	}
	names := map[string]string{}
	for _, r := range rec.requests() {
		assert.Equal(t, "2026-07-28", r.ProtocolVersion)
		assert.Equal(t, r.BodyMethod, r.MCPMethod)
		names[r.BodyMethod] = r.MCPName
	}
	assert.Equal(t, map[string]string{"tools/call": "garage_open", "prompts/get": "diagnose_flow", "resources/read": "piworker://x", "tools/list": ""}, names)
}

func TestRelayLeavesOutNamesThatAreNotHeaderValues(t *testing.T) {
	cfg, rec := echoServer(t)
	b := startRaw(t, cfg)
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}`
	b.send(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"bad\u0007name",`+meta+`}}`)
	assert.Contains(t, b.recv(t), "result", "the server decides; the bridge does not fail the request")
	seen := rec.requests()
	require.Len(t, seen, 1)
	assert.Empty(t, seen[0].MCPName)
	assert.Equal(t, "tools/call", seen[0].MCPMethod)
}

func TestRelayTimesOutOneWayMessages(t *testing.T) {
	s, cfg, _ := newBlockingServer(t)
	cfg.RequestTimeout = 200 * time.Millisecond
	cfg.DrainTimeout = time.Minute
	b := startRaw(t, cfg)
	b.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	recvID(t, s.started, "the notification")
	start := time.Now()
	assert.Empty(t, b.rest(t))
	assert.Less(t, time.Since(start), 10*time.Second, "the notification gave up on its own, not at the drain timeout")
	assert.Contains(t, b.stderr.String(), "notifications/initialized: PiWorker did not answer within 200ms")
}

func TestRelayOneWayMessagesGetNoAnswer(t *testing.T) {
	cfg, rec := echoServer(t)
	b := startRaw(t, cfg)
	b.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	b.send(t, `{"jsonrpc":"2.0","id":"srv-1","result":{"roots":[]}}`) // answer to a server request
	msgs := b.rest(t)
	assert.Empty(t, msgs)
	assert.ElementsMatch(t, []string{"notifications/initialized", ""}, []string{rec.requests()[0].BodyMethod, rec.requests()[1].BodyMethod})
	require.NoError(t, b.result())
}

func TestRelayReadsEventStreams(t *testing.T) {
	cfg, _ := plainServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, ": keep-alive\n\nid: 0\nretry: 100\n\n")
		_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progress\":1}}\n\n")
		fl.Flush()
		_, _ = io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"id\":99,\"result\":{}}\n\n") // someone else's answer
		_, _ = io.WriteString(w, "event: message\r\ndata: {\"jsonrpc\":\"2.0\",\r\ndata: \"id\":5,\"result\":{\"done\":true}}\r\n\r\n")
		_, _ = io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/late\"}\n\n")
	})
	b := startRaw(t, cfg)
	b.send(t, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"x"}}`)
	msgs := b.rest(t)
	require.Len(t, msgs, 2, "the progress notification, then the answer; nothing after it")
	assert.Equal(t, "notifications/progress", msgs[0]["method"])
	assert.Equal(t, float64(5), msgs[1]["id"])
	assert.Equal(t, map[string]any{"done": true}, msgs[1]["result"])
	assert.Contains(t, b.stderr.String(), "ignored an answer for another request")
}

func TestRelayEventStreamWithoutAnAnswer(t *testing.T) {
	cfg, _ := plainServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
	})
	b := startRaw(t, cfg)
	b.send(t, `{"jsonrpc":"2.0","id":5,"method":"tools/call"}`)
	assert.Equal(t, "notifications/progress", b.recv(t)["method"])
	msg := b.recv(t)
	assert.Equal(t, float64(5), msg["id"])
	code, message, _ := rpcErr(t, msg)
	assert.Equal(t, float64(codeUpstream), code)
	assert.Contains(t, message, "without answering")
}

func TestRelayMapsHTTPErrors(t *testing.T) {
	soon := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	for _, tc := range []struct {
		name      string
		status    int
		header    map[string]string
		body      string
		want      []string
		retry     any
		wantNoKey string
	}{
		{name: "401", status: 401, header: map[string]string{"WWW-Authenticate": "Bearer"}, body: `{"error":"invalid token"}`,
			want: []string{"HTTP 401", "rejected the agent token", "Agents"}},
		{name: "403 text", status: 403, body: "Forbidden: host \"pi.evil\" is not allowed\n",
			want: []string{"HTTP 403", `host "pi.evil" is not allowed`, "allowed_hosts"}},
		{name: "403 json-rpc", status: 403, header: map[string]string{"Content-Type": "application/json"},
			body: `{"jsonrpc":"2.0","id":null,"error":{"code":-32001,"message":"token lacks the call scope"}}`,
			want: []string{"HTTP 403", "token lacks the call scope"}},
		{name: "404", status: 404, header: map[string]string{"Content-Type": "application/json"}, body: `{"success":false,"error":"not found"}`,
			want: []string{"HTTP 404", "enabled = true"}},
		{name: "429 seconds", status: 429, header: map[string]string{"Retry-After": "30"},
			want: []string{"HTTP 429", "rate limit", "30 seconds"}, retry: float64(30)},
		{name: "429 date", status: 429, header: map[string]string{"Retry-After": soon},
			want: []string{"HTTP 429", "seconds"}},
		{name: "429 no header", status: 429, want: []string{"HTTP 429", "try again later"}, wantNoKey: "retryAfter"},
		{name: "500", status: 500, body: "boom", want: []string{"HTTP 500", "boom"}},
		{name: "503", status: 503, want: []string{"HTTP 503"}},
		{name: "413", status: 413, want: []string{"HTTP 413", "too large"}},
		{name: "308", status: 308, header: map[string]string{"Location": "https://elsewhere.example/mcp"},
			want: []string{"HTTP 308", "redirect", "https://elsewhere.example/mcp", "--url"}},
		{name: "400", status: 400, body: "Bad Request: Unsupported protocol version", want: []string{"HTTP 400", "Unsupported protocol version"}},
		{name: "control characters", status: 400, header: map[string]string{"Content-Type": "text/plain"},
			body: "bad\x1b[31m\x00thing‮", want: []string{"bad[31mthing"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, rec := plainServer(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			b := startRaw(t, cfg)
			b.send(t, `{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":"x"}}`)
			msg := b.recv(t)
			assert.Equal(t, "call-1", msg["id"])
			code, message, data := rpcErr(t, msg)
			assert.Equal(t, float64(codeUpstream), code)
			for _, want := range tc.want {
				assert.Contains(t, message, want)
			}
			assert.Equal(t, float64(tc.status), data["httpStatus"])
			if tc.retry != nil {
				assert.Equal(t, tc.retry, data["retryAfter"])
			}
			if tc.name == "429 date" {
				assert.InDelta(t, 90, data["retryAfter"], 5)
			}
			if tc.wantNoKey != "" {
				assert.NotContains(t, data, tc.wantNoKey)
			}
			assert.Len(t, rec.requests(), 1, "nothing is retried")
			assert.Contains(t, b.stderr.String(), "tools/call")
			assert.NotContains(t, b.stderr.String(), testToken)
		})
	}
}

func TestRelayPassesJSONRPCErrorsThrough(t *testing.T) {
	cfg, _ := plainServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":null,"error":{"code":-32022,"message":"unsupported protocol version","data":{"supported":["2025-11-25"]}}}`)
	})
	b := startRaw(t, cfg)
	b.send(t, `{"jsonrpc":"2.0","id":7,"method":"server/discover"}`)
	msg := b.recv(t)
	assert.Equal(t, float64(7), msg["id"])
	code, message, data := rpcErr(t, msg)
	assert.Equal(t, float64(-32022), code)
	assert.Equal(t, "unsupported protocol version", message)
	assert.Equal(t, map[string]any{"supported": []any{"2025-11-25"}}, data)
}

func TestRelayRejectsBadAnswers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		handle http.HandlerFunc
		want   string
	}{
		{"answer for another id", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{}}`)
		}, "another request"},
		{"accepted without answer", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}, "without answering"},
		{"not json", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `<html>`)
		}, "not a JSON-RPC message"},
		{"batch answer", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"jsonrpc":"2.0","id":1,"result":{}}]`)
		}, "not a JSON-RPC message"},
		{"html page", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, `<html>`)
		}, "text/html"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _ := plainServer(t, tc.handle)
			b := startRaw(t, cfg)
			b.send(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
			msg := b.recv(t)
			assert.Equal(t, float64(1), msg["id"])
			_, message, _ := rpcErr(t, msg)
			assert.Contains(t, message, tc.want)
		})
	}
}

func TestRelayRejectsBadInput(t *testing.T) {
	cfg, rec := echoServer(t)
	b := startRaw(t, cfg)
	for _, tc := range []struct {
		line string
		code float64
		want string
		id   any
	}{
		{`not json`, -32700, "parse error", nil},
		{`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, -32600, "batch", nil},
		{`{"jsonrpc":"2.0"}`, -32600, "invalid request", nil},
		{`"text"`, -32600, "invalid request", nil},
		{`{"jsonrpc":"2.0","id":{"a":1},"method":"ping"}`, -32600, "id", nil},
		{`{"jsonrpc":"2.0","id":null,"method":"ping"}`, -32600, "id", nil},
		{`{"jsonrpc":"2.0","id":3,"method":7}`, -32600, "invalid request", float64(3)},
		{`{"jsonrpc":"1.0","id":4,"method":"ping"}`, -32600, "jsonrpc", float64(4)},
	} {
		b.send(t, "")
		b.send(t, tc.line)
		msg := b.recv(t)
		assert.Equal(t, tc.id, msg["id"], tc.line)
		code, message, _ := rpcErr(t, msg)
		assert.Equal(t, tc.code, code, tc.line)
		assert.Contains(t, strings.ToLower(message), tc.want, tc.line)
	}
	assert.Empty(t, rec.requests(), "nothing invalid is forwarded")
}

// blockingServer answers each request once released (by id), recording how
// many are in flight at once and which were aborted.
type blockingServer struct {
	mu        sync.Mutex
	release   map[string]chan struct{}
	inFlight  int
	maxSeen   int
	started   chan string
	aborted   chan string
	completed atomic.Int32
}

func newBlockingServer(t *testing.T) (*blockingServer, relayConfig, *recorder) {
	s := &blockingServer{release: map[string]chan struct{}{}, started: make(chan string, 64), aborted: make(chan string, 64)}
	cfg, rec := plainServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID json.RawMessage `json:"id"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		id := string(req.ID)
		s.mu.Lock()
		s.inFlight++
		s.maxSeen = max(s.maxSeen, s.inFlight)
		ch := s.ch(id)
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.inFlight--
			s.mu.Unlock()
		}()
		s.started <- id
		select {
		case <-ch:
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{}}`, id)
			s.completed.Add(1)
		case <-r.Context().Done():
			s.aborted <- id
		}
	})
	t.Cleanup(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, ch := range s.release {
			select {
			case <-ch:
			default:
				close(ch)
			}
		}
	})
	return s, cfg, rec
}

func (s *blockingServer) ch(id string) chan struct{} {
	ch, ok := s.release[id]
	if !ok {
		ch = make(chan struct{})
		s.release[id] = ch
	}
	return ch
}

func (s *blockingServer) let(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	close(s.ch(id))
}

func recvID(t *testing.T, ch <-chan string, what string) string {
	t.Helper()
	select {
	case id := <-ch:
		return id
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return ""
	}
}

func TestRelayAnswersConcurrentRequestsInCompletionOrder(t *testing.T) {
	s, cfg, _ := newBlockingServer(t)
	cfg.MaxInFlight = 2
	b := startRaw(t, cfg)
	for i := 1; i <= 5; i++ {
		b.send(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call"}`, i))
	}
	first := []string{recvID(t, s.started, "a request"), recvID(t, s.started, "a request")}
	select {
	case id := <-s.started:
		t.Fatalf("request %s started beyond the concurrency bound", id)
	case <-time.After(200 * time.Millisecond):
	}
	// Release the later of the two first: its answer comes first.
	s.let(first[1])
	assert.Equal(t, first[1], fmt.Sprint(b.recv(t)["id"]))
	s.let(first[0])
	assert.Equal(t, first[0], fmt.Sprint(b.recv(t)["id"]))
	for range 3 {
		s.let(recvID(t, s.started, "a queued request"))
		b.recv(t)
	}
	s.mu.Lock()
	assert.Equal(t, 2, s.maxSeen)
	s.mu.Unlock()
}

func TestRelayBoundsQueuedRequests(t *testing.T) {
	s, cfg, _ := newBlockingServer(t)
	cfg.MaxInFlight = 1
	cfg.MaxQueued = 2
	b := startRaw(t, cfg)
	b.send(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`)
	b.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/call"}`)
	b.send(t, `{"jsonrpc":"2.0","id":3,"method":"tools/call"}`)
	msg := b.recv(t)
	assert.Equal(t, float64(3), msg["id"])
	_, message, _ := rpcErr(t, msg)
	assert.Contains(t, message, "too many requests")
	// Requests 1 and 2 share the one slot, in either order.
	for range 2 {
		id := recvID(t, s.started, "a queued request")
		s.let(id)
		assert.Equal(t, id, fmt.Sprint(b.recv(t)["id"]))
	}
}

func TestRelayRefusesADuplicateIDInFlight(t *testing.T) {
	s, cfg, rec := newBlockingServer(t)
	b := startRaw(t, cfg)
	b.send(t, `{"jsonrpc":"2.0","id":"a","method":"tools/call"}`)
	recvID(t, s.started, "request a")
	b.send(t, `{"jsonrpc":"2.0","id":"a","method":"tools/list"}`)
	msg := b.recv(t)
	code, message, _ := rpcErr(t, msg)
	assert.Equal(t, float64(-32600), code)
	assert.Contains(t, message, "already in use")
	s.let(`"a"`)
	msg = b.recv(t)
	assert.Equal(t, "a", msg["id"])
	assert.Contains(t, msg, "result")
	assert.Len(t, rec.requests(), 1)
}

func TestRelayCancelledRequestsAreAbortedAndNotAnswered(t *testing.T) {
	s, cfg, rec := newBlockingServer(t)
	b := startRaw(t, cfg)
	b.send(t, `{"jsonrpc":"2.0","id":7,"method":"tools/call"}`)
	b.send(t, `{"jsonrpc":"2.0","id":"s","method":"tools/call"}`)
	recvID(t, s.started, "a request")
	recvID(t, s.started, "a request")

	b.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7.0,"reason":"user"}}`)
	assert.Equal(t, "7", recvID(t, s.aborted, "request 7 to be aborted"))
	b.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"s"}}`)
	assert.Equal(t, `"s"`, recvID(t, s.aborted, "request s to be aborted"))
	b.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":12345}}`) // unknown: ignored

	msgs := b.rest(t)
	assert.Empty(t, msgs, "no answer for cancelled requests")
	for _, r := range rec.requests() {
		assert.NotEqual(t, "notifications/cancelled", r.BodyMethod, "handled locally")
	}
}

func TestRelayTimesOutSlowAnswers(t *testing.T) {
	s, cfg, _ := newBlockingServer(t)
	cfg.RequestTimeout = 200 * time.Millisecond
	b := startRaw(t, cfg)
	b.send(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`)
	msg := b.recv(t)
	_, message, _ := rpcErr(t, msg)
	assert.Contains(t, message, "did not answer within 200ms")
	recvID(t, s.aborted, "the request to be aborted")
}

func TestRelayWaitsForInFlightRequestsAtEOF(t *testing.T) {
	cfg, _ := plainServer(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		respond(w, r, `{"late":true}`)
	})
	b := startRaw(t, cfg)
	b.send(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`)
	msgs := b.rest(t)
	require.Len(t, msgs, 1)
	assert.Equal(t, map[string]any{"late": true}, msgs[0]["result"])
	require.NoError(t, b.result())
}

func TestRelayGivesUpOnInFlightRequestsAfterTheDrainTimeout(t *testing.T) {
	s, cfg, _ := newBlockingServer(t)
	cfg.DrainTimeout = 200 * time.Millisecond
	b := startRaw(t, cfg)
	b.send(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`)
	recvID(t, s.started, "the request")
	start := time.Now()
	b.rest(t)
	assert.Less(t, time.Since(start), 5*time.Second)
	recvID(t, s.aborted, "the request to be aborted")
	require.NoError(t, b.result())
	assert.Contains(t, b.stderr.String(), "gave up on 1 request")
}

func TestRelayHandlesAFinalLineWithoutNewline(t *testing.T) {
	cfg, _ := echoServer(t)
	b := startRaw(t, cfg)
	_, err := io.WriteString(b.stdin, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	require.NoError(t, err)
	msgs := b.rest(t)
	require.Len(t, msgs, 1)
	assert.Equal(t, float64(1), msgs[0]["id"])
}

func TestRelayStopsWhenTheContextIsCancelled(t *testing.T) {
	s, cfg, _ := newBlockingServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	b := wrapRaw(startBridgeCtx(t, ctx, cfg))
	b.send(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`)
	recvID(t, s.started, "the request")
	cancel() // stdin stays open
	select {
	case <-b.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the bridge did not stop")
	}
	assert.ErrorIs(t, b.result(), context.Canceled)
	recvID(t, s.aborted, "the request to be aborted")
}

func TestRelayBoundsMessageSizes(t *testing.T) {
	big := strings.Repeat("x", 2048)
	cfg, rec := plainServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Mcp-Name"), "big") {
			respond(w, r, `{"text":"`+big+`"}`)
			return
		}
		respond(w, r, `{}`)
	})
	cfg.MaxMessageBytes = 1024
	cfg.MaxResponseBytes = 1024
	b := startRaw(t, cfg)

	b.send(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+big+`"}}`)
	code, message, _ := rpcErr(t, b.recv(t))
	assert.Equal(t, float64(-32600), code)
	assert.Contains(t, message, "larger than 1024 bytes")

	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}`
	b.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"big",`+meta+`}}`)
	msg := b.recv(t)
	assert.Equal(t, float64(2), msg["id"])
	_, message, _ = rpcErr(t, msg)
	assert.Contains(t, message, "larger than 1024 bytes")

	b.send(t, `{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	assert.Equal(t, float64(3), b.recv(t)["id"], "the relay goes on after an oversized message")
	assert.Len(t, rec.requests(), 2)
}

func TestRelayReportsUnreachableServers(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	endpoint := mustURL(t, "http://"+addr+"/mcp")
	c, err := newConnector(endpoint, trustOptions{})
	require.NoError(t, err)

	b := startRaw(t, relayConfig{Endpoint: endpoint, Token: testToken, Connector: c})
	b.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	_, message, _ := rpcErr(t, b.recv(t))
	assert.Contains(t, message, "cannot reach PiWorker at "+addr)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestRelayStopsWhenStdoutFails(t *testing.T) {
	cfg, _ := echoServer(t)
	cfg.Log = log.New(io.Discard, "", 0)
	inR, inW := io.Pipe()
	t.Cleanup(func() { _ = inW.Close() })
	done := make(chan error, 1)
	go func() { done <- newRelay(cfg).serve(context.Background(), inR, failingWriter{}) }()
	_, err := io.WriteString(inW, `{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n")
	require.NoError(t, err)
	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stdout")
	case <-time.After(5 * time.Second):
		t.Fatal("the bridge kept running without a client")
	}
}
