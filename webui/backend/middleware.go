package backend

import (
	"net/http"
	"log"
)

func loggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[ URI: '%s' ] [ %s ] Request from: %s, TLS: %v, ContentLenght: %d\n", 
			r.RequestURI, r.Method, r.RemoteAddr, r.TLS, r.ContentLength)
        // Call the next handler, which can be another middleware in the chain, or the final handler.
        next.ServeHTTP(w, r)
    })
}