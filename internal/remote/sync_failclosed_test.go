package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A destination that cannot be parsed must ABORT the write, not be replaced.
// Writing the frame verbatim is precisely the data loss this feature exists to
// prevent: the host's `extra_allowed_paths` grants would be gone, and the remote
// session would start asking for paths the user already approved.
func TestWritePayloadRefusesToOverwriteCorruptDestination(t *testing.T) {
	cfgDir := t.TempDir()
	restore := overrideSyncDirsForTest(t, t.TempDir(), cfgDir)
	defer restore()

	dest := filepath.Join(cfgDir, "ocodeconfig.json")
	corrupt := `{"extra_allowed_paths":["/home/host/grant"],` // truncated JSON
	if err := os.WriteFile(dest, []byte(corrupt), 0o600); err != nil {
		t.Fatal(err)
	}
	sent := stripMachineLocalConfig("ocodeconfig.json",
		[]byte(`{"extra_allowed_paths":["/Users/local/only"],"models":{"a":1}}`))
	p := SyncPayload{Files: []SyncFile{
		{Dir: SyncDirConfig, Name: "ocodeconfig.json", Content: sent},
	}}

	err := WritePayload(p)
	if err == nil {
		t.Fatal("expected WritePayload to refuse a corrupt destination")
	}
	if !strings.Contains(err.Error(), "ocodeconfig.json") {
		t.Fatalf("error should name the file, got %v", err)
	}

	got, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != corrupt {
		t.Fatalf("destination was modified despite the refusal:\nwant %q\ngot  %q", corrupt, got)
	}
}

// The same contract for an unreadable destination (a directory in the file's
// place), which cannot be preserved either.
func TestWritePayloadRefusesUnreadableDestination(t *testing.T) {
	cfgDir := t.TempDir()
	restore := overrideSyncDirsForTest(t, t.TempDir(), cfgDir)
	defer restore()

	dest := filepath.Join(cfgDir, "ocodeconfig.json")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	sent := stripMachineLocalConfig("ocodeconfig.json",
		[]byte(`{"extra_allowed_paths":["/Users/local/only"],"models":{"a":1}}`))
	p := SyncPayload{Files: []SyncFile{
		{Dir: SyncDirConfig, Name: "ocodeconfig.json", Content: sent},
	}}

	if err := WritePayload(p); err == nil {
		t.Fatal("expected WritePayload to refuse an unreadable destination")
	}
	info, err := os.Stat(dest)
	if err != nil || !info.IsDir() {
		t.Fatalf("destination was replaced: %v %v", info, err)
	}
}

// An EMPTY frame carries no JSON object at all, so it must not erase the host's
// file. The old early return took the empty-content case before reading the
// destination, writing the 0-byte frame over the host's config and wiping
// `extra_allowed_paths` exactly like the original bug this feature fixes.
func TestWritePayloadEmptyFrameKeepsDestination(t *testing.T) {
	cfgDir := t.TempDir()
	restore := overrideSyncDirsForTest(t, t.TempDir(), cfgDir)
	defer restore()

	dest := filepath.Join(cfgDir, "ocodeconfig.json")
	existing := `{"extra_allowed_paths":["/home/host/grant"]}`
	if err := os.WriteFile(dest, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	p := SyncPayload{Files: []SyncFile{
		{Dir: SyncDirConfig, Name: "ocodeconfig.json", Content: nil},
	}}

	if err := WritePayload(p); err != nil {
		t.Fatalf("empty frame should preserve the destination, got error: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Fatalf("empty frame erased the destination:\nwant %q\ngot  %q", existing, got)
	}
}

// `null` unmarshals into a nil map with NO error, so the merge loop assigned
// into it and panicked ("assignment to entry in nil map") whenever the host
// held the key — failing every remote connect. It must keep the destination.
func TestWritePayloadNullFrameKeepsDestination(t *testing.T) {
	cfgDir := t.TempDir()
	restore := overrideSyncDirsForTest(t, t.TempDir(), cfgDir)
	defer restore()

	dest := filepath.Join(cfgDir, "ocodeconfig.json")
	existing := `{"extra_allowed_paths":["/home/host/grant"]}`
	if err := os.WriteFile(dest, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	p := SyncPayload{Files: []SyncFile{
		{Dir: SyncDirConfig, Name: "ocodeconfig.json", Content: []byte("null")},
	}}

	if err := WritePayload(p); err != nil {
		t.Fatalf("null frame should preserve the destination, got error: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Fatalf("null frame erased the destination:\nwant %q\ngot  %q", existing, got)
	}
}

// A corrupt FRAME is the LOCAL config's problem. Keeping the destination
// protects the host's grants and, unlike a hard error, does not abort the whole
// sync — which would make one bad local config brick every remote connect
// (`RemoteWorkspace.Connect` treats a sync error as fatal). This mirrors
// `stripMachineLocalConfig`'s documented policy that failing the sync over a
// config that does not round-trip through JSON "would be a regression".
func TestWritePayloadCorruptFrameKeepsDestination(t *testing.T) {
	cfgDir := t.TempDir()
	restore := overrideSyncDirsForTest(t, t.TempDir(), cfgDir)
	defer restore()

	dest := filepath.Join(cfgDir, "ocodeconfig.json")
	existing := `{"extra_allowed_paths":["/home/host/grant"]}`
	if err := os.WriteFile(dest, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	p := SyncPayload{Files: []SyncFile{
		{Dir: SyncDirConfig, Name: "ocodeconfig.json", Content: []byte(`{"extra_allowed_paths":[`)},
	}}

	if err := WritePayload(p); err != nil {
		t.Fatalf("corrupt frame should keep the destination, got error: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Fatalf("corrupt frame erased the destination:\nwant %q\ngot  %q", existing, got)
	}
}
