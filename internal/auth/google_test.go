package auth

import (
	"net/url"
	"strings"
	"testing"
)

// TestStartGoogleOAuthRequiresClientID pins the early failure when the
// OAuth client id is not configured.
func TestStartGoogleOAuthRequiresClientID(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "")
	if _, _, err := StartGoogleOAuth(); err == nil {
		t.Fatal("expected an error when GOOGLE_CLIENT_ID is unset")
	}
}

// TestStartGoogleOAuthSplitsStartFromFinish pins the begin/complete
// split: Start returns a well-formed authorize URL without opening a
// browser. The finish func blocks for the flow's two-minute window,
// so it is released without waiting rather than driven to completion.
func TestStartGoogleOAuthSplitsStartFromFinish(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "test-client-id")
	authURL, finish, err := StartGoogleOAuth()
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = finish() }()

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "accounts.google.com" {
		t.Fatalf("authorize host = %q, want accounts.google.com", parsed.Host)
	}
	q := parsed.Query()
	if q.Get("client_id") != "test-client-id" {
		t.Fatalf("client_id = %q, want test-client-id", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "http://localhost:8080/callback" {
		t.Fatalf("redirect_uri = %q, want the loopback callback", q.Get("redirect_uri"))
	}
	if q.Get("code_challenge") == "" {
		t.Fatal("code_challenge missing from authorize URL")
	}
	if q.Get("state") == "" {
		t.Fatal("state missing from authorize URL")
	}
	if !strings.Contains(q.Get("scope"), "userinfo.email") {
		t.Fatalf("scope %q missing userinfo.email", q.Get("scope"))
	}
}
