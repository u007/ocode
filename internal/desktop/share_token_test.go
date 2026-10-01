package desktop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/server"
)

const testLaunchToken = "launch-token"

// shutdownHandle stops a booted server so the next StartServer in the same test
// can reuse the sticky port instead of walking forward through the range.
func shutdownHandle(t *testing.T, h *Handle) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Srv.Shutdown(ctx); err != nil {
		t.Logf("shutdown: %v", err)
	}
}

// startShareTokenServer boots a real listener with the share-token routes
// mounted, so the tests exercise the route registry and authMiddleware rather
// than calling the handlers directly — a missing or unauthenticated mount is
// exactly the failure that would hand the share token to any off-host caller.
func startShareTokenServer(t *testing.T, srv *server.Server, store *ShareTokenStore) string {
	t.Helper()
	srv.HandleAuthedDesktopRoute(ShareTokenPath, store.TokenHandler())
	srv.HandleAuthedDesktopRoute(ShareTokenResetPath, store.ResetHandler())
	ln, err := srv.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	go func() {
		if err := srv.Serve(ln); err != nil {
			t.Logf("serve: %v", err)
		}
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Logf("shutdown: %v", err)
		}
	})
	return "http://" + ln.Addr().String()
}

func shareTokenGET(t *testing.T, base, token string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+ShareTokenPath, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return doTokenReq(t, req)
}

func shareTokenReset(t *testing.T, base, token string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+ShareTokenResetPath, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return doTokenReq(t, req)
}

func doTokenReq(t *testing.T, req *http.Request) (int, string) {
	t.Helper()
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return res.StatusCode, strings.TrimSpace(string(body))
}

func tokenFromBody(t *testing.T, body string) string {
	t.Helper()
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return payload.Token
}

// The point of the feature: the token in a URL already handed to another device
// must keep working after the app restarts, so the SECOND launch must read the
// first launch's token off disk rather than minting a new one.
func TestShareTokenPersistsAcrossLaunches(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", t.TempDir())

	srv1 := server.New("127.0.0.1:0", "ocode", testLaunchToken, nil)
	store1, err := NewShareTokenStore(srv1)
	if err != nil {
		t.Fatalf("NewShareTokenStore: %v", err)
	}
	first, err := store1.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate (first launch): %v", err)
	}
	if !validShareToken(first) {
		t.Fatalf("first token %q is not well-formed", first)
	}

	srv2 := server.New("127.0.0.1:0", "ocode", testLaunchToken, nil)
	store2, err := NewShareTokenStore(srv2)
	if err != nil {
		t.Fatalf("NewShareTokenStore (second launch): %v", err)
	}
	second, err := store2.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate (second launch): %v", err)
	}
	if second != first {
		t.Fatalf("second launch minted %q, want the persisted %q — an already-shared link would break on restart", second, first)
	}
	// The reused token must also be live on the new server, not merely on disk.
	if srv2.ShareToken() != first {
		t.Fatalf("server share token = %q, want %q", srv2.ShareToken(), first)
	}
}

func TestShareTokenFileIsOwnerOnly(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", t.TempDir())
	srv := server.New("127.0.0.1:0", "ocode", testLaunchToken, nil)
	store, err := NewShareTokenStore(srv)
	if err != nil {
		t.Fatalf("NewShareTokenStore: %v", err)
	}
	if _, err := store.LoadOrCreate(); err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatalf("stat %s: %v", store.Path(), err)
	}
	// The file is a live credential, not debug output: anything but 0600 would
	// let another local account read a token that grants full desktop control.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("share token file mode = %04o, want 0600", perm)
	}
}

// A truncated or hand-mangled file must not be adopted: a token nobody can
// authenticate with would silently replace a working link.
func TestShareTokenStoreRegeneratesMalformedFile(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", t.TempDir())
	srv := server.New("127.0.0.1:0", "ocode", testLaunchToken, nil)
	store, err := NewShareTokenStore(srv)
	if err != nil {
		t.Fatalf("NewShareTokenStore: %v", err)
	}
	if err := os.WriteFile(store.Path(), []byte("truncated"), 0o600); err != nil {
		t.Fatalf("seed malformed file: %v", err)
	}
	token, err := store.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if !validShareToken(token) {
		t.Fatalf("token %q is not well-formed after regenerating", token)
	}
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != token {
		t.Fatalf("file holds %q, want the regenerated %q", got, token)
	}
	if srv.ShareToken() != token {
		t.Fatalf("server share token = %q, want %q", srv.ShareToken(), token)
	}
}

func TestShareTokenResetRotatesRevokesAndPersists(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", t.TempDir())
	srv := server.New("127.0.0.1:0", "ocode", testLaunchToken, nil)
	store, err := NewShareTokenStore(srv)
	if err != nil {
		t.Fatalf("NewShareTokenStore: %v", err)
	}
	old, err := store.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	base := startShareTokenServer(t, srv, store)

	// Reading the token needs a credential — the route is credential-bearing,
	// not an open read of a secret.
	if code, _ := shareTokenGET(t, base, ""); code != http.StatusUnauthorized {
		t.Fatalf("GET share-token unauthenticated = %d, want 401", code)
	}
	if code, _ := shareTokenReset(t, base, ""); code != http.StatusUnauthorized {
		t.Fatalf("POST reset unauthenticated = %d, want 401", code)
	}
	if code, body := shareTokenGET(t, base, testLaunchToken); code != http.StatusOK {
		t.Fatalf("GET share-token = %d body=%s, want 200", code, body)
	} else if got := tokenFromBody(t, body); got != old {
		t.Fatalf("GET share-token returned %q, want the live %q", got, old)
	}

	code, body := shareTokenReset(t, base, testLaunchToken)
	if code != http.StatusOK {
		t.Fatalf("POST reset = %d body=%s, want 200", code, body)
	}
	fresh := tokenFromBody(t, body)
	if fresh == old || !validShareToken(fresh) {
		t.Fatalf("reset returned %q (previous was %q)", fresh, old)
	}

	// The revoked link dies at once...
	if code, _ := shareTokenGET(t, base, old); code != http.StatusUnauthorized {
		t.Fatalf("GET with the pre-reset token = %d, want 401 — the shared link was not revoked", code)
	}
	// ...the fresh one works...
	if code, body := shareTokenGET(t, base, fresh); code != http.StatusOK {
		t.Fatalf("GET with the new token = %d body=%s, want 200", code, body)
	}
	// ...and the webview is not logged out by the reset.
	if code, _ := shareTokenGET(t, base, testLaunchToken); code != http.StatusOK {
		t.Fatalf("GET with the launch token after reset = %d, want 200", code)
	}

	// A reset must survive the NEXT restart, or the link it just issued would
	// break while the revoked one still worked.
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read share token file: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != fresh {
		t.Fatalf("file holds %q, want the rotated %q", got, fresh)
	}
	srv3 := server.New("127.0.0.1:0", "ocode", testLaunchToken, nil)
	store3, err := NewShareTokenStore(srv3)
	if err != nil {
		t.Fatalf("NewShareTokenStore (third launch): %v", err)
	}
	third, err := store3.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate (third launch): %v", err)
	}
	if third != fresh {
		t.Fatalf("third launch got %q, want the rotated %q", third, fresh)
	}
}

// authedStatus hits a cheap, fully authed API route and returns its status.
// Used to prove what a SHARED LINK (a token in the query string) can actually
// reach — a share token that the SPA accepts but the API rejects would produce
// a link that loads a login screen.
func authedStatus(t *testing.T, base, path, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+path+"?token="+token, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body)
	return res.StatusCode
}

// StartServer is where the token and its routes get wired together, so a test
// that mounts them by hand would miss a boot.go regression — notably
// registering on the srv that the saved-port fallback then replaces.
func TestStartServerWiresDurableShareToken(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", t.TempDir())

	h1, err := StartServer(nil, t.TempDir(), nil, testCert(t))
	if err != nil {
		t.Fatalf("StartServer (first launch): %v", err)
	}
	shutdownHandle(t, h1)

	code, body := shareTokenGET(t, h1.HTTPURL, h1.Token)
	if code != http.StatusOK {
		t.Fatalf("GET share-token after boot = %d body=%s, want 200 (a 404 means boot.go never mounted the route)", code, body)
	}
	first := tokenFromBody(t, body)
	if first == h1.Token {
		t.Fatal("the durable share token IS the launch token; resetting it would log the webview out")
	}
	if !validShareToken(first) {
		t.Fatalf("share token %q is not well-formed", first)
	}
	if code, _ := shareTokenGET(t, h1.HTTPURL, ""); code != http.StatusUnauthorized {
		t.Fatalf("GET share-token unauthenticated = %d, want 401", code)
	}
	// The whole point of the link: the share token reaches the real API.
	if code := authedStatus(t, h1.HTTPURL, "/api/theme", first); code != http.StatusOK {
		t.Fatalf("API call with the share token = %d, want 200", code)
	}

	// Relaunch over the same config dir: the link must survive.
	h2, err := StartServer(nil, t.TempDir(), nil, testCert(t))
	if err != nil {
		t.Fatalf("StartServer (second launch): %v", err)
	}
	shutdownHandle(t, h2)
	code, body = shareTokenGET(t, h2.HTTPURL, h2.Token)
	if code != http.StatusOK {
		t.Fatalf("GET share-token after relaunch = %d body=%s, want 200", code, body)
	}
	if got := tokenFromBody(t, body); got != first {
		t.Fatalf("relaunch minted %q, want the persisted %q — every already-shared link would break", got, first)
	}
	if code := authedStatus(t, h2.HTTPURL, "/api/theme", first); code != http.StatusOK {
		t.Fatalf("API call with the pre-restart token after relaunch = %d, want 200", code)
	}

	// Reset through the real route: the old link dies, the new one works, and
	// the webview keeps its own credential.
	code, body = shareTokenReset(t, h2.HTTPURL, h2.Token)
	if code != http.StatusOK {
		t.Fatalf("POST reset = %d body=%s, want 200", code, body)
	}
	fresh := tokenFromBody(t, body)
	if fresh == first {
		t.Fatal("reset returned the token it was supposed to replace")
	}
	if code := authedStatus(t, h2.HTTPURL, "/api/theme", first); code != http.StatusUnauthorized {
		t.Fatalf("API call with the revoked token = %d, want 401 — a shared link survived its reset", code)
	}
	if code := authedStatus(t, h2.HTTPURL, "/api/theme", fresh); code != http.StatusOK {
		t.Fatalf("API call with the fresh token = %d, want 200", code)
	}
	if code := authedStatus(t, h2.HTTPURL, "/api/theme", h2.Token); code != http.StatusOK {
		t.Fatalf("API call with the launch token after reset = %d, want 200", code)
	}
}

func TestValidShareToken(t *testing.T) {
	cases := []struct {
		name string
		tok  string
		want bool
	}{
		{"empty", "", false},
		{"not hex", strings.Repeat("z", 32), false},
		{"too short", strings.Repeat("a", 31), false},
		{"too long", strings.Repeat("a", 33), false},
		{"correct length and hex", strings.Repeat("a", server.ShareTokenBytes*2), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validShareToken(tc.tok); got != tc.want {
				t.Fatalf("validShareToken(%q) = %v, want %v", tc.tok, got, tc.want)
			}
		})
	}
}
