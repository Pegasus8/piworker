// Package mcpserver serves the /mcp endpoint (docs/design/mcp.md §8,
// ADR-0009, ADR-0010): the Model Context Protocol over Streamable HTTP in
// stateless mode with JSON responses, for AI agents holding an agent token.
//
// Every request passes, in order: a loopback check (TLS off without
// mcp.lan_access), the Host allowlist, an Origin check, a longer write
// deadline, the Bearer token verifier with its own per-client failure
// limiter, a per-token request rate limit and a scope check. The request is
// then served by an *mcp.Server built for that token, holding only the tools
// and prompts it may use now; servers are cached by token, tools snapshot
// version, tokens epoch and token info, so revocations, grant changes and
// redeploys show on the next request. Tool calls go through the tool service
// (internal/service), which re-checks permissions and validates arguments:
// the per-token server only decides what is advertised.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
)

// Path is where the endpoint is mounted.
const Path = "/mcp"

// Defaults of Options.
const (
	// DefaultRequestsPerMinute and DefaultRequestBurst bound each token's
	// requests (§8: 30 a minute, bursts of 10).
	DefaultRequestsPerMinute = 30
	DefaultRequestBurst      = 10
	// DefaultAuthFailuresPerMinute bounds failed authentications per client
	// address; past it the client gets 429 until a slot frees.
	DefaultAuthFailuresPerMinute = 10
	// DefaultMaxServers bounds the cached per-token servers.
	DefaultMaxServers = 32
	// DefaultMaxRequestBodyBytes bounds a request body. A tool call carries
	// arguments of at most 64 KiB in practice (string properties are capped
	// at 256 characters unless the admin raises maxLength, and approvals
	// refuse arguments over 64 KiB), and batches are refused, so 1 MiB
	// leaves room for long strings while keeping a Pi's memory safe from a
	// flood of large bodies (the SDK's default is 4 MiB).
	DefaultMaxRequestBodyBytes = 1 << 20
	// DefaultWriteDeadline covers the longest tool timeout (30 s) plus
	// margin; the server's WriteTimeout (15 s) would cut slow calls.
	DefaultWriteDeadline = 35 * time.Second
	// DefaultListTTL is the ttlMs advertised with list results (cacheScope
	// private): short, so a revoked grant soon disappears from clients.
	DefaultListTTL = 5 * time.Second
	// DefaultResultWait bounds how long pw_get_result waits for a running
	// call before answering "running" again (well inside the write
	// deadline).
	DefaultResultWait = 10 * time.Second
	// maxAuthFailureKeys and maxRateKeys bound the limiters' memory.
	maxAuthFailureKeys = 4096
	maxRateKeys        = 1024
)

// ToolService is the tool service the endpoint adapts (*service.Tools).
type ToolService interface {
	List(actor service.Actor) (version uint64, tools []flow.ToolInfo)
	Call(ctx context.Context, actor service.Actor, ref service.ToolRef, args map[string]any) (flow.CallResult, error)
	Result(ctx context.Context, actor service.Actor, runID string) (flow.CallResult, error)
}

// TokenVerifier verifies agent tokens (*agentauth.TokenVerifier).
type TokenVerifier interface {
	Verify(raw string) (agentauth.TokenInfo, error)
	Epoch() int64
}

// Options configures the endpoint.
type Options struct {
	// Tools serves the published flow tools (required).
	Tools ToolService
	// Tokens verifies Bearer tokens (required).
	Tokens TokenVerifier
	// StaticTools and Prompts are the built-in tools and prompts besides
	// pw_get_result, which is always present.
	StaticTools []StaticTool
	Prompts     []StaticPrompt
	// TLS is set when PiWorker serves HTTPS itself. Without it only loopback
	// clients are served, unless LANAccess (mcp.lan_access).
	TLS       bool
	LANAccess bool
	// HostAllowed is the Host allowlist (api.HostGuard.Allowed); nil allows
	// every Host (the router's guard then applies).
	HostAllowed func(host string) bool
	// AllowedOrigins are browser origins accepted besides this server's own.
	AllowedOrigins []string
	// Version is reported as the server's implementation version.
	Version string
	Logger  zerolog.Logger
	Now     func() time.Time

	// Limits (0 means the default).
	RequestsPerMinute     int
	RequestBurst          int
	AuthFailuresPerMinute int
	MaxServers            int
	MaxRequestBodyBytes   int64
	WriteDeadline         time.Duration
	ListTTL               time.Duration
	ResultWait            time.Duration
}

// Handler serves /mcp.
type Handler struct {
	tools       ToolService
	tokens      TokenVerifier
	tls         bool
	lanAccess   bool
	hostAllowed func(string) bool
	origins     map[string]bool
	version     string
	log         zerolog.Logger
	slog        *slog.Logger
	now         func() time.Time
	maxBody     int64
	listTTL     time.Duration
	resultWait  time.Duration

	static   []*staticTool
	prompts  []StaticPrompt
	requests *buckets
	failures *buckets
	servers  *serverCache
	chain    http.Handler
}

// New builds the endpoint.
func New(opts Options) (*Handler, error) {
	if opts.Tools == nil || opts.Tokens == nil {
		return nil, errors.New("mcpserver: Tools and Tokens are required")
	}
	h := &Handler{
		tools:       opts.Tools,
		tokens:      opts.Tokens,
		tls:         opts.TLS,
		lanAccess:   opts.LANAccess,
		hostAllowed: opts.HostAllowed,
		origins:     map[string]bool{},
		version:     opts.Version,
		log:         opts.Logger.With().Str("component", "mcp").Logger(),
		now:         opts.Now,
		maxBody:     opts.MaxRequestBodyBytes,
		listTTL:     opts.ListTTL,
		resultWait:  opts.ResultWait,
		prompts:     opts.Prompts,
	}
	if h.now == nil {
		h.now = time.Now
	}
	if h.maxBody <= 0 {
		h.maxBody = DefaultMaxRequestBodyBytes
	}
	if h.listTTL <= 0 {
		h.listTTL = DefaultListTTL
	}
	if h.resultWait <= 0 {
		h.resultWait = DefaultResultWait
	}
	if h.version == "" {
		h.version = "dev"
	}
	for _, o := range opts.AllowedOrigins {
		h.origins[o] = true
	}
	h.slog = slog.New(slog.NewTextHandler(sdkLogWriter{h.log}, &slog.HandlerOptions{Level: slog.LevelWarn}))

	static := append([]StaticTool{h.getResultTool()}, opts.StaticTools...)
	seen := map[string]bool{}
	for _, st := range static {
		c, err := compileStaticTool(st)
		if err != nil {
			return nil, err
		}
		if seen[st.Name] {
			return nil, fmt.Errorf("mcpserver: tool %q is registered twice", st.Name)
		}
		seen[st.Name] = true
		h.static = append(h.static, c)
	}
	prompts := map[string]bool{}
	for _, p := range opts.Prompts {
		if p.Prompt == nil || p.Prompt.Name == "" || p.Handler == nil || !slices.Contains(knownScopes, p.Scope) {
			return nil, errors.New("mcpserver: a prompt needs a name, a known scope and a handler")
		}
		if prompts[p.Prompt.Name] {
			return nil, fmt.Errorf("mcpserver: prompt %q is registered twice", p.Prompt.Name)
		}
		prompts[p.Prompt.Name] = true
	}

	rate, burst := orDefault(opts.RequestsPerMinute, DefaultRequestsPerMinute), orDefault(opts.RequestBurst, DefaultRequestBurst)
	h.requests = newBuckets(rate, burst, h.now, maxRateKeys)
	failures := orDefault(opts.AuthFailuresPerMinute, DefaultAuthFailuresPerMinute)
	h.failures = newBuckets(failures, failures, h.now, maxAuthFailureKeys)
	h.servers = newServerCache(orDefault(opts.MaxServers, DefaultMaxServers))

	sdk := mcp.NewStreamableHTTPHandler(serverFromContext, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// PiWorker's Host allowlist and Origin check replace the SDK's
		// loopback-only rebinding protection (ADR-0014).
		DisableLocalhostProtection:   true,
		MaxRequestBodyBytes:          h.maxBody,
		PropagateRequestCancellation: true,
		Logger:                       h.slog,
	})
	writeDeadline := opts.WriteDeadline
	if writeDeadline <= 0 {
		writeDeadline = DefaultWriteDeadline
	}
	h.chain = h.loopbackOnly(h.checkHost(h.checkOrigin(extendWriteDeadline(writeDeadline,
		h.authenticate(h.limitRate(h.checkScope(sdk)))))))
	return h, nil
}

// ServeHTTP serves an MCP request.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.chain.ServeHTTP(w, r)
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// sdkLogWriter forwards the SDK's warnings and errors to the zerolog logger.
type sdkLogWriter struct{ log zerolog.Logger }

func (w sdkLogWriter) Write(p []byte) (int, error) {
	w.log.Warn().Msg(string(trimNewline(p)))
	return len(p), nil
}

func trimNewline(p []byte) []byte {
	for len(p) > 0 && (p[len(p)-1] == '\n' || p[len(p)-1] == '\r') {
		p = p[:len(p)-1]
	}
	return p
}
