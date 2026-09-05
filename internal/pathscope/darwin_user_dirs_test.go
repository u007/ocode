package pathscope

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDarwinUserDirsMatchesConfstrTempDir(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	dirs := DarwinUserDirs()
	if len(dirs) == 0 {
		t.Fatal("expected per-user /var/folders dirs on darwin")
	}
	for _, d := range dirs {
		if b := filepath.Base(d); b != "T" && b != "C" {
			t.Fatalf("unexpected dir %q", d)
		}
	}
	// $TMPDIR (when set) is the confstr T dir and must be among the results.
	if tmp := os.Getenv("TMPDIR"); tmp != "" {
		want := filepath.Clean(tmp)
		found := false
		for _, d := range DarwinUserTempDir() {
			if d == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("TMPDIR %q not in %v", want, dirs)
		}
	}
	// Recognized as temp even with TMPDIR unset.
	t.Setenv("TMPDIR", "")
	for _, d := range DarwinUserTempDir() {
		if !IsTempDir(filepath.Join(d, "x")) {
			t.Fatalf("%q not classified as temp dir", d)
		}
	}
}
