package auth

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestOpenAIAuthorizeURLRequestsCodexOAuthScopes(t *testing.T) {
	authURL, err := buildOpenAIAuthorizeURL("challenge", "state")
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}

	scopeSet := map[string]bool{}
	for _, scope := range strings.Fields(parsed.Query().Get("scope")) {
		scopeSet[scope] = true
	}
	for _, want := range []string{"openid", "profile", "email", "offline_access"} {
		if !scopeSet[want] {
			t.Fatalf("scope %q missing %s", parsed.Query().Get("scope"), want)
		}
	}
}

// TestStartOpenAIOAuthSplitsStartFromFinish pins the begin/complete
// split: Start returns a well-formed authorize URL and a finish func
// without opening a browser, and a cancelled context makes finish
// bail out instead of blocking on the callback.
func TestStartOpenAIOAuthSplitsStartFromFinish(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	authURL, finish, err := StartOpenAIOAuth(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "bind localhost") {
			t.Skipf("loopback port %d busy: %v", openaiLoopbackPort, err)
		}
		t.Fatal(err)
	}
	t.Cleanup(cancel)

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "auth.openai.com" {
		t.Fatalf("authorize host = %q, want auth.openai.com", parsed.Host)
	}
	q := parsed.Query()
	if q.Get("client_id") != openaiClientID {
		t.Fatalf("client_id = %q, want %q", q.Get("client_id"), openaiClientID)
	}
	if q.Get("redirect_uri") != openaiRedirectURI {
		t.Fatalf("redirect_uri = %q, want %q", q.Get("redirect_uri"), openaiRedirectURI)
	}
	if q.Get("code_challenge") == "" {
		t.Fatal("code_challenge missing from authorize URL")
	}
	if q.Get("state") == "" {
		t.Fatal("state missing from authorize URL")
	}

	cancel()
	if _, err := finish(); err == nil {
		t.Fatal("finish with a cancelled context returned a nil error")
	}
}
