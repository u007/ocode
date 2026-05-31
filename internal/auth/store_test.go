package auth

import (
	"os"
	"path/filepath"
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

func TestLoadStoreMigratesLegacyFlatCredentials(t *testing.T) {
	home := resetStoreForTest(t)
	legacyPath, err := legacyAuthPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	flat := []byte(`{"openai":{"kind":"api_key","key":"flat-legacy-key","base_url":"https://flat.example/v1"}}`)
	if err := os.WriteFile(legacyPath, flat, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := LoadStore(); err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	cred, ok := Get("openai")
	if !ok {
		t.Fatal("expected migrated openai credential")
	}
	if cred.Kind != KindAPIKey {
		t.Fatalf("Kind = %q, want %q", cred.Kind, KindAPIKey)
	}
	if cred.Key != "flat-legacy-key" {
		t.Fatalf("Key = %q, want flat-legacy-key", cred.Key)
	}
	if cred.BaseURL != "https://flat.example/v1" {
		t.Fatalf("BaseURL = %q, want legacy base_url to survive migration", cred.BaseURL)
	}
	newPath, err := authPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("migrated auth file missing at %s: %v", newPath, err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy auth file should be removed after migration, stat err=%v (home=%s)", err, home)
	}
}

func TestLoadStoreMigratesLegacyWrapperCredentials(t *testing.T) {
	resetStoreForTest(t)
	legacyPath, err := legacyAuthPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	wrapper := []byte(`{"credentials":{"openai":{"kind":"api_key","key":"wrapper-legacy-key","base_url":"https://wrapper.example/v1"}}}`)
	if err := os.WriteFile(legacyPath, wrapper, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := LoadStore(); err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	cred, ok := Get("openai")
	if !ok {
		t.Fatal("expected migrated openai credential from wrapper format")
	}
	if cred.Kind != KindAPIKey {
		t.Fatalf("Kind = %q, want %q", cred.Kind, KindAPIKey)
	}
	if cred.Key != "wrapper-legacy-key" {
		t.Fatalf("Key = %q, want wrapper-legacy-key", cred.Key)
	}
	if cred.BaseURL != "https://wrapper.example/v1" {
		t.Fatalf("BaseURL = %q, want wrapper base_url to survive migration", cred.BaseURL)
	}
}
