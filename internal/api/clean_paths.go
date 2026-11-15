package api

import (
	"net/http"
	"path"

	"github.com/gorilla/mux"
)

// CleanPathsAsSent makes router clean request paths as gorilla/mux does (a
// 301 to the canonical path when it has "//", "/./" or "/../", or lacks
// the leading '/'), but judges the path as the client sent it
// (URL.EscapedPath), not decoded. An escaped '/' (%2F) or '.' (%2E) is
// part of a path segment: a conversation key or knowledge document ID the
// editor path-escapes (a URL, "docs/../faq") reaches its route unchanged,
// where mux's own cleaning, on the decoded path, would redirect it to
// another key (and a DELETE would delete another item, or none). Routes
// still match the decoded path, so every handler reads its variables as
// before.
//
// It must be called before any route is registered: the redirect is the
// router's first route. It runs behind the router's middleware (Host
// allowlist, request guard, session), where mux's redirect ran before it.
func CleanPathsAsSent(router *mux.Router) {
	router.SkipClean(true)
	router.MatcherFunc(func(r *http.Request, _ *mux.RouteMatch) bool {
		p := r.URL.EscapedPath()
		return cleanPath(p) != p
	}).HandlerFunc(redirectToCleanPath)
}

// redirectToCleanPath answers 301 to the request's clean path (query
// kept).
func redirectToCleanPath(w http.ResponseWriter, r *http.Request) {
	location := cleanPath(r.URL.EscapedPath())
	if r.URL.RawQuery != "" {
		location += "?" + r.URL.RawQuery
	}
	w.Header().Set("Location", location)
	w.WriteHeader(http.StatusMovedPermanently)
}

// cleanPath is gorilla/mux's: the canonical form of p, with a leading '/'
// and its trailing '/' kept.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	np := path.Clean(p)
	if p[len(p)-1] == '/' && np != "/" {
		np += "/"
	}
	return np
}
