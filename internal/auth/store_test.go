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
	storeMu.Unlock()

	t.Cleanup(func() {
		storeMu.Lock()
		cache = nil
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
