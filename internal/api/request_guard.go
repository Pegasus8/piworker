package api

import (
	"mime"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

// isSafeMethod reports whether a method is read-only and therefore exempt from
// cross-site request checks.
func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// isGuardedAPIPath reports whether path is part of the editor's own API. Webhook
// endpoints for external callers live outside /api (under /webhooks/), so the
// whole /api tree is guarded with no exceptions.
func isGuardedAPIPath(path string) bool {
	return strings.HasPrefix(path, "/api/")
}

// csrfAllowed verifies that an unsafe request comes from the PiWorker editor:
// it must carry the X-PiWorker-Request header (which a cross-origin page cannot
// set without a CORS preflight) and, when the browser reports an Origin, it must
// be this server or an allowlisted origin.
func csrfAllowed(r *http.Request, secure bool, origins map[string]bool) bool {
	if r.Header.Get("X-PiWorker-Request") != "1" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	scheme := "http"
	if r.TLS != nil || secure {
		scheme = "https"
	}
	return origin == scheme+"://"+r.Host || origins[origin]
}

// hasBody reports whether the request declares (or may carry) a body.
func hasBody(r *http.Request) bool {
	return r.ContentLength != 0 && r.Body != nil && r.Body != http.NoBody
}

// RequestGuardMiddleware enforces the CSRF check and a JSON Content-Type on
// every unsafe /api/ request, independently of whether authentication is
// enabled. Without it a page open in the admin's browser could blind-POST a
// text/plain "simple request" to create and deploy a flow when auth is off.
func RequestGuardMiddleware(secure bool, allowedOrigins []string) mux.MiddlewareFunc {
	origins := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		origins[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSafeMethod(r.Method) || !isGuardedAPIPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			if !csrfAllowed(r, secure, origins) {
				writeError(w, http.StatusForbidden, "Request origin could not be verified")
				return
			}
			if hasBody(r) {
				mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil || mt != "application/json" {
					writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
