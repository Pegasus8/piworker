package backend

import (
	"log"
	"net/http"
)

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tls bool
		if r.TLS != nil {
			tls = true
		}
		log.Printf("[ URI: '%s' ] [ %s ] Request from: %s, TLS: %t, ContentLenght: %d\n",
			r.RequestURI, r.Method, r.RemoteAddr, tls, r.ContentLength)
		// Call the next handler, which can be another middleware in the chain, or the final handler.
		next.ServeHTTP(w, r)
	})
}
