package cloudhub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHubClientSendsBearerAndSyncURL pins the two load-bearing facts about the
// hub client: the base URL comes from the sync_url resolution path (never
// backend_url), and the sync bearer is attached as the only Authorization
// header. It drives ListConnections against an httptest hub so no real hub is
// contacted.
func TestHubClientSendsBearerAndSyncURL(t *testing.T) {
	var gotPath, gotAuth, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"provider":"dropbox","connection_id":"c1","root_path":"/Apps","expires_at":"2026-11-18T00:00:00Z"}]`))
	}))
	defer srv.Close()

	oldBase, oldToken := resolveBaseURL, readSyncToken
	resolveBaseURL = func() (string, error) { return srv.URL, nil }
	readSyncToken = func() (string, bool, error) { return "sync-bearer-xyz", true, nil }
	defer func() { resolveBaseURL, readSyncToken = oldBase, oldToken }()

	c, err := NewFromSync()
	if err != nil {
		t.Fatalf("NewFromSync: %v", err)
	}
	conns, err := c.ListConnections(context.Background())
	if err != nil {
		t.Fatalf("ListConnections: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/ocode/cloud/connections" {
		t.Fatalf("path = %q, want /api/ocode/cloud/connections", gotPath)
	}
	if gotAuth != "Bearer sync-bearer-xyz" {
		t.Fatalf("Authorization = %q, want the sync bearer", gotAuth)
	}
	if len(conns) != 1 {
		t.Fatalf("got %d connections, want 1", len(conns))
	}
	want := ConnectionMeta{Provider: "dropbox", ConnectionID: "c1", RootPath: "/Apps"}
	if conns[0].Provider != want.Provider || conns[0].ConnectionID != want.ConnectionID || conns[0].RootPath != want.RootPath {
		t.Fatalf("connection = %+v, want %+v", conns[0], want)
	}
	if conns[0].ExpiresAt.IsZero() {
		t.Fatal("expires_at was not decoded into a time.Time")
	}
}

// TestHubClientMapsNon2xxWithoutLoggingToken pins that a non-2xx becomes a
// typed *HubError carrying the status code, and that the sync bearer never
// leaks into the error string even if the hub echoes the request header back
// in its body.
func TestHubClientMapsNon2xxWithoutLoggingToken(t *testing.T) {
	const token = "super-secret-bearer"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		// Echo the Authorization header to prove the client redacts the token.
		w.Write([]byte(`{"error":"upstream down; got ` + r.Header.Get("Authorization") + `"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, token)
	_, err := c.ListConnections(context.Background())
	if err == nil {
		t.Fatal("expected an error for a 502 response")
	}
	var he *HubError
	if !errors.As(err, &he) {
		t.Fatalf("error type = %T, want *HubError: %v", err, err)
	}
	if he.StatusCode != http.StatusBadGateway {
		t.Fatalf("StatusCode = %d, want 502", he.StatusCode)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("sync bearer leaked into error: %v", err)
	}
}

// TestHubClientReadsLargeConnectionList pins that a successful response is not
// truncated at the error-excerpt limit: a >4 KiB /connections list must decode.
func TestHubClientReadsLargeConnectionList(t *testing.T) {
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < 60; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"provider":"dropbox","connection_id":"conn-`)
		b.WriteString(strings.Repeat("x", 80))
		b.WriteString(`","root_path":"/Apps","expires_at":"2026-11-18T00:00:00Z"}`)
	}
	b.WriteByte(']')
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(b.String()))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	conns, err := c.ListConnections(context.Background())
	if err != nil {
		t.Fatalf("ListConnections on a >4 KiB body: %v", err)
	}
	if len(conns) != 60 {
		t.Fatalf("got %d connections, want 60", len(conns))
	}
}

// TestHubClientRejectsIncompleteResponses pins that an empty auth_url or token
// is an explicit error rather than a silent success.
func TestHubClientRejectsIncompleteResponses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ocode/cloud/oauth/start":
			w.Write([]byte(`{"auth_url":""}`))
		case "/api/ocode/cloud/oauth/redeem":
			w.Write([]byte(`{"connection_id":"","access_token":"","expires_at":"2026-11-18T00:00:00Z"}`))
		default:
			w.Write([]byte(`{"access_token":"","expires_at":"2026-11-18T00:00:00Z"}`))
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "tok")

	if _, err := c.StartOAuth(context.Background(), "dropbox", "c", "cs", "ch"); err == nil {
		t.Fatal("expected error for empty auth_url")
	}
	if _, err := c.Redeem(context.Background(), "code", "ver", "cs"); err == nil {
		t.Fatal("expected error for empty token bundle")
	}
	if _, _, err := c.RefreshToken(context.Background(), "c1"); err == nil {
		t.Fatal("expected error for empty access_token")
	}
}

// TestNewFromSyncFailsWhenSyncURLLoadFails pins that NewFromSync never falls
// back to a default hub when the configured sync_url cannot be resolved.
func TestNewFromSyncFailsWhenSyncURLLoadFails(t *testing.T) {
	oldBase, oldToken := resolveBaseURL, readSyncToken
	resolveBaseURL = func() (string, error) { return "", errors.New("config read failed") }
	readSyncToken = func() (string, bool, error) { return "tok", true, nil }
	defer func() { resolveBaseURL, readSyncToken = oldBase, oldToken }()

	if _, err := NewFromSync(); err == nil {
		t.Fatal("expected error when sync_url cannot be resolved")
	}
}
