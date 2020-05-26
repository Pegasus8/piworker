package backend

import (
	"net"
	"net/http"

	"github.com/Pegasus8/piworker/core/configs"

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

		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			log.Error().Err(err).Msg("Error on the middleware")
			return
		}

		// If the host is on the slice of denied IPs, we must block it.
		configs.CurrentConfigs.RLock()
		for _, blockedIP := range configs.CurrentConfigs.Security.DeniedIPs {
			if blockedIP == host {
				configs.CurrentConfigs.RUnlock()
				w.WriteHeader(http.StatusForbidden)

				log.Info().
					Str("URI", r.RequestURI).
					Str("method", r.Method).
					Str("remoteAddr", r.RemoteAddr).
					Bool("tls", tls).
					Int64("contentLength", r.ContentLength).
					Msg("IP blacklisted. Request rejected")

				return
			}
		}
		configs.CurrentConfigs.RUnlock()

		// Call the next handler, which can be another middleware in the chain, or the final handler.
		next.ServeHTTP(w, r)
	})
}
