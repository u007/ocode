package remote

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setHomeTree isolates every path this package resolves through HOME/XDG for the
// duration of one test. internal/remote has no package-wide TestMain isolation
// (unlike internal/config), so the test must move all the variables itself:
// paths.GlobalConfigDir ignores XDG_CONFIG_HOME on darwin but honours it
// elsewhere, and OcodeGlobalDataDir does the mirror image for XDG_DATA_HOME, so
// setting only HOME lands in the shared directory on Linux.
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

// The full pipeline, not just the two transforms: BuildSyncPayload reads a local
// config carrying macOS paths, and WritePayload applies that frame to a host
// config that already holds its own grant. The local list must not arrive and
// the host grant must survive the full-file replacement.
func TestBuildSyncPayloadRoundTripPreservesHostGrant(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)

	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	localCfg := `{"extra_allowed_paths":["/Users/local/only"],"models":{"a":1}}`
	if err := os.WriteFile(filepath.Join(cfgDir, "ocodeconfig.json"), []byte(localCfg), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := globalConfigDirFn
	globalConfigDirFn = func() (string, error) { return cfgDir, nil }
	t.Cleanup(func() { globalConfigDirFn = orig })

	payload, err := BuildSyncPayload()
	if err != nil {
		t.Fatalf("BuildSyncPayload: %v", err)
	}
	var sent []byte
	for _, f := range payload.Files {
		if f.Dir == SyncDirConfig && f.Name == "ocodeconfig.json" {
			sent = f.Content
		}
	}
	if sent == nil {
		t.Fatal("payload has no ocodeconfig.json entry")
	}
	if bytes.Contains(sent, []byte("extra_allowed_paths")) {
		t.Fatalf("sender shipped the local path list:\n%s", sent)
	}
	if !bytes.Contains(sent, []byte(`"models"`)) {
		t.Fatalf("sender dropped an unrelated key:\n%s", sent)
	}

	destDir := t.TempDir()
	restore := overrideSyncDirsForTest(t, t.TempDir(), destDir)
	defer restore()
	dest := filepath.Join(destDir, "ocodeconfig.json")
	hostCfg := `{"extra_allowed_paths":["/home/host/grant"],"models":{"a":1}}`
	if err := os.WriteFile(dest, []byte(hostCfg), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := WritePayload(payload); err != nil {
		t.Fatalf("WritePayload: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		ExtraAllowedPaths []string       `json:"extra_allowed_paths"`
		Models            map[string]int `json:"models"`
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("written config is not valid JSON: %v\n%s", err, got)
	}
	if len(doc.ExtraAllowedPaths) != 1 || doc.ExtraAllowedPaths[0] != "/home/host/grant" {
		t.Fatalf("host grant not preserved through the round trip: %v\n%s", doc.ExtraAllowedPaths, got)
	}
	if strings.Contains(string(got), "/Users/local/only") {
		t.Fatalf("local path reached the host:\n%s", got)
	}
	if _, ok := doc.Models["a"]; !ok {
		t.Fatalf("synced content missing:\n%s", got)
	}
}

// A destination that exists but has no extra_allowed_paths key must simply stay
// without one: the preservation step must not synthesise an empty list (which
// would look like a deliberate revoke to anything that treats presence as
// authoritative).
func TestWritePayloadDoesNotSynthesiseMissingExtraAllowedPaths(t *testing.T) {
	cfgDir := t.TempDir()
	restore := overrideSyncDirsForTest(t, t.TempDir(), cfgDir)
	defer restore()

	dest := filepath.Join(cfgDir, "ocodeconfig.json")
	if err := os.WriteFile(dest, []byte(`{"models":{"a":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sent := stripMachineLocalConfig("ocodeconfig.json",
		[]byte(`{"extra_allowed_paths":["/Users/local/only"],"models":{"a":2}}`))
	p := SyncPayload{Files: []SyncFile{
		{Dir: SyncDirConfig, Name: "ocodeconfig.json", Content: sent},
	}}
	if err := WritePayload(p); err != nil {
		t.Fatalf("WritePayload: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "extra_allowed_paths") {
		t.Fatalf("a missing key must stay missing:\n%s", got)
	}
}
