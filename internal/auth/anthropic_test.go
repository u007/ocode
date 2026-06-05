package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAnthropicExchangeUsesFormEncoding(t *testing.T) {
	assertAnthropicTokenRequest(t, func() (Credential, error) {
		return AnthropicExchange("code123", "state123", "verifier123")
	}, map[string]string{
		"grant_type":    "authorization_code",
		"code":          "code123",
		"state":         "state123",
		"client_id":     anthropicClientID,
		"redirect_uri":  anthropicRedirectURI,
		"code_verifier": "verifier123",
	})
}

func TestAnthropicRefreshUsesFormEncoding(t *testing.T) {
	assertAnthropicTokenRequest(t, func() (Credential, error) {
		return AnthropicRefresh("refresh123")
	}, map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": "refresh123",
		"client_id":     anthropicClientID,
	})
}

func assertAnthropicTokenRequest(t *testing.T, call func() (Credential, error), wantFields map[string]string) {
	t.Helper()

	var gotMethod, gotContentType, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"access","refresh_token":"refresh","expires_in":3600}`)
	}))
	t.Cleanup(server.Close)

	oldURL := anthropicTokenURL
	oldClient := anthropicHTTPClient
	anthropicTokenURL = server.URL
	anthropicHTTPClient = server.Client()
	t.Cleanup(func() {
		anthropicTokenURL = oldURL
		anthropicHTTPClient = oldClient
	})

	cred, err := call()
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if cred.AccessToken != "access" || cred.RefreshToken != "refresh" {
		t.Fatalf("unexpected credential: %+v", cred)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("expected form content type, got %q", gotContentType)
	}

	vals, err := url.ParseQuery(strings.TrimSpace(gotBody))
	if err != nil {
		t.Fatalf("parse form body: %v", err)
	}
	for key, want := range wantFields {
		if got := vals.Get(key); got != want {
			t.Fatalf("%s = %q, want %q (body=%q)", key, got, want, gotBody)
		}
	}
}
