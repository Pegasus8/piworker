// Package webhook provides a process-wide hub that routes incoming HTTP webhook
// requests to registered handlers. Webhook trigger nodes register a path on the
// hub when they start; the HTTP server forwards matching requests to them. This
// is the bridge that lets trigger nodes (which only receive a Start(ctx, out)
// call) react to external HTTP requests without owning a listener themselves.
package webhook

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/Pegasus8/piworker/internal/node"
)

const (
	// PathPrefix is where webhook endpoints are mounted on the HTTP server.
	PathPrefix = "/webhooks/"
	// maxBodyBytes caps how much of a webhook request body is read.
	maxBodyBytes = 1 << 20
)

// Request is the normalized representation of an incoming webhook HTTP request
// delivered to a registered handler.
//
// Credentials are kept out of Headers and Query, which end up in flow
// payloads (and from there in logs, files, notifications): Cookie,
// Authorization, X-Webhook-Token and X-PiWorker-* headers and the "token"
// query parameter are removed. The webhook token, if any, is passed in Token
// instead (X-Webhook-Token header first, then ?token=).
type Request struct {
	Method     string
	Path       string
	Query      map[string][]string
	Headers    map[string]string
	Body       interface{}
	RemoteAddr string
	Token      string
}

// Result is a handler's verdict on a request; the hub maps it to the HTTP
// status the caller sees.
type Result int

const (
	// Accepted: the request was queued into the flow (200).
	Accepted Result = iota
	// Unauthorized: missing or wrong token (401).
	Unauthorized
	// MethodNotAllowed: the node filters on another method (405).
	MethodNotAllowed
	// Dropped: the flow is stopping or its queue is full (503), so the
	// sender knows to retry instead of assuming delivery.
	Dropped
)

// httpStatus maps a Result to its HTTP status code.
func (r Result) httpStatus() int {
	switch r {
	case Accepted:
		return http.StatusOK
	case Unauthorized:
		return http.StatusUnauthorized
	case MethodNotAllowed:
		return http.StatusMethodNotAllowed
	default:
		return http.StatusServiceUnavailable
	}
}

// Handler processes a delivered webhook request.
type Handler func(Request) Result

// Hub routes incoming webhook requests to handlers keyed by path.
type Hub struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewHub creates an empty Hub.
func NewHub() *Hub {
	return &Hub{handlers: make(map[string]Handler)}
}

// Register binds a handler to a path. It errors if the path is already taken,
// so two flows can't silently fight over the same webhook URL.
func (h *Hub) Register(path string, handler Handler) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.handlers[path]; exists {
		return fmt.Errorf("%w: webhook path %q is already registered", node.ErrResourceConflict, path)
	}
	h.handlers[path] = handler
	return nil
}

// Unregister removes the handler for a path.
func (h *Hub) Unregister(path string) {
	h.mu.Lock()
	delete(h.handlers, path)
	h.mu.Unlock()
}

// Dispatch delivers a request to the handler registered for its path and
// returns its result; ok is false if no handler is registered.
func (h *Hub) Dispatch(path string, req Request) (result Result, ok bool) {
	h.mu.RLock()
	handler, ok := h.handlers[path]
	h.mu.RUnlock()
	if !ok {
		return Dropped, false
	}
	return handler(req), true
}

// Has reports whether a path is registered.
func (h *Hub) Has(path string) bool {
	h.mu.RLock()
	_, ok := h.handlers[path]
	h.mu.RUnlock()
	return ok
}

// strippedHeader reports whether a request header must not reach the flow
// payload because it carries credentials.
func strippedHeader(name string) bool {
	switch strings.ToLower(name) {
	case "cookie", "authorization", "proxy-authorization", "x-webhook-token":
		return true
	}
	return strings.HasPrefix(strings.ToLower(name), "x-piworker-")
}

// HandlerFunc returns an http.HandlerFunc that parses the request and dispatches
// it to the hub based on the path following PathPrefix. The response status
// reflects the outcome: 404 unknown path, 400 unreadable body, 413 body over
// 1 MiB, then the handler's Result (200/401/405/503).
func (h *Hub) HandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(strings.TrimPrefix(r.URL.Path, PathPrefix), "/")
		if path == "" || !h.Has(path) {
			http.Error(w, "no webhook registered for this path", http.StatusNotFound)
			return
		}

		// Parse the body as JSON when possible, otherwise keep it as a string.
		// Read one byte past the limit to tell "exactly max" from "too big":
		// a silently truncated body must not be delivered as if complete.
		var body interface{}
		if r.Body != nil {
			data, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
			if err != nil {
				http.Error(w, "failed to read request body", http.StatusBadRequest)
				return
			}
			if len(data) > maxBodyBytes {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			if len(data) > 0 {
				var parsed interface{}
				if json.Unmarshal(data, &parsed) == nil {
					body = parsed
				} else {
					body = string(data)
				}
			}
		}

		headers := make(map[string]string, len(r.Header))
		for k, v := range r.Header {
			if len(v) > 0 && !strippedHeader(k) {
				headers[k] = v[0]
			}
		}

		query := r.URL.Query()
		token := r.Header.Get("X-Webhook-Token")
		if token == "" {
			token = query.Get("token")
		}
		query.Del("token")

		req := Request{
			Method:     r.Method,
			Path:       path,
			Query:      query,
			Headers:    headers,
			Body:       body,
			RemoteAddr: r.RemoteAddr,
			Token:      token,
		}

		result, ok := h.Dispatch(path, req)
		if !ok { // unregistered while the body was being read
			http.Error(w, "no webhook registered for this path", http.StatusNotFound)
			return
		}
		status := result.httpStatus()
		if status != http.StatusOK {
			http.Error(w, http.StatusText(status), status)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}
}

// DefaultHub is the process-wide hub shared by webhook trigger nodes and the
// HTTP server.
var DefaultHub = NewHub()
