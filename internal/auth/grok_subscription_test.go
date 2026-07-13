package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGrokSubscriptionLogin_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/app-user/auth" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("x-csrf-token"); got != "ct0val" {
			t.Errorf("x-csrf-token = %q, want ct0val", got)
		}
		if got := r.Header.Get("Cookie"); got != "auth_token=atval; ct0=ct0val" {
			t.Errorf("Cookie = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"sso-token-123","expires_in":3600}`))
	}))
	defer srv.Close()

	orig := GrokAuthURL
	GrokAuthURL = srv.URL + "/rest/app-user/auth"
	defer func() { GrokAuthURL = orig }()

	cred, err := GrokSubscriptionLogin(context.Background(), "atval", "ct0val")
	if err != nil {
		t.Fatalf("GrokSubscriptionLogin: %v", err)
	}
	if cred.Kind != KindOAuth {
		t.Errorf("Kind = %q, want oauth", cred.Kind)
	}
	if cred.AccessToken != "sso-token-123" {
		t.Errorf("AccessToken = %q", cred.AccessToken)
	}
	if cred.BaseURL != GrokSubscriptionBaseURL {
		t.Errorf("BaseURL = %q", cred.BaseURL)
	}
	if cred.CookieAuthToken != "atval" || cred.CookieCt0 != "ct0val" {
		t.Errorf("cookies not stored: %q / %q", cred.CookieAuthToken, cred.CookieCt0)
	}
	if cred.ExpiresAt == 0 {
		t.Errorf("ExpiresAt should be set from expires_in")
	}
}

func TestGrokSubscriptionLogin_AltTokenField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token":"alt-token"}`))
	}))
	defer srv.Close()

	orig := GrokAuthURL
	GrokAuthURL = srv.URL + "/rest/app-user/auth"
	defer func() { GrokAuthURL = orig }()

	cred, err := GrokSubscriptionLogin(context.Background(), "a", "c")
	if err != nil {
		t.Fatalf("GrokSubscriptionLogin: %v", err)
	}
	if cred.AccessToken != "alt-token" {
		t.Errorf("AccessToken = %q, want alt-token", cred.AccessToken)
	}
}

func TestGrokSubscriptionLogin_MissingCookies(t *testing.T) {
	_, err := GrokSubscriptionLogin(context.Background(), "", "ct0val")
	if err == nil {
		t.Fatal("expected error for missing auth_token")
	}
}

func TestGrokSubscriptionLogin_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	orig := GrokAuthURL
	GrokAuthURL = srv.URL + "/rest/app-user/auth"
	defer func() { GrokAuthURL = orig }()

	_, err := GrokSubscriptionLogin(context.Background(), "a", "c")
	if err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestGrokSubscriptionRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"refreshed-token","expires_in":7200}`))
	}))
	defer srv.Close()

	orig := GrokAuthURL
	GrokAuthURL = srv.URL + "/rest/app-user/auth"
	defer func() { GrokAuthURL = orig }()

	cred := Credential{Kind: KindOAuth, CookieAuthToken: "a", CookieCt0: "c"}
	refreshed, err := GrokSubscriptionRefresh(context.Background(), cred)
	if err != nil {
		t.Fatalf("GrokSubscriptionRefresh: %v", err)
	}
	if refreshed.AccessToken != "refreshed-token" {
		t.Errorf("AccessToken = %q", refreshed.AccessToken)
	}
	if refreshed.CookieAuthToken != "a" || refreshed.CookieCt0 != "c" {
		t.Errorf("cookies not preserved: %q / %q", refreshed.CookieAuthToken, refreshed.CookieCt0)
	}
}
