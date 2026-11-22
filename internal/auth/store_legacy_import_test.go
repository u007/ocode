package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// useLegacyFile points opencode's legacy path at a fresh file holding content.
// That is the case the import exists for. setHomeTree cannot make the two paths
// differ on Linux, because AuthPath and the legacy path both resolve from
// XDG_DATA_HOME, so the legacyAuthPath hook is the portable way to do it.
func useLegacyFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}
	legacyAuthPath = func() string { return p }
	t.Cleanup(func() { legacyAuthPath = opencodeLegacyAuthPath })
	return p
}

// sentinelExists reports whether the once-only import marker is on disk.
func sentinelExists(t *testing.T) bool {
	t.Helper()
	_, err := os.Stat(legacySentinelPath(mustAuthPath(t)))
	return err == nil
}

// TestLegacyEntriesImportedOnce: the first load folds the legacy entries that
// auth.json lacks into auth.json, writes the sentinel, and never reads the legacy
// file again. A later legacy edit must not reach the store.
func TestLegacyEntriesImportedOnce(t *testing.T) {
	resetCrossProcessStores(t)
	legacy := useLegacyFile(t, `{"legacy-a": {"type": "api", "key": "sk-legacy-a"}}`)
	if err := writeFileAtomic(mustAuthPath(t), []byte(`{"alpha": {"type": "api", "key": "sk-alpha"}}`)); err != nil {
		t.Fatalf("seed auth.json: %v", err)
	}

	if _, ok := Get("legacy-a"); !ok {
		t.Fatal("legacy entry was not imported on the first load")
	}
	disk := onDiskAuth(t)
	for _, p := range []string{"alpha", "legacy-a"} {
		if _, ok := disk[p]; !ok {
			t.Errorf("auth.json lacks %q after the import", p)
		}
	}
	if !sentinelExists(t) {
		t.Fatal("import wrote no sentinel")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(legacySentinelPath(mustAuthPath(t)))
		if err != nil {
			t.Fatalf("stat sentinel: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("sentinel mode = %o, want 0600", info.Mode().Perm())
		}
	}

	// A legacy file that changes after the import must not be read again.
	if err := os.WriteFile(legacy, []byte(`{"legacy-b": {"type": "api", "key": "sk-legacy-b"}}`), 0o600); err != nil {
		t.Fatalf("rewrite legacy file: %v", err)
	}
	resetAuthCacheForTest()
	if _, ok := Get("legacy-b"); ok {
		t.Error("legacy file was read again after the sentinel existed")
	}
}

// TestRemovedImportedProviderSurvivesRestart: a provider imported from the legacy
// file and then removed must stay removed after a cold reload and after a simulated
// process restart. The removal is the regression H2 describes.
func TestRemovedImportedProviderSurvivesRestart(t *testing.T) {
	resetCrossProcessStores(t)
	useLegacyFile(t, `{"legacy-a": {"type": "api", "key": "sk-legacy-a"}}`)
	mustSet(t, "alpha", "sk-alpha")
	if _, ok := Get("legacy-a"); !ok {
		t.Fatal("legacy entry was not imported")
	}

	mustRemove(t, "legacy-a")

	// A failed reload keeps the cache, and the recovered file must not bring the
	// removed provider back either.
	path := mustAuthPath(t)
	if err := writeFileAtomic(path, []byte(`{"alpha": {"type": "api", "key": `)); err != nil {
		t.Fatalf("write corrupt auth.json: %v", err)
	}
	if _, ok := Get("legacy-a"); ok {
		t.Error("removed legacy provider served while auth.json is corrupt")
	}
	if err := writeFileAtomic(path, []byte(`{"alpha": {"type": "api", "key": "sk-alpha"}}`)); err != nil {
		t.Fatalf("recover auth.json: %v", err)
	}
	if _, ok := Get("legacy-a"); ok {
		t.Error("removed legacy provider came back after the reload recovered")
	}
	if _, ok := onDiskAuth(t)["legacy-a"]; ok {
		t.Error("removed legacy provider is on disk after the reload recovered")
	}

	resetAuthCacheForTest() // a cold load, as a fresh process would do
	if _, ok := Get("legacy-a"); ok {
		t.Error("removed legacy provider came back after a cold reload")
	}
	if _, ok := onDiskAuth(t)["legacy-a"]; ok {
		t.Error("removed legacy provider is on disk after a cold reload")
	}
}

// TestNewerAuthCredentialNotReplacedByLegacy: when auth.json and the legacy file
// both hold a provider, auth.json's credential wins.
func TestNewerAuthCredentialNotReplacedByLegacy(t *testing.T) {
	resetCrossProcessStores(t)
	useLegacyFile(t, `{"shared": {"type": "api", "key": "sk-old-legacy"}}`)
	if err := writeFileAtomic(mustAuthPath(t), []byte(`{"shared": {"type": "api", "key": "sk-new"}}`)); err != nil {
		t.Fatalf("seed auth.json: %v", err)
	}

	cred, ok := Get("shared")
	if !ok {
		t.Fatal("shared provider not readable")
	}
	if cred.Key != "sk-new" {
		t.Error("the legacy credential replaced the newer auth.json credential")
	}
	if onDiskAuth(t)["shared"].Key != "sk-new" {
		t.Error("the legacy credential was written over the newer auth.json credential")
	}
}

// TestNoSentinelWhenLegacyIsAuthJSON: when the legacy path is auth.json itself
// there is nothing to import, so the import does nothing and writes no sentinel.
func TestNoSentinelWhenLegacyIsAuthJSON(t *testing.T) {
	resetCrossProcessStores(t)
	legacyAuthPath = func() string { return mustAuthPath(t) }
	t.Cleanup(func() { legacyAuthPath = opencodeLegacyAuthPath })
	if err := writeFileAtomic(mustAuthPath(t), []byte(`{"alpha": {"type": "api", "key": "sk-alpha"}}`)); err != nil {
		t.Fatalf("seed auth.json: %v", err)
	}

	if _, ok := Get("alpha"); !ok {
		t.Fatal("provider not readable")
	}
	if sentinelExists(t) {
		t.Error("a sentinel was written although the legacy path is auth.json")
	}
}

// TestNoSentinelWhenLegacyAbsent: an absent legacy file imports nothing and writes
// no sentinel, so an opencode login that happens later can still be imported.
func TestNoSentinelWhenLegacyAbsent(t *testing.T) {
	resetCrossProcessStores(t)
	legacyAuthPath = func() string { return filepath.Join(t.TempDir(), "missing-auth.json") }
	t.Cleanup(func() { legacyAuthPath = opencodeLegacyAuthPath })

	mustSet(t, "alpha", "sk-alpha")
	if sentinelExists(t) {
		t.Error("a sentinel was written although the legacy file is absent")
	}
}

// TestUnparseableLegacyIsIgnoredWithoutSentinel: a broken legacy file must not
// stop the store from loading or accepting writes, and is not marked imported.
func TestUnparseableLegacyIsIgnoredWithoutSentinel(t *testing.T) {
	resetCrossProcessStores(t)
	useLegacyFile(t, `{"legacy-a": {"type": "api", "key": `)

	mustSet(t, "alpha", "sk-alpha")
	if _, ok := Get("legacy-a"); ok {
		t.Error("an unparseable legacy file contributed a provider")
	}
	if sentinelExists(t) {
		t.Error("an unparseable legacy file was marked imported")
	}
}
