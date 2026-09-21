package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMergeUserBinPath(t *testing.T) {
	sep := string(os.PathListSeparator)
	cases := []struct {
		name    string
		current string
		dirs    []string
		want    string
	}{
		{"prepends in order", "a" + sep + "b", []string{"x", "y"}, "x" + sep + "y" + sep + "a" + sep + "b"},
		{"skips already present", "x" + sep + "a", []string{"x", "y"}, "y" + sep + "x" + sep + "a"},
		{"empty current", "", []string{"x"}, "x"},
		{"nothing to add", "a", nil, "a"},
		{"empty dirs entries skipped", "a", []string{"", "x"}, "x" + sep + "a"},
		{"all present is unchanged", "x" + sep + "y", []string{"x", "y"}, "x" + sep + "y"},
		{"preserves empty path elements", "a" + sep + sep + "b", []string{"x"}, "x" + sep + "a" + sep + sep + "b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergeUserBinPath(tc.current, tc.dirs); got != tc.want {
				t.Fatalf("mergeUserBinPath(%q, %v) = %q, want %q", tc.current, tc.dirs, got, tc.want)
			}
		})
	}
}

func TestEnsureUserBinPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no ~/.local/bin convention on Windows")
	}
	home := t.TempDir()
	localBin := filepath.Join(home, ".local", "bin")
	userBin := filepath.Join(home, "bin")
	for _, d := range []string{localBin, userBin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sep := string(os.PathListSeparator)

	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin"+sep+"/bin")

	if !EnsureUserBinPath() {
		t.Fatal("EnsureUserBinPath() = false, want true")
	}
	want := localBin + sep + userBin + sep + "/usr/bin" + sep + "/bin"
	if got := os.Getenv("PATH"); got != want {
		t.Fatalf("PATH = %q, want %q", got, want)
	}

	// Idempotent: a second call must report no change and not duplicate entries.
	if EnsureUserBinPath() {
		t.Fatal("second EnsureUserBinPath() = true, want false")
	}
	if got := os.Getenv("PATH"); got != want {
		t.Fatalf("PATH after second call = %q, want %q", got, want)
	}
	if n := strings.Count(os.Getenv("PATH"), localBin); n != 1 {
		t.Fatalf("PATH contains %q %d times, want 1: %q", localBin, n, os.Getenv("PATH"))
	}
}

func TestEnsureUserBinPathSkipsMissingAndExistingDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no ~/.local/bin convention on Windows")
	}
	sep := string(os.PathListSeparator)
	home := t.TempDir()
	localBin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	// ~/bin does not exist → only ~/.local/bin is added.
	t.Setenv("PATH", "/usr/bin")
	if !EnsureUserBinPath() {
		t.Fatal("EnsureUserBinPath() = false, want true (adds the existing ~/.local/bin)")
	}
	if got, want := os.Getenv("PATH"), localBin+sep+"/usr/bin"; got != want {
		t.Fatalf("PATH = %q, want %q", got, want)
	}

	// Already on PATH → no change at all.
	t.Setenv("PATH", localBin+sep+"/usr/bin")
	if EnsureUserBinPath() {
		t.Fatal("EnsureUserBinPath() = true when ~/.local/bin is already on PATH, want false")
	}
}
