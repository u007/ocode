package server

import (
	"io/fs"
	"net/http"
	"strings"
)

// spaHandler serves the embedded React SPA.
// API routes (/api/*) are handled separately; everything else falls through to index.html.
func spaHandler(webFS fs.FS) http.Handler {
	if webFS == nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(webFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if f, err := webFS.(fs.ReadFileFS).Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for client-side routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
