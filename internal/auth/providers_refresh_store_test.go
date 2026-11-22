package auth

import (
	"bytes"
	"errors"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeAnthropicRefresh stands in for the anthropic refresh-token exchange. It
// counts calls and returns a rotated refresh token with a far-future expiry. The
// original entry is restored on cleanup.
func fakeAnthropicRefresh(t *testing.T) *int32 {
	t.Helper()
	var calls int32
	prev := providerRefreshers["anthropic"]
	providerRefreshers["anthropic"] = func(string) (Credential, error) {
		atomic.AddInt32(&calls, 1)
		return Credential{
			Kind:         KindOAuth,
			AccessToken:  "new-access",
			RefreshToken: "rt-rotated",
			ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		}, nil
	}
	t.Cleanup(func() { providerRefreshers["anthropic"] = prev })
	return &calls
}

// expiredOAuthForTest is an OAuth credential already past its expiry.
func expiredOAuthForTest(access, refresh string) Credential {
	return Credential{
		Kind:         KindOAuth,
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    time.Now().Add(-time.Minute).Unix(),
	}
}

// TestProfileRefreshWritesProfileStoreAndLeavesBaseIntact: refreshing a profile's
// OAuth credential saves into the profile store and leaves the base credential
// byte-identical in auth.json.
func TestProfileRefreshWritesProfileStoreAndLeavesBaseIntact(t *testing.T) {
	resetCrossProcessStores(t)
	calls := fakeAnthropicRefresh(t)

	base := Credential{
		Kind:         KindOAuth,
		AccessToken:  "base-access",
		RefreshToken: "rt-base",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}
	if err := Set("anthropic", base); err != nil {
		t.Fatalf("seed base Set: %v", err)
	}
	authBefore, err := os.ReadFile(mustAuthPath(t))
	if err != nil {
		t.Fatalf("read auth.json: %v", err)
	}

	if err := SetProfileCredential("work", "anthropic", expiredOAuthForTest("old-access", "rt-profile")); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	tok, ok := OAuthAccessTokenForProfile("work", "anthropic")
	if !ok || tok != "new-access" {
		t.Fatalf("profile refresh did not return the refreshed token (ok=%v)", ok)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("refresher ran %d times, want 1", got)
	}

	stored, ok := GetProfileCredential("work", "anthropic")
	if !ok || stored.RefreshToken != "rt-rotated" {
		t.Error("profile store does not hold the rotated refresh token")
	}
	authAfter, err := os.ReadFile(mustAuthPath(t))
	if err != nil {
		t.Fatalf("read auth.json after: %v", err)
	}
	if !bytes.Equal(authAfter, authBefore) {
		t.Error("a profile refresh changed the base auth.json")
	}
	if now, _ := Get("anthropic"); now.AccessToken != "base-access" {
		t.Error("the base credential was replaced by the profile's token")
	}
}

// TestSecondCallerAfterRefreshDoesNotRefreshAgain: a caller that still holds the
// STALE copy it read before another caller refreshed must reuse the stored result
// instead of spending the rotated refresh token a second time. Covers both stores.
func TestSecondCallerAfterRefreshDoesNotRefreshAgain(t *testing.T) {
	cases := map[string]func(t *testing.T) CredentialStore{
		"base": func(t *testing.T) CredentialStore {
			stale := expiredOAuthForTest("old-access", "rt-base")
			if err := Set("anthropic", stale); err != nil {
				t.Fatalf("seed base: %v", err)
			}
			return baseCredentialStore
		},
		"profile": func(t *testing.T) CredentialStore {
			stale := expiredOAuthForTest("old-access", "rt-profile")
			if err := SetProfileCredential("work", "anthropic", stale); err != nil {
				t.Fatalf("seed profile: %v", err)
			}
			return profileCredentialStore("work")
		},
	}
	for name, seed := range cases {
		t.Run(name, func(t *testing.T) {
			resetCrossProcessStores(t)
			calls := fakeAnthropicRefresh(t)
			stale := expiredOAuthForTest("old-access", "rt-stale")
			store := seed(t)

			first := refreshIfExpiring("anthropic", stale, store)
			if first.AccessToken != "new-access" {
				t.Fatal("first caller did not refresh")
			}
			second := refreshIfExpiring("anthropic", stale, store)
			if second.AccessToken != "new-access" {
				t.Error("second caller with a stale copy did not get the stored refresh")
			}
			if got := atomic.LoadInt32(calls); got != 1 {
				t.Errorf("refresher ran %d times, want 1", got)
			}
		})
	}
}

// TestRefreshDoesNotResurrectRemovedCredential: a caller holding a stale copy must
// not refresh and re-save a profile credential that another caller removed.
func TestRefreshDoesNotResurrectRemovedCredential(t *testing.T) {
	resetCrossProcessStores(t)
	calls := fakeAnthropicRefresh(t)
	stale := expiredOAuthForTest("old-access", "rt-profile")
	if err := SetProfileCredential("work", "anthropic", stale); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	if err := RemoveProfileCredential("work", "anthropic"); err != nil {
		t.Fatalf("RemoveProfileCredential: %v", err)
	}

	got := refreshIfExpiring("anthropic", stale, profileCredentialStore("work"))
	if got.AccessToken != "old-access" {
		t.Errorf("a removed credential was refreshed (access changed)")
	}
	if atomic.LoadInt32(calls) != 0 {
		t.Errorf("refresher ran for a removed credential")
	}
	if _, ok := GetProfileCredential("work", "anthropic"); ok {
		t.Error("the refresh re-saved a removed profile credential")
	}
}

// TestRefreshSaveFailureIsLoggedAndTokenStillReturned: a failed save is logged,
// the refreshed credential still goes back to the caller, and neither the log
// line nor the return value carries a token value.
func TestRefreshSaveFailureIsLoggedAndTokenStillReturned(t *testing.T) {
	resetCrossProcessStores(t)
	fakeAnthropicRefresh(t)

	var buf bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	stale := expiredOAuthForTest("old-access", "rt-stale")
	failing := CredentialStore{
		get: func(string) (Credential, bool) { return stale, true },
		set: func(string, Credential) error { return errors.New("disk full") },
	}

	got := refreshIfExpiring("anthropic", stale, failing)
	if got.AccessToken != "new-access" {
		t.Fatal("the refreshed credential was not returned after a failed save")
	}
	logged := buf.String()
	if !strings.Contains(logged, "save refreshed anthropic credential") {
		t.Error("a failed save was not logged")
	}
	if strings.Contains(logged, "new-access") || strings.Contains(logged, "rt-rotated") {
		t.Error("the save-failure log line carries a token value")
	}
}
