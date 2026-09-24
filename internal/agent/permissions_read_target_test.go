package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readTargetArgs(path string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"path": path})
	return b
}

// macOS screenshot filenames use U+202F before AM/PM and a model re-emits it
// as an ASCII space; the read gate must recover the sibling instead of
// hard-denying a file that exists.
func TestDecideReadRecoversUnicodeSpaceTarget(t *testing.T) {
	dir := t.TempDir()
	uploads := filepath.Join(dir, ".ocode", "uploads")
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(uploads, "Screenshot 2026-09-23 at 11.00.05\u202fPM.txt")
	if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	pm := NewPermissionManager()
	pm.SetWorkDir(dir)

	d := pm.Decide("read", readTargetArgs(".ocode/uploads/Screenshot 2026-09-23 at 11.00.05 PM.txt"))
	if d.Level != PermissionAllow {
		t.Fatalf("expected allow via unicode-space recovery, got level=%s hard=%v reason=%q", d.Level, d.HardDeny, d.DenyReason)
	}
}

// A genuinely missing read target is still a hard deny, but the reason must
// name the resolved path and nearby candidates instead of a bare basename.
func TestDecideReadMissingTargetNamesResolvedPathAndCandidates(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	pm := NewPermissionManager()
	pm.SetWorkDir(dir)

	d := pm.Decide("read", readTargetArgs("notes.md"))
	if d.Level != PermissionDeny || !d.HardDeny {
		t.Fatalf("expected hard deny, got level=%s hard=%v", d.Level, d.HardDeny)
	}
	if !strings.Contains(d.DenyReason, "resolved to") {
		t.Fatalf("reason should name the resolved path, got %q", d.DenyReason)
	}
	if !strings.Contains(d.DenyReason, "notes.txt") {
		t.Fatalf("reason should suggest the similar name, got %q", d.DenyReason)
	}
}

// An existing read target is unaffected (no regression to the normal path).
func TestDecideReadExistingTargetAllowed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "present.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	pm := NewPermissionManager()
	pm.SetWorkDir(dir)

	d := pm.Decide("read", readTargetArgs("present.txt"))
	if d.Level != PermissionAllow {
		t.Fatalf("expected allow, got level=%s reason=%q", d.Level, d.DenyReason)
	}
}
