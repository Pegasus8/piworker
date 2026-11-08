package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ctxKey int

const (
	actorKey ctxKey = iota
	serverCtxKey
)

// writeJSONError answers a refused request with PiWorker's error envelope
// ({"success": false, "error": "…"}), never cached.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	data, _ := json.Marshal(struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}{false, msg})
	w.Write(data) //nolint:errcheck // the client went away
}

func setRetryAfter(w http.ResponseWriter, d time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(d.Seconds())))))
}

// loopbackOnly refuses non-loopback clients when PiWorker serves plain HTTP
// and mcp.lan_access is off: tokens would cross the LAN in clear text.
func (h *Handler) loopbackOnly(next http.Handler) http.Handler {
	if h.tls || h.lanAccess {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopback(r.RemoteAddr) {
			writeJSONError(w, http.StatusForbidden,
				"MCP over plain HTTP is limited to this machine. Enable HTTPS (tls.mode = auto) or set mcp.lan_access = true.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Unmap().IsLoopback()
}

// checkHost applies the Host allowlist (ADR-0014). The router-wide guard
// covers /mcp as well; this keeps the endpoint safe on its own.
func (h *Handler) checkHost(next http.Handler) http.Handler {
	if h.hostAllowed == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.hostAllowed(r.Host) {
			writeJSONError(w, http.StatusForbidden, fmt.Sprintf(
				"Host %q is not allowed. To reach PiWorker by this name, add it to allowed_hosts (PIWORKER_ALLOWED_HOSTS or -allowed-hosts).", shorten(r.Host, 100)))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// checkOrigin refuses browser requests from other origins: an Origin header,
// when present, must be this server (same scheme, host and port as the
// request, with an allowed host) or one of the allowed origins.
func (h *Handler) checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && !h.originAllowed(r, origin) {
			writeJSONError(w, http.StatusForbidden, "Request origin is not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) originAllowed(r *http.Request, origin string) bool {
	if h.origins[origin] {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	switch u.Scheme {
	case "https":
	case "http":
		if r.TLS != nil {
			return false // a plain-HTTP page is another origin than this HTTPS server
		}
	default:
		return false
	}
	// Without TLS, PiWorker may sit behind an HTTPS reverse proxy, so both
	// schemes name this server.
	if !strings.EqualFold(withDefaultPort(u.Host, u.Scheme), withDefaultPort(r.Host, requestScheme(r, u.Scheme))) {
		return false
	}
	return h.hostAllowed == nil || h.hostAllowed(u.Host)
}

func requestScheme(r *http.Request, originScheme string) string {
	if r.TLS != nil {
		return "https"
	}
	return originScheme
}

// withDefaultPort adds the scheme's default port to a host without one.
func withDefaultPort(host, scheme string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	port := "80"
	if scheme == "https" {
		port = "443"
	}
	return net.JoinHostPort(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"), port)
}

// normalizeHostname lower-cases a Host value without its port.
func normalizeHostname(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"))
}

// extendWriteDeadline lets the answer take up to d: a tool call may wait 30 s
// for its flow, more than the server's WriteTimeout.
func extendWriteDeadline(d time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(d))
		next.ServeHTTP(w, r)
	})
}

// authenticate accepts only Bearer agent tokens (cookies and query
// parameters are ignored). Failed verifications count against the client's
// address; a client past the limit gets 429 before its token is looked at.
// Tokens are never logged.
func (h *Handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client := clientKey(r.RemoteAddr)
		if blocked, retry := h.failures.blocked(client); blocked {
			setRetryAfter(w, retry)
			writeJSONError(w, http.StatusTooManyRequests, "Too many failed authentications from this address; retry later")
			return
		}
		raw, ok := bearerToken(r)
		if !ok {
			challenge(w, "")
			writeJSONError(w, http.StatusUnauthorized, "An agent token is required: send Authorization: Bearer pwk_…")
			return
		}
		info, err := h.tokens.Verify(raw)
		if errors.Is(err, agentauth.ErrInvalidToken) {
			h.failures.spend(client)
			ev := h.log.Warn().Str("client", client)
			if id, _, parsed := storage.ParseAPIToken(raw); parsed {
				ev = ev.Str("tokenId", id) // the id is not secret; the rest is never logged
			}
			ev.Msg("Rejected an invalid, expired or revoked agent token")
			challenge(w, "invalid_token")
			writeJSONError(w, http.StatusUnauthorized, "The agent token is invalid, expired or revoked")
			return
		}
		if err != nil {
			h.log.Error().Err(err).Msg("Cannot verify agent tokens")
			setRetryAfter(w, 5*time.Second)
			writeJSONError(w, http.StatusServiceUnavailable, "Cannot verify the agent token now; retry later")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey, verified{info: info, actor: service.AgentActor(info)})))
	})
}

// verified is the authenticated caller of a request.
type verified struct {
	info  agentauth.TokenInfo
	actor service.Actor
}

func verifiedFrom(ctx context.Context) (verified, bool) {
	v, ok := ctx.Value(actorKey).(verified)
	return v, ok
}

func challenge(w http.ResponseWriter, errCode string) {
	value := `Bearer realm="piworker"`
	if errCode != "" {
		value += `, error="` + errCode + `"`
	}
	w.Header().Set("WWW-Authenticate", value)
}

// bearerToken reads "Authorization: Bearer <token>" (scheme case-insensitive).
func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

// limitRate applies the per-token request budget.
func (h *Handler) limitRate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, _ := verifiedFrom(r.Context())
		if ok, retry := h.requests.take(v.info.ID); !ok {
			setRetryAfter(w, retry)
			writeJSONError(w, http.StatusTooManyRequests, fmt.Sprintf("Request rate limit of this token reached; retry in %d s", int(math.Ceil(retry.Seconds()))))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// checkScope refuses tokens without a scope the endpoint serves, and tool
// calls naming a tool whose scope the token lacks: a built-in tool's own
// scope, call for any other name (so hidden and unknown flow tools are
// alike). It also refuses what the SDK would serve outside the protocol
// versions /mcp negotiates: JSON-RPC batches (removed in 2025-06-18; they
// would also let one request carry many calls past the per-token rate
// limit), and requests other than initialize without an
// Mcp-Protocol-Version header (the SDK would serve them as 2025-03-26).
// subscriptions/listen is refused (lists never change while a client is
// connected). It then resolves the token's server for the SDK.
//
// The per-token server enforces the same scope rules (it holds no tool of a
// scope the token lacks, and the tool service checks every call), so the
// scope check only answers early with a clear HTTP status. The body is
// parsed strictly (case-sensitive names, no duplicates) like the SDK reads
// it, and a body it cannot parse is refused, so the two never read
// different requests.
func (h *Handler) checkScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, _ := verifiedFrom(r.Context())
		if !h.servesAnyScope(v.actor) {
			writeJSONError(w, http.StatusForbidden, "This token has no scope that /mcp serves (call or read)")
			return
		}
		if r.Method == http.MethodPost && r.Body != nil && !h.checkMessage(w, r, v.actor) {
			return
		}
		srv := h.serverFor(v)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), serverCtxKey, srv)))
	})
}

// checkMessage reads and checks the JSON-RPC message of a POST, answering
// the refusal itself; the body is put back for the SDK.
func (h *Handler) checkMessage(w http.ResponseWriter, r *http.Request, actor service.Actor) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxBody))
	r.Body.Close()
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeJSONError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("The request body exceeds %d bytes", h.maxBody))
		return false
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Cannot read the request body")
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	if isBatch(body) {
		writeRPCError(w, http.StatusBadRequest, nil, jsonrpc.CodeInvalidRequest,
			"JSON-RPC batching is not supported: send one message per request")
		return false
	}
	var m rpcMessage
	if err := jsonv2.Unmarshal(body, &m); err != nil {
		writeRPCError(w, http.StatusBadRequest, nil, jsonrpc.CodeParseError, "Parse error: "+shorten(err.Error(), 200))
		return false
	}
	isCall := len(m.ID) > 0
	switch {
	case isCall && m.Method != "initialize" && r.Header.Get(protocolVersionHeader) == "":
		writeRPCError(w, http.StatusBadRequest, m.ID, jsonrpc.CodeInvalidRequest, fmt.Sprintf(
			"The %s header is required on every request but initialize (supported versions: %s)",
			protocolVersionHeader, strings.Join(ProtocolVersions, ", ")))
		return false
	case m.Method == "subscriptions/listen":
		writeRPCError(w, http.StatusOK, m.ID, jsonrpc.CodeMethodNotFound,
			"subscriptions/listen is not supported: PiWorker's lists do not change while a client is connected (listChanged is false)")
		return false
	case m.Method == "tools/call":
		if scope := h.scopeOfTool(m.Params.Name); !actor.HasScope(scope) {
			writeJSONError(w, http.StatusForbidden, fmt.Sprintf("This token does not have the %s scope needed to call %q", scope, shorten(m.Params.Name, 128)))
			return false
		}
	}
	return true
}

// protocolVersionHeader names the request's MCP protocol version.
const protocolVersionHeader = "Mcp-Protocol-Version"

// rpcMessage is what checkScope reads of a JSON-RPC message.
type rpcMessage struct {
	ID     jsontext.Value `json:"id"`
	Method string         `json:"method"`
	Params struct {
		Name string `json:"name"`
	} `json:"params"`
}

// isBatch reports a JSON array body: a JSON-RPC batch.
func isBatch(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	return len(trimmed) > 0 && trimmed[0] == '['
}

// writeRPCError answers a JSON-RPC error without involving the SDK.
func writeRPCError(w http.ResponseWriter, status int, id jsontext.Value, code int64, msg string) {
	if len(id) == 0 {
		id = jsontext.Value("null")
	}
	data, _ := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   jsonrpc.Error   `json:"error"`
	}{JSONRPC: "2.0", ID: json.RawMessage(id), Error: jsonrpc.Error{Code: code, Message: msg}})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write(data) //nolint:errcheck // the client went away
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// serverFromContext is the SDK's getServer: the server checkScope resolved.
func serverFromContext(r *http.Request) *mcp.Server {
	srv, _ := r.Context().Value(serverCtxKey).(*mcp.Server)
	return srv
}
