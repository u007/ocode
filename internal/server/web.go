package server

import (
	"io/fs"
	"net/http"
	"strings"
)

// spaHandler serves the embedded React SPA.
// API routes (/api/*) are handled separately; everything else falls through to index.html.
//
// Unknown /api/* paths must NOT fall through to the SPA fallback: serving
// index.html (200, text/html) for an API route makes the SPA's fetchJSON
// try response.json() on HTML, which in WebKit (the desktop WKWebView)
// throws the cryptic "SyntaxError: The string did not match the expected
// pattern" instead of a readable 404 — and it breaks availability probes
// that branch on the status code (e.g. isPortMapsAvailable treats a 200
// HTML response as "the endpoint exists"). Return a JSON 404 for those
// instead so clients get a clean ApiError(404).
func spaHandler(webFS fs.FS) http.Handler {
	if webFS == nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(webFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		// Cache policy. The embed.FS backing this handler reports a zero
		// ModTime for every file, so Go emits no Last-Modified and no ETag —
		// the browser has no validator and would re-download the whole bundle
		// on every page load (slow in the desktop webview, brutal over
		// Tailscale). Vite emits content-hashed filenames under assets/, so
		// those are safe to cache forever; everything else (index.html, the
		// SPA fallback) must revalidate so a new build is picked up at once.
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
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
