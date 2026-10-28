package remote

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// extra_allowed_paths are absolute paths on ONE machine. Pushing them to another
// host injects paths that cannot exist there (macOS /Users/... on Linux) and,
// because WritePayload replaces the whole file, silently revokes every grant the
// user made on that host.
func TestStripMachineLocalConfigRemovesExtraAllowedPaths(t *testing.T) {
	in := []byte(`{"models":{"a":1},"extra_allowed_paths":["/Users/james/www/ocode"],"terminal_font_size":13}`)
	out := stripMachineLocalConfig("ocodeconfig.json", in)

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if _, ok := doc["extra_allowed_paths"]; ok {
		t.Fatalf("extra_allowed_paths survived the strip:\n%s", out)
	}
	for _, key := range []string{"models", "terminal_font_size"} {
		if _, ok := doc[key]; !ok {
			t.Fatalf("key %q was dropped:\n%s", key, out)
		}
	}
}

// The receiver must keep the destination's own grants: a stripped key in the
// frame would otherwise ERASE the host's list and the remote session would ask
// for a path the user already approved there.
func TestSyncRoundTripKeepsHostGrantAndDropsLocalPaths(t *testing.T) {
	cfgDir := t.TempDir()
	restore := overrideSyncDirsForTest(t, t.TempDir(), cfgDir)
	defer restore()

	dest := filepath.Join(cfgDir, "ocodeconfig.json")
	hostDoc := `{"extra_allowed_paths":["/home/james/www/aimsai2"],"models":{"a":1}}`
	if err := os.WriteFile(dest, []byte(hostDoc), 0o600); err != nil {
		t.Fatal(err)
	}

	// Exactly what BuildSyncPayload ships: the local value already stripped.
	sent := stripMachineLocalConfig("ocodeconfig.json",
		[]byte(`{"extra_allowed_paths":["/Users/james/www/ocode"],"models":{"a":1}}`))
	p := SyncPayload{Files: []SyncFile{
		{Dir: SyncDirConfig, Name: "ocodeconfig.json", Content: sent},
	}}
	if err := WritePayload(p); err != nil {
		t.Fatalf("WritePayload: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read %s: %v", dest, err)
	}
	var doc struct {
		ExtraAllowedPaths []string       `json:"extra_allowed_paths"`
		Models            map[string]int `json:"models"`
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("written config is not valid JSON: %v\n%s", err, got)
	}
	if len(doc.ExtraAllowedPaths) != 1 || doc.ExtraAllowedPaths[0] != "/home/james/www/aimsai2" {
		t.Fatalf("host grant not preserved: %v\n%s", doc.ExtraAllowedPaths, got)
	}
	if _, ok := doc.Models["a"]; !ok {
		t.Fatalf("synced content missing:\n%s", got)
	}
	for _, p := range doc.ExtraAllowedPaths {
		if strings.Contains(p, "/Users/") {
			t.Fatalf("local machine path leaked onto the host: %q\n%s", p, got)
		}
	}
}

// A fresh host has no destination config: nothing to preserve, and the frame is
// written unchanged (no synthetic empty list).
func TestWritePayloadFreshHostWritesFrameUnchanged(t *testing.T) {
	cfgDir := t.TempDir()
	restore := overrideSyncDirsForTest(t, t.TempDir(), cfgDir)
	defer restore()

	sent := stripMachineLocalConfig("ocodeconfig.json",
		[]byte(`{"extra_allowed_paths":["/Users/james/www/ocode"],"models":{"a":1}}`))
	p := SyncPayload{Files: []SyncFile{
		{Dir: SyncDirConfig, Name: "ocodeconfig.json", Content: sent},
	}}
	if err := WritePayload(p); err != nil {
		t.Fatalf("WritePayload: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(cfgDir, "ocodeconfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(sent) {
		t.Fatalf("fresh-host content changed:\nwant %q\ngot  %q", sent, got)
	}
}
