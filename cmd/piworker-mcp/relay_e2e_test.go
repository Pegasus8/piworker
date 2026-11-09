package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sdkFixture is a real go-sdk MCP server behind a bearer check, served over
// TLS by a test CA, like PiWorker's /mcp endpoint.
type sdkFixture struct {
	url       string
	ca        *testCA
	rec       *recorder
	started   chan struct{} // the slow tool started
	cancelled chan struct{} // the slow tool saw its context cancelled
	finished  chan struct{} // the slow tool was released and returned
	release   chan struct{} // lets the slow tool finish; see releaseSlow
	calls     atomic.Int32  // tool calls that reached a handler

	releaseOnce sync.Once
}

// releaseSlow lets every slow call finish.
func (f *sdkFixture) releaseSlow() { f.releaseOnce.Do(func() { close(f.release) }) }

type echoIn struct {
	Text string `json:"text"`
}

type echoOut struct {
	Echo string `json:"echo"`
}

func newSDKFixture(t *testing.T, jsonResponse bool) *sdkFixture {
	t.Helper()
	f := &sdkFixture{
		started:   make(chan struct{}, 4),
		cancelled: make(chan struct{}, 4),
		finished:  make(chan struct{}, 4),
		release:   make(chan struct{}),
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "piworker-test", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "echo", Description: "Echoes the text."},
		func(_ context.Context, _ *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, echoOut, error) {
			f.calls.Add(1)
			return nil, echoOut{Echo: in.Text}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "slow", Description: "Waits until released or cancelled."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			f.calls.Add(1)
			f.started <- struct{}{}
			select {
			case <-ctx.Done():
				f.cancelled <- struct{}{}
			case <-f.release:
				f.finished <- struct{}{}
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "done"}}}, nil, nil
		})
	server.AddPrompt(&mcp.Prompt{Name: "greet", Arguments: []*mcp.PromptArgument{{Name: "name", Required: true}}},
		func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: "Hello, " + req.Params.Arguments["name"] + "!"},
			}}}, nil
		})

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:                    true,
		JSONResponse:                 jsonResponse,
		PropagateRequestCancellation: true,
	})
	f.ca = newTestCA(t, "PiWorker Local CA test")
	srv, rec := newTLSServer(t, f.ca.serverCert(t, false, "127.0.0.1"), f.ca, bearer(testToken, handler))
	// Registered after the server, so it runs before srv.Close, which waits
	// for slow calls still running.
	t.Cleanup(f.releaseSlow)
	f.url, f.rec = srv.URL+"/mcp", rec
	return f
}

// bearer rejects requests without the token, like PiWorker's /mcp.
func bearer(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"invalid or missing agent token"}`)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// bridgeSession runs the relay between in-memory pipes and connects a real
// go-sdk client to it over the stdio transport.
type bridgeSession struct {
	*pipeBridge
	session *mcp.ClientSession
}

func connectThroughBridge(t *testing.T, cfg relayConfig, protocolVersion string) (*bridgeSession, error) {
	t.Helper()
	b := startBridge(t, cfg)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var opts *mcp.ClientSessionOptions
	if protocolVersion != "" {
		opts = &mcp.ClientSessionOptions{ProtocolVersion: protocolVersion}
	}
	session, err := client.Connect(ctx, &mcp.IOTransport{Reader: b.stdout, Writer: b.stdin}, opts)
	if err != nil {
		_ = b.stdin.Close()
		return &bridgeSession{pipeBridge: b}, err
	}
	t.Cleanup(func() { _ = session.Close() })
	return &bridgeSession{pipeBridge: b, session: session}, nil
}

func fixtureConfig(t *testing.T, f *sdkFixture) relayConfig {
	t.Helper()
	c, err := newConnector(mustURL(t, f.url), trustOptions{CAFingerprint: f.ca.fingerprint()})
	require.NoError(t, err)
	return relayConfig{Endpoint: mustURL(t, f.url), Token: testToken, Connector: c}
}

func TestBridgeRelaysToolsAndPromptsAcrossProtocolVersions(t *testing.T) {
	for _, jsonResponse := range []bool{true, false} {
		for _, version := range []string{"2025-06-18", "2025-11-25", "2026-07-28"} {
			name := version + "/sse"
			if jsonResponse {
				name = version + "/json"
			}
			t.Run(name, func(t *testing.T) {
				f := newSDKFixture(t, jsonResponse)
				b, err := connectThroughBridge(t, fixtureConfig(t, f), version)
				require.NoError(t, err)
				cs := b.session
				ctx := testCtx(t)
				require.Equal(t, version, cs.InitializeResult().ProtocolVersion)

				tools, err := cs.ListTools(ctx, nil)
				require.NoError(t, err)
				var names []string
				for _, tool := range tools.Tools {
					names = append(names, tool.Name)
				}
				assert.ElementsMatch(t, []string{"echo", "slow"}, names)

				res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"text": "héllo\nworld"}})
				require.NoError(t, err)
				require.False(t, res.IsError)
				assert.Equal(t, map[string]any{"echo": "héllo\nworld"}, res.StructuredContent)

				prompt, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: "greet", Arguments: map[string]string{"name": "Ada"}})
				require.NoError(t, err)
				require.Len(t, prompt.Messages, 1)
				assert.Equal(t, "Hello, Ada!", prompt.Messages[0].Content.(*mcp.TextContent).Text)

				// Every request carried the token and the transport headers;
				// all but the handshake carried the negotiated version.
				var methods []string
				for _, r := range f.rec.requests() {
					if r.Path != "/mcp" {
						continue
					}
					methods = append(methods, r.BodyMethod)
					assert.Equal(t, "Bearer "+testToken, r.Authorization)
					assert.Equal(t, "application/json", r.ContentType)
					assert.Contains(t, r.Accept, "application/json")
					assert.Contains(t, r.Accept, "text/event-stream")
					if r.BodyMethod != "initialize" {
						assert.Equal(t, version, r.ProtocolVersion, r.BodyMethod)
					}
					if version >= "2026-07-28" {
						assert.Equal(t, r.BodyMethod, r.MCPMethod)
					}
					switch r.BodyMethod {
					case "tools/call":
						if version >= "2026-07-28" {
							assert.Equal(t, "echo", r.MCPName)
						}
					case "prompts/get":
						if version >= "2026-07-28" {
							assert.Equal(t, "greet", r.MCPName)
						}
					}
				}
				assert.Contains(t, methods, "tools/list")
				assert.Contains(t, methods, "tools/call")
				assert.Contains(t, methods, "prompts/get")
			})
		}
	}
}

func TestBridgeWrongFingerprintFailsTheHandshakeAndSendsNothing(t *testing.T) {
	ca := newTestCA(t, "PiWorker Local CA test")
	// The server presents its CA in the chain, so no download is needed to
	// see that it is not the pinned one.
	impostor := newTestCA(t, "Impostor")
	srv, rec := newTLSServer(t, impostor.serverCert(t, true, "127.0.0.1"), impostor, bearer(testToken, http.NotFoundHandler()))
	endpoint := mustURL(t, srv.URL+"/mcp")
	c, err := newConnector(endpoint, trustOptions{CAFingerprint: ca.fingerprint()})
	require.NoError(t, err)

	b, err := connectThroughBridge(t, relayConfig{Endpoint: endpoint, Token: testToken, Connector: c}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match --ca-fingerprint")
	assert.Empty(t, rec.requests(), "the server never saw a request")
	assert.Contains(t, b.stderr.String(), "does not match --ca-fingerprint", "the reason is logged too")
}

func TestBridgeWrongServerNameFails(t *testing.T) {
	ca := newTestCA(t, "PiWorker Local CA test")
	srv, rec := newTLSServer(t, ca.serverCert(t, true, "piworker.test"), ca, nil)
	c, err := newConnector(mustURL(t, srv.URL+"/mcp"), trustOptions{CAFingerprint: ca.fingerprint()})
	require.NoError(t, err)

	_, err = connectThroughBridge(t, relayConfig{Endpoint: mustURL(t, srv.URL+"/mcp"), Token: testToken, Connector: c}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not valid for 127.0.0.1")
	assert.Empty(t, rec.requests())
}

func TestBridgeCAFilePinning(t *testing.T) {
	f := newSDKFixture(t, true)
	c, err := newConnector(mustURL(t, f.url), trustOptions{CAFile: f.ca.file(t)})
	require.NoError(t, err)
	b, err := connectThroughBridge(t, relayConfig{Endpoint: mustURL(t, f.url), Token: testToken, Connector: c}, "")
	require.NoError(t, err)
	_, err = b.session.ListTools(testCtx(t), nil)
	require.NoError(t, err)

	other := newTestCA(t, "Other CA")
	c, err = newConnector(mustURL(t, f.url), trustOptions{CAFile: other.file(t)})
	require.NoError(t, err)
	before := len(f.rec.requests())
	_, err = connectThroughBridge(t, relayConfig{Endpoint: mustURL(t, f.url), Token: testToken, Connector: c}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not signed by a trusted CA")
	assert.Len(t, f.rec.requests(), before)
}

func TestBridgeWrongTokenIsAClearError(t *testing.T) {
	f := newSDKFixture(t, true)
	cfg := fixtureConfig(t, f)
	cfg.Token = "pwk_wrong"
	_, err := connectThroughBridge(t, cfg, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.Contains(t, err.Error(), "token")
	assert.Zero(t, f.calls.Load())
}

// Cancelling a call aborts its POST. From protocol 2026-07-28 the POST is the
// whole request, so PiWorker (PropagateRequestCancellation) cancels the call;
// before that, the SDK lets the call run to its end and the bridge drops the
// answer. Either way the client is answered at once and the session goes on.
func TestBridgeCancellation(t *testing.T) {
	for _, jsonResponse := range []bool{true, false} {
		for _, version := range []string{"2025-11-25", "2026-07-28"} {
			name := version + "/sse"
			if jsonResponse {
				name = version + "/json"
			}
			t.Run(name, func(t *testing.T) {
				f := newSDKFixture(t, jsonResponse)
				b, err := connectThroughBridge(t, fixtureConfig(t, f), version)
				require.NoError(t, err)

				ctx, cancel := context.WithCancel(context.Background())
				errc := make(chan error, 1)
				go func() {
					_, err := b.session.CallTool(ctx, &mcp.CallToolParams{Name: "slow", Arguments: map[string]any{}})
					errc <- err
				}()
				waitFor(t, f.started, "the slow tool to start")
				cancel()
				require.ErrorIs(t, <-errc, context.Canceled)

				if version >= "2026-07-28" {
					waitFor(t, f.cancelled, "the server to see the cancellation")
				} else {
					select {
					case <-f.cancelled:
						t.Fatal("before 2026-07-28 the call is not cancelled on the server")
					case <-time.After(300 * time.Millisecond):
					}
					f.releaseSlow()
					waitFor(t, f.finished, "the call to run to its end")
				}

				// The session still works, and the dropped answer did not
				// reach the client.
				res, err := b.session.CallTool(testCtx(t), &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"text": "still here"}})
				require.NoError(t, err)
				assert.Equal(t, map[string]any{"echo": "still here"}, res.StructuredContent)
				for _, r := range f.rec.requests() {
					assert.NotEqual(t, "notifications/cancelled", r.BodyMethod, "cancellation is local: the stateless server cannot match it")
				}
			})
		}
	}
}

func TestBridgeExitsWhenTheClientCloses(t *testing.T) {
	f := newSDKFixture(t, true)
	b, err := connectThroughBridge(t, fixtureConfig(t, f), "")
	require.NoError(t, err)
	_, err = b.session.ListTools(testCtx(t), nil)
	require.NoError(t, err)
	require.NoError(t, b.session.Close())
	select {
	case <-b.done:
		assert.NoError(t, b.result())
	case <-time.After(5 * time.Second):
		t.Fatal("the bridge did not exit after stdin closed")
	}
	assert.NotContains(t, b.stderr.String(), testToken, "the token is never logged")
	assert.False(t, strings.Contains(b.stderr.String(), "panic"))
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}
