package auth

import (
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
