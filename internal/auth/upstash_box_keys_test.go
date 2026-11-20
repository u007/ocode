package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setHomeTree points HOME and every XDG/Windows equivalent at ONE temp tree so
// path resolution is identical on darwin and Linux. A bare t.Setenv("HOME", …)
// would let the auth store resolve its data dir differently per platform. The
// same helper exists in internal/config, internal/server and internal/agent.
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

// resetUpstashStoreForTest isolates the process-wide credential cache at a
// fresh home and drops the cache again on cleanup.
func resetUpstashStoreForTest(t *testing.T) {
	t.Helper()
	setHomeTree(t, t.TempDir())
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
}

func TestUpstashBoxAPIKeyLivesInAuthJSON(t *testing.T) {
	resetUpstashStoreForTest(t)
	const secret = "ubx-test-secret-key-0123456789abcdef"

	if err := SetUpstashBoxAPIKey("ub_1", secret); err != nil {
		t.Fatalf("SetUpstashBoxAPIKey: %v", err)
	}
	got, ok := UpstashBoxAPIKey("ub_1")
	if !ok || got != secret {
		t.Fatalf("UpstashBoxAPIKey = %q ok=%v, want the stored key", got, ok)
	}

	path, err := AuthPath()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read auth.json: %v", err)
	}
	if !strings.Contains(string(raw), secret) {
		t.Fatalf("key not written to auth.json:\n%s", raw)
	}
	if !strings.Contains(string(raw), `"upstash-box:ub_1"`) {
		t.Fatalf("auth.json entry not namespaced under upstash-box:<id>:\n%s", raw)
	}
}

func TestUpstashBoxKeyNamespaceIsNotAProvider(t *testing.T) {
	if !IsUpstashBoxCredentialID(UpstashBoxCredentialID("ub_1")) {
		t.Fatal("namespaced id not recognised")
	}
	if IsUpstashBoxCredentialID("openai") {
		t.Fatal("an LLM provider id must not be treated as an Upstash key")
	}
}

func TestUpstashBoxRemoveAPIKey(t *testing.T) {
	resetUpstashStoreForTest(t)
	if err := SetUpstashBoxAPIKey("ub_2", "ubx-remove-me-0123456789"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveUpstashBoxAPIKey("ub_2"); err != nil {
		t.Fatalf("RemoveUpstashBoxAPIKey: %v", err)
	}
	if _, ok := UpstashBoxAPIKey("ub_2"); ok {
		t.Fatal("key still present after removal")
	}
}
