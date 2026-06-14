// Package api provides the HTTP REST API for PiWorker.
package api

import (
	"context"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

// RecoveryMiddleware recovers from panics in downstream handlers, logs the panic
// with a stack trace, and returns a structured 500 JSON response instead of a
// dropped/empty connection. Register it as the outermost middleware.
func RecoveryMiddleware(logger zerolog.Logger) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error().
						Interface("panic", rec).
						Str("method", r.Method).
						Str("path", r.URL.Path).
						Bytes("stack", debug.Stack()).
						Msg("Recovered from panic in HTTP handler")
					writeError(w, http.StatusInternalServerError, "Internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// MetricsMiddleware records HTTP request count and latency, labelled by the mux
// route template (not the raw path) to keep Prometheus cardinality bounded.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &ResponseWriter{ResponseWriter: w, StatusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		routeLabel := r.URL.Path
		if route := mux.CurrentRoute(r); route != nil {
			if tmpl, err := route.GetPathTemplate(); err == nil {
				routeLabel = tmpl
			}
		}
		metrics.RecordHTTPRequest(r.Method, routeLabel, wrapped.StatusCode, time.Since(start).Seconds())
	})
}

// ResponseWriter wraps http.ResponseWriter to capture status code.
type ResponseWriter struct {
	http.ResponseWriter
	StatusCode int
}

// WriteHeader captures the status code and delegates to the underlying writer.
func (w *ResponseWriter) WriteHeader(code int) {
	w.StatusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush delegates to the underlying writer's Flusher so streaming handlers
// (Server-Sent Events) keep working when wrapped by logging/metrics middleware.
// Without this, the embedded http.ResponseWriter's Flush is not promoted (the
// interface has no Flush method) and the SSE handler's http.Flusher assertion
// fails.
func (w *ResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// LoggingMiddleware logs all incoming requests.
func LoggingMiddleware(logger zerolog.Logger) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status code
			wrapped := &ResponseWriter{ResponseWriter: w, StatusCode: http.StatusOK}

			next.ServeHTTP(wrapped, r)

			logger.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", wrapped.StatusCode).
				Dur("duration", time.Since(start)).
				Str("remoteAddr", r.RemoteAddr).
				Msg("HTTP request")
		})
	}
}

// corsMiddleware adds CORS headers.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// contentTypeMiddleware sets the default content type to JSON.
func contentTypeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

// SetupMiddleware configures middleware for the server's router.
func (s *Server) SetupMiddleware() {
	s.router.Use(LoggingMiddleware(s.logger))
	s.router.Use(corsMiddleware)
	s.router.Use(contentTypeMiddleware)
}

// StartServer starts the HTTP server with context cancellation support.
func StartServer(ctx context.Context, addr string, handler http.Handler, logger zerolog.Logger) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown on context cancellation
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error().Err(err).Msg("Error during server shutdown")
		}
	}()

	logger.Info().Str("addr", addr).Msg("Starting HTTP server")
	return srv.ListenAndServe()
}
