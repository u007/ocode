package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAutoShareRoutesAreRegistered drives the REAL mux rather than calling the
// handlers directly.
//
// A handler-level test cannot catch a missing or shadowed route — the failure
// mode that matters here is a 404 from the SPA fallback, which is how the
// terminal-tabs "second browser sees nothing" bug presented. New config routes
// are exactly where that regression hides.
func TestAutoShareRoutesAreRegistered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	isolateConfigDir(t)

	// No username/password: authMiddleware passes requests through, so a
	// non-200 here means the ROUTE is missing, not that auth rejected us.
	srv := New("127.0.0.1:0", "", "", nil)
	h := srv.serveHandler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config/ocode/auto-share", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config/ocode/auto-share = %d body=%s (404 means the route is missing)", rec.Code, rec.Body.String())
	}
	var got autoShareResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v body=%s", err, rec.Body.String())
	}
	if got.Enabled {
		t.Fatal("fresh server must report auto-share off by default")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/config/ocode/auto-share", strings.NewReader(`{"enabled":true}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config/ocode/auto-share = %d body=%s (404 means the route is missing)", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode PUT: %v", err)
	}
	if !got.Enabled {
		t.Fatal("PUT did not enable auto-share")
	}
}

// TestAutoShareRoutesRequireAuth pins that the toggle is not readable or
// writable by an unauthenticated caller.
//
// It changes whether the instance is published to the network, so an open route
// would let anyone on the tailnet turn sharing on. This uses a server with NO
// username/password so authMiddleware is actually exercised rather than
// trivially passing.
func TestAutoShareRoutesRequireAuth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	isolateConfigDir(t)

	srv := New("127.0.0.1:0", "", "tok", nil)
	h := srv.serveHandler()

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/config/ocode/auto-share"},
		{http.MethodPut, "/api/config/ocode/auto-share"},
	} {
		rec := httptest.NewRecorder()
		var body *strings.Reader
		if tc.method == http.MethodPut {
			body = strings.NewReader(`{"enabled":true}`)
		}
		var req *http.Request
		if body != nil {
			req = httptest.NewRequest(tc.method, tc.path, body)
		} else {
			req = httptest.NewRequest(tc.method, tc.path, nil)
		}
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("%s %s returned 200 without a credential; the auto-share toggle must be authenticated", tc.method, tc.path)
		}
	}
}
