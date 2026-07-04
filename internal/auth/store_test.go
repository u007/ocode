package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func resetStoreForTest(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))

	storeMu.Lock()
	cache = nil
	cacheLoaded = false
	storeMu.Unlock()

	t.Cleanup(func() {
		storeMu.Lock()
		cache = nil
		cacheLoaded = false
		storeMu.Unlock()
	})

	return home
}

func TestGetLazyLoadsWithoutDeadlock(t *testing.T) {
	resetStoreForTest(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, ok := Get("openai"); ok {
			t.Errorf("expected no credential")
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Get deadlocked during lazy load")
	}
}

func TestHydrateEnvSkipsOAuthCredentials(t *testing.T) {
	resetStoreForTest(t)
	t.Setenv("OPENAI_API_KEY", "")

	if err := Set("openai", Credential{Kind: KindOAuth, AccessToken: "oauth-token"}); err != nil {
		t.Fatal(err)
	}
	if err := HydrateEnv(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("OPENAI_API_KEY"); got != "" {
		t.Fatalf("OPENAI_API_KEY = %q, want empty", got)
	}
	if got := ResolveKey("openai"); got != "" {
		t.Fatalf("ResolveKey(openai) = %q, want empty for OAuth credential", got)
	}
}

func TestReadLegacyOcodeFormatRejectsFlatJSON(t *testing.T) {
	dst := map[string]Credential{}
	if readLegacyOcodeFormat([]byte(`{"openai":{"kind":"api_key","key":"legacy-key"}}`), dst) {
		t.Fatal("expected flat legacy JSON to be rejected by wrapper parser")
	}
}

func TestRefreshPreservesAccountID(t *testing.T) {
	resetStoreForTest(t)
	cred := Credential{
		Kind:         KindOAuth,
		AccessToken:  "old-access",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(-1 * time.Hour).Unix(),
		AccountID:    "acct-123",
	}
	Set("test-preserve", cred)

	result := refreshIfExpiring("test-preserve", cred)
	if result.AccountID != "acct-123" {
		t.Errorf("expected AccountID preserved, got %q", result.AccountID)
	}
}

func TestConcurrentRefresh(t *testing.T) {
	resetStoreForTest(t)
	cred := Credential{
		Kind:         KindOAuth,
		AccessToken:  "old-access",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(-1 * time.Hour).Unix(),
		AccountID:    "acct-456",
	}
	Set("test-concurrent", cred)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			refreshIfExpiring("test-concurrent", cred)
		}()
	}
	wg.Wait()

	result, ok := Get("test-concurrent")
	if !ok {
		t.Fatal("credential not found after concurrent refresh")
	}
	if result.AccountID != "acct-456" {
		t.Errorf("AccountID lost after concurrent refresh: %q", result.AccountID)
	}
}

// TestOpencodeOAuthFormat verifies that opencode's auth.json format (with
// "access", "refresh", "expires", "accountId" fields) is correctly deserialized.
func TestOpencodeOAuthFormat(t *testing.T) {
	futureMs := time.Now().Add(24 * time.Hour).UnixMilli()
	input := []byte(fmt.Sprintf(`{
		"type": "oauth",
		"access": "eyJ-opencode-access-token",
		"refresh": "rt.1.opencode-refresh-token",
		"expires": %d,
		"accountId": "opencode-acct-id"
	}`, futureMs))

	var cred Credential
	if err := json.Unmarshal(input, &cred); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cred.Kind != KindOAuth {
		t.Errorf("Kind = %q, want %q", cred.Kind, KindOAuth)
	}
	if cred.AccessToken != "eyJ-opencode-access-token" {
		t.Errorf("AccessToken = %q, want %q", cred.AccessToken, "eyJ-opencode-access-token")
	}
	if cred.RefreshToken != "rt.1.opencode-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", cred.RefreshToken, "rt.1.opencode-refresh-token")
	}
	if cred.AccountID != "opencode-acct-id" {
		t.Errorf("AccountID = %q, want %q", cred.AccountID, "opencode-acct-id")
	}
	// expires should be converted from ms to seconds
	expectedSeconds := futureMs / 1000
	if cred.ExpiresAt != expectedSeconds {
		t.Errorf("ExpiresAt = %d, want %d (converted from ms)", cred.ExpiresAt, expectedSeconds)
	}
	// Token should be in the future
	if time.Until(time.Unix(cred.ExpiresAt, 0)) <= 0 {
		t.Errorf("ExpiresAt should be in the future, got %v", time.Unix(cred.ExpiresAt, 0))
	}
}

// TestOcodeOAuthFormat verifies that ocode's own format still works.
func TestOcodeOAuthFormat(t *testing.T) {
	futureSec := time.Now().Add(24 * time.Hour).Unix()
	input := []byte(fmt.Sprintf(`{
		"type": "oauth",
		"access_token": "eyJ-ocode-access-token",
		"refresh_token": "rt.1.ocode-refresh-token",
		"expires_at": %d,
		"account_id": "ocode-acct-id"
	}`, futureSec))

	var cred Credential
	if err := json.Unmarshal(input, &cred); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if cred.Kind != KindOAuth {
		t.Errorf("Kind = %q, want %q", cred.Kind, KindOAuth)
	}
	if cred.AccessToken != "eyJ-ocode-access-token" {
		t.Errorf("AccessToken = %q, want %q", cred.AccessToken, "eyJ-ocode-access-token")
	}
	if cred.RefreshToken != "rt.1.ocode-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", cred.RefreshToken, "rt.1.ocode-refresh-token")
	}
	if cred.AccountID != "ocode-acct-id" {
		t.Errorf("AccountID = %q, want %q", cred.AccountID, "ocode-acct-id")
	}
	if cred.ExpiresAt != futureSec {
		t.Errorf("ExpiresAt = %d, want %d", cred.ExpiresAt, futureSec)
	}
}

func TestFindByBaseURLDeterministicAndNormalized(t *testing.T) {
	resetStoreForTest(t)

	if err := Set("z-provider", Credential{Kind: KindAPIKey, Key: "z-key", BaseURL: "http://localhost:1234/v1/"}); err != nil {
		t.Fatal(err)
	}
	if err := Set("a-provider", Credential{Kind: KindAPIKey, Key: "a-key", BaseURL: "http://localhost:1234"}); err != nil {
		t.Fatal(err)
	}

	cred, ok := FindByBaseURL("http://localhost:1234/v1")
	if !ok {
		t.Fatal("expected credential match")
	}
	if cred.Key != "a-key" {
		t.Fatalf("Key = %q, want %q", cred.Key, "a-key")
	}

	if _, ok := FindByBaseURL("http://localhost:9999/v1"); ok {
		t.Fatal("expected no match for different endpoint")
	}
}
