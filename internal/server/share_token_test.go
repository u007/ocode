package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestShareTokenIsAcceptedAsASecondCredential(t *testing.T) {
	srv := New("127.0.0.1:0", "ocode", "launch-token", nil)
	srv.SetShareToken("share-token")

	cases := []struct {
		name   string
		method string
		path   string
		auth   string
		want   bool
	}{
		{"launch token via bearer", http.MethodGet, "/x", "Bearer launch-token", true},
		{"share token via bearer", http.MethodGet, "/x", "Bearer share-token", true},
		{"share token via query", http.MethodGet, "/x?token=share-token", "", true},
		{"launch token via query", http.MethodGet, "/x?token=launch-token", "", true},
		{"wrong token", http.MethodGet, "/x", "Bearer nope", false},
		{"no credential", http.MethodGet, "/x", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			if got := srv.checkAuth(req); got != tc.want {
				t.Fatalf("checkAuth(%q, %q) = %v, want %v", tc.path, tc.auth, got, tc.want)
			}
		})
	}
}

// An unset share token must not widen the credential set: with both sides
// empty, a request presenting an empty token would otherwise authenticate.
func TestEmptyShareTokenDoesNotAuthenticateEmptyToken(t *testing.T) {
	srv := New("127.0.0.1:0", "ocode", "launch-token", nil)
	req := httptest.NewRequest(http.MethodGet, "/x?token=", nil)
	if srv.checkAuth(req) {
		t.Fatal("empty token authenticated with no share token configured")
	}
	if srv.ShareToken() != "" {
		t.Fatalf("ShareToken() = %q, want empty on a fresh server", srv.ShareToken())
	}
}

func TestRotateShareTokenReplacesThePreviousValue(t *testing.T) {
	srv := New("127.0.0.1:0", "ocode", "launch-token", nil)
	first, err := srv.RotateShareToken()
	if err != nil {
		t.Fatalf("RotateShareToken: %v", err)
	}
	if len(first) != ShareTokenBytes*2 {
		t.Fatalf("rotated token %q is not %d hex chars", first, ShareTokenBytes*2)
	}
	second, err := srv.RotateShareToken()
	if err != nil {
		t.Fatalf("RotateShareToken (second): %v", err)
	}
	if first == second {
		t.Fatal("RotateShareToken returned the same value twice")
	}
	if srv.ShareToken() != second {
		t.Fatalf("ShareToken() = %q, want the rotated %q", srv.ShareToken(), second)
	}
	// The revoked value must stop working on the very next request; that is
	// the whole point of resetting a shared link.
	if srv.checkAuth(bearerReq(first)) {
		t.Fatal("the pre-rotation share token still authenticates")
	}
	if !srv.checkAuth(bearerReq(second)) {
		t.Fatal("the rotated share token does not authenticate")
	}
	// Rotating must not disturb the webview's own credential.
	if !srv.checkAuth(bearerReq("launch-token")) {
		t.Fatal("rotating the share token logged out the launch token")
	}
}

// HandleDesktopRoute is unauthenticated by design (storage migration), which is
// exactly why credential routes need the authed variant: an off-host caller can
// reach the 0.0.0.0-bound listener with any share URL.
func TestHandleAuthedDesktopRouteRequiresACredential(t *testing.T) {
	srv := New("127.0.0.1:0", "ocode", "launch-token", nil)
	srv.SetShareToken("share-token")
	srv.HandleAuthedDesktopRoute("/api/desktop/share-token", http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
	h := srv.serveHandler()

	cases := []struct {
		name string
		auth string
		want int
	}{
		{"no credential", "", http.StatusUnauthorized},
		{"wrong credential", "Bearer nope", http.StatusUnauthorized},
		{"launch token", "Bearer launch-token", http.StatusOK},
		{"share token", "Bearer share-token", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/desktop/share-token", nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("GET share-token with %q = %d, want %d (a 404 means the route is missing)", tc.auth, rec.Code, tc.want)
			}
		})
	}
}

func bearerReq(token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}
