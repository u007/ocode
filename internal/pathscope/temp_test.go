package pathscope

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTempRootsForGOOS(t *testing.T) {
	got := TempRootsForGOOS("windows")
	wantTemp := normalizeRoot(filepath.Clean(os.TempDir()))
	if len(got) != 1 || got[0] != wantTemp {
		t.Fatalf("windows roots = %v, want [%q]", got, wantTemp)
	}

	got = TempRootsForGOOS("linux")
	wantRoots := map[string]bool{
		normalizeRoot(filepath.Clean(os.TempDir())): true,
		normalizeRoot("/tmp"):                       true,
		normalizeRoot("/var/tmp"):                   true,
	}
	for want := range wantRoots {
		found := false
		for _, gotRoot := range got {
			if gotRoot == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("linux roots = %v, missing %q", got, want)
		}
	}
}

func TestIsTempDir(t *testing.T) {
	root := t.TempDir()
	if !IsTempDir(filepath.Join(root, "child")) {
		t.Fatalf("expected child path under temp dir to be allowed")
	}

	var outside string
	if runtime.GOOS == "windows" {
		outside = `C:\definitely-not-temp\file.txt`
	} else {
		outside = "/definitely-not-temp/file.txt"
	}
	if IsTempDir(outside) {
		t.Fatalf("expected %q to be rejected as temp", outside)
	}
}

func TestIsTempDirUnderRoots(t *testing.T) {
	root := t.TempDir()
	if !IsTempDirUnderRoots(filepath.Join(root, "nested"), []string{root}) {
		t.Fatalf("expected nested child to be within temp root")
	}
	if IsTempDirUnderRoots(filepath.Join(os.TempDir(), "other"), []string{filepath.Join(root, "other")}) {
		t.Fatalf("expected unrelated path to be rejected")
	}
}
