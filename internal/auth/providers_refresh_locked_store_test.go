package auth

import (
	"bytes"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// RefreshLocked re-reads and saves through the store it is given, never the base
// store: a profile's credential is refreshed into the profile, and the base
// account's valid credential is neither returned nor rewritten.
func TestRefreshLockedUsesProfileStoreNotBase(t *testing.T) {
	resetCrossProcessStores(t)
	const id = "store-scoped-refresh"
	base := Credential{
		Kind:        KindOAuth,
		AccessToken: "base-valid",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}
	if err := Set(id, base); err != nil {
		t.Fatalf("seed base: %v", err)
	}
	if err := SetProfileCredential("work", id, expiredOAuthForTest("profile-old", "rt-profile")); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	authBefore, err := os.ReadFile(mustAuthPath(t))
	if err != nil {
		t.Fatalf("read auth.json: %v", err)
	}

	var calls int32
	refresh := func() (Credential, error) {
		atomic.AddInt32(&calls, 1)
		return Credential{Kind: KindOAuth, AccessToken: "profile-new", ExpiresAt: time.Now().Add(time.Hour).Unix()}, nil
	}
	got, handled := RefreshLocked(id, expiredOAuthForTest("profile-old", "rt-profile"), profileCredentialStore("work"), refresh)
	if !handled {
		t.Fatal("RefreshLocked returned handled=false")
	}
	if got.AccessToken != "profile-new" {
		t.Errorf("RefreshLocked returned access %q, want the profile's refreshed token", got.AccessToken)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("refresh ran %d times, want 1", n)
	}
	stored, ok := GetProfileCredential("work", id)
	if !ok || stored.AccessToken != "profile-new" {
		t.Error("the refreshed token was not saved into the profile store")
	}
	authAfter, err := os.ReadFile(mustAuthPath(t))
	if err != nil {
		t.Fatalf("read auth.json after: %v", err)
	}
	if !bytes.Equal(authAfter, authBefore) {
		t.Error("a profile refresh changed the base auth.json")
	}
	if now, _ := Get(id); now.AccessToken != "base-valid" {
		t.Error("the base credential was replaced by the profile's token")
	}
}

// A store that holds no OAuth credential for id leaves cred unrefreshed and runs
// no refresh, so a refresh cannot bring a removed credential back, and it never
// falls through to the base store.
func TestRefreshLockedMissingInStoreReturnsCredUnrefreshed(t *testing.T) {
	resetCrossProcessStores(t)
	const id = "store-scoped-missing"
	if err := Set(id, expiredOAuthForTest("base-old", "rt-base")); err != nil {
		t.Fatalf("seed base: %v", err)
	}
	authBefore, err := os.ReadFile(mustAuthPath(t))
	if err != nil {
		t.Fatalf("read auth.json: %v", err)
	}

	var calls int32
	refresh := func() (Credential, error) {
		atomic.AddInt32(&calls, 1)
		return Credential{Kind: KindOAuth, AccessToken: "should-not-happen", ExpiresAt: time.Now().Add(time.Hour).Unix()}, nil
	}
	cred := expiredOAuthForTest("profile-old", "rt-profile")
	got, handled := RefreshLocked(id, cred, profileCredentialStore("work"), refresh)
	if !handled {
		t.Fatal("RefreshLocked returned handled=false")
	}
	if got.AccessToken != "profile-old" {
		t.Errorf("RefreshLocked returned access %q, want the caller's unrefreshed credential", got.AccessToken)
	}
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Errorf("refresh ran %d times for a credential the store does not hold, want 0", n)
	}
	if _, ok := GetProfileCredential("work", id); ok {
		t.Error("a missing profile credential was re-saved")
	}
	authAfter, err := os.ReadFile(mustAuthPath(t))
	if err != nil {
		t.Fatalf("read auth.json after: %v", err)
	}
	if !bytes.Equal(authAfter, authBefore) {
		t.Error("a profile refresh changed the base auth.json")
	}
}
