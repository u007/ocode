package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestClassifiedRootsTempDirWritable locks the screenshot-temp guarantee at
// the central root calculation: os.TempDir() (e.g. /var/folders/.../T/ on
// macOS, where ocode/harness screenshots like screenshot-<ts>.png land) must
// be present and WRITABLE in the capability model consumed by the sandbox
// backend via NewRootSet — no per-path or per-filename special-casing.
func TestClassifiedRootsTempDirWritable(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetWorkDir(t.TempDir())
	want := resolvedForScopeCheckPath(os.TempDir())
	found := false
	for _, spec := range pm.AllowedRootsClassified() {
		if spec.Path == want {
			found = true
			if !spec.Writable {
				t.Fatalf("temp dir %q classified read-only, want writable", spec.Path)
			}
		}
	}
	if !found {
		t.Fatalf("temp dir %q missing from classified roots", want)
	}
}

// TestAllowedRootsIncludesTempDir locks the same guarantee for the flat
// union consumed by the permission prompt and the interpreter verifier.
func TestAllowedRootsIncludesTempDir(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetWorkDir(t.TempDir())
	want := resolvedForScopeCheckPath(os.TempDir())
	for _, root := range pm.AllowedRoots() {
		if root == want {
			return
		}
	}
	t.Fatalf("temp dir %q missing from AllowedRoots %v", want, pm.AllowedRoots())
}

// TestDecideSandboxAllowsScreenshotTempPaths locks promptless operation on
// screenshot-shaped temp paths in sandbox mode: reads, capture-style writes,
// and redirect writes under os.TempDir() auto-allow, while a redirect outside
// all approved roots still asks (no boundary widening).
func TestDecideSandboxAllowsScreenshotTempPaths(t *testing.T) {
	pm := NewPermissionManager()
	// Workdir deliberately outside the temp tree so every Allow below must
	// come from the temp allowance (temp is in the allowed scope and has
	// its own auto-allow rule), never from workdir containment.
	pm.SetWorkDir("/Users/test/project")
	pm.SetMode(PermissionModeSandbox)

	shot := filepath.Join(os.TempDir(), "screenshot-1788435661317.png")
	allow := []string{
		"cat " + shot,
		"screencapture -x " + shot,
		"echo hi > " + filepath.Join(os.TempDir(), "screenshot-out.png"),
	}
	for _, cmd := range allow {
		dec := pm.Decide("bash", json.RawMessage(`{"command":`+strconv.Quote(cmd)+`}`))
		if dec.Level != PermissionAllow {
			t.Fatalf("Decide(%q) = %s, want Allow", cmd, dec.Level)
		}
	}

	// File-tool read of the screenshot path is temp-allowed too.
	dec := pm.Decide("read", json.RawMessage(`{"file_path":`+strconv.Quote(shot)+`}`))
	if dec.Level != PermissionAllow {
		t.Fatalf("Decide(read %q) = %s, want Allow", shot, dec.Level)
	}

	// Same commands must also auto-allow in normal mode via the temp rule
	// (no sandbox blanket-allow involved there).
	pm.SetMode(PermissionModeNormal)
	for _, cmd := range allow {
		dec := pm.Decide("bash", json.RawMessage(`{"command":`+strconv.Quote(cmd)+`}`))
		if dec.Level != PermissionAllow {
			t.Fatalf("normal-mode Decide(%q) = %s, want Allow", cmd, dec.Level)
		}
	}

	// Redirect outside every approved root must NOT auto-allow in normal
	// mode (in sandbox mode the OS boundary, not the prompt, is the
	// enforcement — bash auto-allows there by design and Seatbelt denies).
	outside := string(filepath.Separator) + filepath.Join("definitely-outside-sandbox-roots", "out.png")
	dec = pm.Decide("bash", json.RawMessage(`{"command":"echo hi > `+outside+`"}`))
	if dec.Level == PermissionAllow {
		t.Fatalf("Decide(echo hi > %q) = Allow, want Ask/Deny", outside)
	}
}
