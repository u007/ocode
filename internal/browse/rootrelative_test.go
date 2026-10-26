package browse

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRootRelativeRedirect(t *testing.T) {
	cases := []struct {
		name, referer, path, query, want string
		ok                               bool
	}{
		{
			name:    "root-mounted module import",
			referer: "http://127.0.0.1:5001/b/tab:x/http/localhost:3000/src/main.tsx",
			path:    "/node_modules/.vite/deps/react.js", query: "v=abc",
			want: "/b/tab:x/http/localhost:3000/node_modules/.vite/deps/react.js?v=abc", ok: true,
		},
		{
			name:    "remote mount is carried over",
			referer: "https://desk.tailnet.ts.net:8443/api/remote/user@host/browse/b/tab:y/http/localhost:3000/@vite/client",
			path:    "/@vite/env",
			want:    "/api/remote/user@host/browse/b/tab:y/http/localhost:3000/@vite/env", ok: true,
		},
		{
			name:    "referer at the route root",
			referer: "http://127.0.0.1:5001/b/tab:x/https/10.0.0.5:8443/",
			path:    "/@fs/Users/me/app/node_modules/x.js",
			want:    "/b/tab:x/https/10.0.0.5:8443/@fs/Users/me/app/node_modules/x.js", ok: true,
		},
		{
			name:    "upstream path containing /b/ does not confuse the split",
			referer: "http://127.0.0.1:5001/b/tab:x/http/localhost:3000/static/b/app.js",
			path:    "/chunk.js",
			want:    "/b/tab:x/http/localhost:3000/chunk.js", ok: true,
		},
		{name: "no referer", referer: "", path: "/x.js"},
		{name: "referer outside the route", referer: "http://127.0.0.1:5001/__ocode_capture.js", path: "/x.js"},
		{name: "foreign referer path", referer: "https://example.com/docs", path: "/x.js"},
		{name: "malformed route in referer", referer: "http://127.0.0.1:5001/b/tab:x/ftp/host/", path: "/x.js"},
		{name: "relative path never redirects", referer: "http://127.0.0.1:5001/b/tab:x/http/h/", path: "x.js"},
		{name: "unparseable referer", referer: "http://[::1", path: "/x.js"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := RootRelativeRedirect(c.referer, c.path, c.query)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v (got %q)", ok, c.ok, got)
			}
			if got != c.want {
				t.Fatalf("loc = %q, want %q", got, c.want)
			}
		})
	}
}

func TestOriginRootRedirectsModuleImportOntoRoute(t *testing.T) {
	s := New("apitoken", nil)
	r := httptest.NewRequest("POST", "/api/items?page=2", nil)
	r.Header.Set("Referer", "http://127.0.0.1:5001/b/tab:x/http/localhost:3000/src/main.tsx")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	// 307, not 302: the method and body of a root-relative POST must survive.
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307; body %q", w.Code, w.Body.String())
	}
	if got, want := w.Header().Get("Location"), "/b/tab:x/http/localhost:3000/api/items?page=2"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestOriginRootWithoutRefererIs404(t *testing.T) {
	s := New("apitoken", nil)
	r := httptest.NewRequest("GET", "/node_modules/.vite/deps/react.js", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestOriginRootWithoutRefererExplainsReferrer(t *testing.T) {
	s := New("apitoken", nil)
	r := httptest.NewRequest("GET", "/node_modules/.vite/deps/react.js", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	want := "root-relative request without a /b/ Referer: /node_modules/.vite/deps/react.js. The embedded browser resolves root-relative dev-server imports from the Referer; if the page sets a no-referrer policy, open it in Chrome mode.\n"
	if got := w.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", ct)
	}
}

func TestOriginRootDoesNotShadowCaptureScript(t *testing.T) {
	s := New("apitoken", nil)
	r := httptest.NewRequest("GET", "/__ocode_capture.js", nil)
	r.Header.Set("Referer", "http://127.0.0.1:5001/b/tab:x/http/localhost:3000/")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code == http.StatusTemporaryRedirect {
		t.Fatalf("capture script must be served by its own route, got a redirect to %q", w.Header().Get("Location"))
	}
}
