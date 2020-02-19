package backend

import (
	"net/http"
	"github.com/rs/zerolog/log"
)

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tls bool
		if r.TLS != nil {
			tls = true
		}
		log.Info().
			Str("URI", r.RequestURI).
			Str("method", r.Method).
			Str("remoteAddr", r.RemoteAddr).
			Bool("tls", tls).
			Int64("contentLength", r.ContentLength).
			Msg("Request received")
		// Call the next handler, which can be another middleware in the chain, or the final handler.
		next.ServeHTTP(w, r)
	})
}
