package grok

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/auth"
)

// setHomeTree points HOME and every XDG/Windows equivalent at ONE temp tree, the
// same isolation internal/auth and internal/config use. A bare t.Setenv("HOME", …)
// would let the credential store resolve differently on Linux and macOS.
func setHomeTree(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
}

// grokRefreshEnv isolates the credential store and points the Grok token exchange
// at a local server that answers with a fixed refreshed access token.
func grokRefreshEnv(t *testing.T) {
	t.Helper()
	setHomeTree(t, t.TempDir())
	// The store's cache survives between tests in this binary, and a reload after
	// the HOME change never seeds a missing file. An empty auth.json in this home
	// makes every read below hit a real file, so each assertion is meaningful.
	p, err := auth.AuthPath()
	if err != nil {
		t.Fatalf("AuthPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
		t.Fatalf("seed empty auth.json: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"refreshed-grok","expires_in":7200}`))
	}))
	t.Cleanup(srv.Close)
	orig := auth.GrokAuthURL
	auth.GrokAuthURL = srv.URL + "/rest/app-user/auth"
	t.Cleanup(func() { auth.GrokAuthURL = orig })
}

func grokAuthPath(t *testing.T) string {
	t.Helper()
	p, err := auth.AuthPath()
	if err != nil {
		t.Fatalf("AuthPath: %v", err)
	}
	return p
}

// A profile's expired Grok credential refreshes into the PROFILE store, even when
// the base account holds a valid Grok credential. The base credential is neither
// returned to the profile nor rewritten.
func TestProfileGrokRefreshWritesProfileStoreAndLeavesBaseIntact(t *testing.T) {
	grokRefreshEnv(t)
	base := auth.Credential{
		Kind:            auth.KindOAuth,
		AccessToken:     "base-valid",
		ExpiresAt:       time.Now().Add(time.Hour).Unix(),
		CookieAuthToken: "base-at",
		CookieCt0:       "base-ct",
	}
	if err := auth.Set("grok", base); err != nil {
		t.Fatalf("seed base: %v", err)
	}
	profile := auth.Credential{
		Kind:            auth.KindOAuth,
		AccessToken:     "profile-old",
		ExpiresAt:       time.Now().Add(-time.Minute).Unix(),
		CookieAuthToken: "profile-at",
		CookieCt0:       "profile-ct",
	}
	if err := auth.SetProfileCredential("work", "grok", profile); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	authBefore, err := os.ReadFile(grokAuthPath(t))
	if err != nil {
		t.Fatalf("read auth.json: %v", err)
	}

	tok, ok := auth.OAuthAccessTokenForProfile("work", "grok")
	if !ok || tok != "refreshed-grok" {
		t.Fatalf("profile refresh returned ok=%v token mismatch; want the refreshed token", ok)
	}
	stored, ok := auth.GetProfileCredential("work", "grok")
	if !ok || stored.AccessToken != "refreshed-grok" {
		t.Error("the profile store does not hold the refreshed Grok token")
	}
	if stored.CookieAuthToken != "profile-at" || stored.CookieCt0 != "profile-ct" {
		t.Error("the profile's refresh dropped its session cookies")
	}
	authAfter, err := os.ReadFile(grokAuthPath(t))
	if err != nil {
		t.Fatalf("read auth.json after: %v", err)
	}
	if !bytes.Equal(authAfter, authBefore) {
		t.Error("a profile Grok refresh changed the base auth.json")
	}
	if now, _ := auth.Get("grok"); now.AccessToken != "base-valid" {
		t.Error("the base Grok credential was replaced by the profile's token")
	}
}

// When the base account holds no Grok credential, the profile's refreshed token
// goes into the profile store only. It must not be written into base auth.json.
func TestProfileGrokRefreshWithoutBaseLeavesBaseUntouched(t *testing.T) {
	grokRefreshEnv(t)
	profile := auth.Credential{
		Kind:            auth.KindOAuth,
		AccessToken:     "profile-old",
		ExpiresAt:       time.Now().Add(-time.Minute).Unix(),
		CookieAuthToken: "profile-at",
		CookieCt0:       "profile-ct",
	}
	if err := auth.SetProfileCredential("work", "grok", profile); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	tok, ok := auth.OAuthAccessTokenForProfile("work", "grok")
	if !ok || tok != "refreshed-grok" {
		t.Fatalf("profile refresh returned ok=%v token mismatch; want the refreshed token", ok)
	}
	stored, ok := auth.GetProfileCredential("work", "grok")
	if !ok || stored.AccessToken != "refreshed-grok" {
		t.Error("the profile store does not hold the refreshed Grok token")
	}
	if _, ok := auth.Get("grok"); ok {
		t.Error("the profile refresh created a base Grok credential")
	}
	data, err := os.ReadFile(grokAuthPath(t))
	if err != nil {
		t.Fatalf("read auth.json: %v", err)
	}
	if strings.Contains(string(data), "grok") || strings.Contains(string(data), "refreshed-grok") {
		t.Error("the profile's Grok token leaked into base auth.json")
	}
}

// The base store's Grok refresh is unchanged: an expired base credential is
// refreshed and saved back into the base store.
func TestBaseGrokRefreshUnchanged(t *testing.T) {
	grokRefreshEnv(t)
	base := auth.Credential{
		Kind:            auth.KindOAuth,
		AccessToken:     "base-old",
		ExpiresAt:       time.Now().Add(-time.Minute).Unix(),
		CookieAuthToken: "base-at",
		CookieCt0:       "base-ct",
	}
	if err := auth.Set("grok", base); err != nil {
		t.Fatalf("seed base: %v", err)
	}

	tok, ok := auth.OAuthAccessToken("grok")
	if !ok || tok != "refreshed-grok" {
		t.Fatalf("base refresh returned ok=%v token mismatch; want the refreshed token", ok)
	}
	stored, ok := auth.Get("grok")
	if !ok || stored.AccessToken != "refreshed-grok" {
		t.Error("the base store does not hold the refreshed Grok token")
	}
	if stored.CookieAuthToken != "base-at" || stored.CookieCt0 != "base-ct" {
		t.Error("the base refresh dropped its session cookies")
	}
}
