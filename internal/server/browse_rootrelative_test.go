package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The browse-proxy listener's root: a remote project's iframe lives on this
// listener, so a dev-server module import that escaped the
// /api/remote/{host}/browse mount lands here and must be sent back onto it.
func TestBrowseRootRelativeRedirectKeepsRemoteMount(t *testing.T) {
	s := &Server{rl: newRateLimiter()}
	r := httptest.NewRequest("GET", "/node_modules/.vite/deps/react.js?v=1", nil)
	r.Header.Set("Referer", "http://localhost:9999/api/remote/user@realhost/browse/b/tab:x/http/localhost:3000/src/main.tsx")
	w := httptest.NewRecorder()
	s.handleBrowseRootRelative(w, r)
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307; body %q", w.Code, w.Body.String())
	}
	want := "/api/remote/user@realhost/browse/b/tab:x/http/localhost:3000/node_modules/.vite/deps/react.js?v=1"
	if got := w.Header().Get("Location"); got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestBrowseRootRelativeRedirectLocalMount(t *testing.T) {
	s := &Server{rl: newRateLimiter()}
	r := httptest.NewRequest("GET", "/@vite/env", nil)
	r.Header.Set("Referer", "http://localhost:9999/b/tab:x/http/localhost:3000/@vite/client")
	w := httptest.NewRecorder()
	s.handleBrowseRootRelative(w, r)
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307", w.Code)
	}
	if got, want := w.Header().Get("Location"), "/b/tab:x/http/localhost:3000/@vite/env"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestBrowseRootRelativeWithoutRefererIs404(t *testing.T) {
	s := &Server{rl: newRateLimiter()}
	r := httptest.NewRequest("GET", "/@vite/env", nil)
	w := httptest.NewRecorder()
	s.handleBrowseRootRelative(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", ct)
	}
	if body := w.Body.String(); !strings.Contains(body, "without a /b/ Referer: /@vite/env.") {
		t.Fatalf("body does not name the path and cause: %q", body)
	}
}
