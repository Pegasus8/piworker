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

// Handler returns an http.Handler that serves the embedded frontend files.
// It implements SPA routing by serving index.html for any path that doesn't
// match a static file.
func Handler() http.Handler {
	// Get the dist subdirectory
	distSubFS, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic("failed to get dist subdirectory: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(distSubFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
