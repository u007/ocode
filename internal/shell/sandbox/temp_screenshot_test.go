package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSeatbeltProfileGrantsTempDir locks the OS-boundary half of the
// screenshot-temp guarantee: the central calculation hands os.TempDir() to
// NewRootSet as a writable spec, and the Seatbelt profile must emit it as a
// canonical file-write* subpath rule. Uses the real runtime temp dir so the
// /var/folders/.../T/ → /private/var/folders/.../T/ macOS alias is covered.
func TestSeatbeltProfileGrantsTempDir(t *testing.T) {
	tmp := os.TempDir()
	roots := NewRootSet([]RootSpec{{Path: tmp, Writable: true}})
	profile, err := seatbeltProfileSafe(roots)
	if err != nil {
		t.Fatalf("seatbeltProfileSafe: %v", err)
	}
	canonical, err := filepath.EvalSymlinks(filepath.Clean(tmp))
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", tmp, err)
	}
	want := `(allow file-write* (subpath "` + canonical + `"))`
	if !strings.Contains(profile, want) {
		t.Fatalf("profile missing temp-dir rule %s:\n%s", want, profile)
	}
}

// TestSeatbeltProfileCoversScreenshotShapes locks the exact filename shapes
// ocode/harness screenshots take directly under the temp dir
// (screenshot-<millis>.png, plus a nested capture subdir): every shape must
// sit beneath the canonical temp root the profile grants, whether spelled
// via the /var/... alias or the /private/var/... canonical path.
func TestSeatbeltProfileCoversScreenshotShapes(t *testing.T) {
	tmp := os.TempDir()
	canonical, err := filepath.EvalSymlinks(filepath.Clean(tmp))
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", tmp, err)
	}
	shapes := []string{
		filepath.Join(tmp, "screenshot-1788435661317.png"),
		filepath.Join(canonical, "screenshot-1788435661317.png"),
		filepath.Join(tmp, "captures", "screenshot-1788435661317.png"),
	}
	for _, shape := range shapes {
		resolved, err := filepath.EvalSymlinks(filepath.Dir(shape))
		if err != nil {
			// Parent may not exist (nested shape) — resolve grandparent.
			resolved, err = filepath.EvalSymlinks(tmp)
			if err != nil {
				t.Fatalf("EvalSymlinks(%q): %v", tmp, err)
			}
		}
		if resolved != canonical && !strings.HasPrefix(resolved, canonical+string(filepath.Separator)) {
			t.Fatalf("screenshot shape %q resolves under %q, outside temp root %q", shape, resolved, canonical)
		}
	}
}

// TestSeatbeltProfileDeniesOutsideTemp locks the no-widening half: with only
// the temp dir writable, the profile must grant no write rule covering an
// unrelated root.
func TestSeatbeltProfileDeniesOutsideTemp(t *testing.T) {
	roots := NewRootSet([]RootSpec{{Path: os.TempDir(), Writable: true}})
	profile, err := seatbeltProfileSafe(roots)
	if err != nil {
		t.Fatalf("seatbeltProfileSafe: %v", err)
	}
	outside := string(filepath.Separator) + filepath.Join("definitely-outside-sandbox-roots")
	if strings.Contains(profile, `(subpath "`+outside+`")`) {
		t.Fatalf("profile must not grant outside root %q:\n%s", outside, profile)
	}
	if strings.Contains(profile, `(allow file-write*)`) {
		t.Fatalf("profile must not contain a global file-write* allow:\n%s", profile)
	}
}
