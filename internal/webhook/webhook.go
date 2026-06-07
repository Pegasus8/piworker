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
)

const (
	// PathPrefix is where webhook endpoints are mounted on the HTTP server.
	PathPrefix = "/api/webhooks/"
	// maxBodyBytes caps how much of a webhook request body is read.
	maxBodyBytes = 1 << 20
)

// Request is the normalized representation of an incoming webhook HTTP request
// delivered to a registered handler.
type Request struct {
	Method     string
	Path       string
	Query      map[string][]string
	Headers    map[string]string
	Body       interface{}
	RemoteAddr string
}

// Handler processes a delivered webhook request.
type Handler func(Request)

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
		return fmt.Errorf("webhook path %q is already registered", path)
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

// Dispatch delivers a request to the handler registered for its path, returning
// false if none is registered.
func (h *Hub) Dispatch(path string, req Request) bool {
	h.mu.RLock()
	handler, ok := h.handlers[path]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	handler(req)
	return true
}

// Has reports whether a path is registered.
func (h *Hub) Has(path string) bool {
	h.mu.RLock()
	_, ok := h.handlers[path]
	h.mu.RUnlock()
	return ok
}

// HandlerFunc returns an http.HandlerFunc that parses the request and dispatches
// it to the hub based on the path following PathPrefix.
func (h *Hub) HandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(strings.TrimPrefix(r.URL.Path, PathPrefix), "/")
		if path == "" {
			http.Error(w, "webhook path required", http.StatusNotFound)
			return
		}

		// Parse the body as JSON when possible, otherwise keep it as a string.
		var body interface{}
		if r.Body != nil {
			data, _ := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
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
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}

		req := Request{
			Method:     r.Method,
			Path:       path,
			Query:      r.URL.Query(),
			Headers:    headers,
			Body:       body,
			RemoteAddr: r.RemoteAddr,
		}

		if !h.Dispatch(path, req) {
			http.Error(w, "no webhook registered for this path", http.StatusNotFound)
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
