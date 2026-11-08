// Package webui provides embedded frontend assets for production deployment.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist/*
var distFS embed.FS

// ContentSecurityPolicy is sent with every web UI response (docs/design/mcp.md
// §9). The editor shows text from agents and flows; even if a rendering bug
// let markup through, no inline, eval'd or foreign script runs, no plugin
// loads, <base> cannot redirect relative URLs and no other site can frame
// the editor. The built UI loads only its own files and talks only to its
// own server; inline styles stay allowed (component libraries set them).
const ContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; " +
	"form-action 'self'; frame-ancestors 'none'"

// Handler returns an http.Handler that serves the embedded frontend files.
// It implements SPA routing by serving index.html for any path that doesn't
// match a static file. Every response carries ContentSecurityPolicy.
func Handler() http.Handler {
	// Get the dist subdirectory
	distSubFS, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic("failed to get dist subdirectory: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(distSubFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", ContentSecurityPolicy)
		path := r.URL.Path

		// Clean the path
		if path == "/" {
			path = "/index.html"
		}

		// Try to open the file
		cleanPath := strings.TrimPrefix(path, "/")
		_, err := fs.Stat(distSubFS, cleanPath)

		if err != nil {
			// File doesn't exist - serve index.html for SPA routing
			r.URL.Path = "/"
		}

		fileServer.ServeHTTP(w, r)
	})
}

// IsAvailable returns true if the embedded frontend is available.
// This will be false during development when the dist folder doesn't exist.
func IsAvailable() bool {
	_, err := fs.Stat(distFS, "dist/index.html")
	return err == nil
}
