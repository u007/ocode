package pyinterp

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestCandidatesDefaultsAreUnchanged pins the default interpreter search.
//
// ExtraCandidateDirs and FrameworkCandidateGlobs are vars so a test can
// restrict the search (the absolute paths bypass PATH, so a test that only
// rewrites PATH would still find the host's real Python). They must keep
// their production values: a host that installed Python with a package
// manager, or with the python.org installer, depends on them being probed.
func TestCandidatesDefaultsAreUnchanged(t *testing.T) {
	wantDirs := []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"}
	if len(ExtraCandidateDirs) != len(wantDirs) {
		t.Fatalf("ExtraCandidateDirs = %v, want %v", ExtraCandidateDirs, wantDirs)
	}
	for i, d := range wantDirs {
		if ExtraCandidateDirs[i] != d {
			t.Fatalf("ExtraCandidateDirs[%d] = %q, want %q", i, ExtraCandidateDirs[i], d)
		}
	}
	wantGlobs := []string{"/Library/Frameworks/Python.framework/Versions/3.%d/bin/python3"}
	if len(FrameworkCandidateGlobs) != len(wantGlobs) {
		t.Fatalf("FrameworkCandidateGlobs = %v, want %v", FrameworkCandidateGlobs, wantGlobs)
	}
	for i, g := range wantGlobs {
		if FrameworkCandidateGlobs[i] != g {
			t.Fatalf("FrameworkCandidateGlobs[%d] = %q, want %q", i, FrameworkCandidateGlobs[i], g)
		}
	}
}

// TestCandidatesIncludeTheExtraDirs checks the var is actually USED, not
// merely declared: the absolute directories must appear among the candidates,
// both versioned and unversioned.
func TestCandidatesIncludeTheExtraDirs(t *testing.T) {
	orig := ExtraCandidateDirs
	ExtraCandidateDirs = []string{"/some/where/python"}
	t.Cleanup(func() { ExtraCandidateDirs = orig })

	cands := Candidates(Range{Min: [2]int{3, 10}})
	haveUnversioned := false
	haveVersioned := false
	for _, c := range cands {
		if len(c) != 1 {
			continue
		}
		if c[0] == filepath.Join("/some/where/python", "python3") {
			haveUnversioned = true
		}
		if filepath.Dir(c[0]) == filepath.Join("/some/where/python") && c[0] != filepath.Join("/some/where/python", "python3") {
			haveVersioned = true
		}
	}
	if !haveUnversioned {
		t.Errorf("the extra candidate directory's unversioned python3 was not probed")
	}
	if !haveVersioned {
		t.Errorf("the extra candidate directory's versioned pythons were not probed")
	}
}

// TestCandidatesIncludeTheFrameworkGlobs checks the framework patterns are
// used too.
//
// The glob only matches a real file, so the test lays out a real
// "Versions/3.<minor>/bin/python3" tree under a temp dir and points the
// pattern at it. Without a real file the assertion would pass even if the
// var were never read.
func TestCandidatesIncludeTheFrameworkGlobs(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the framework candidates are only probed on darwin")
	}
	root := t.TempDir()
	// The pattern receives the minor version, so lay out 3.10..3.18.
	want := ""
	for minor := 10; minor <= 18; minor++ {
		dir := filepath.Join(root, "Versions", "3."+itoa(minor), "bin")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "python3")
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if want == "" {
			want = p
		}
	}

	orig := FrameworkCandidateGlobs
	FrameworkCandidateGlobs = []string{filepath.Join(root, "Versions", "3.%d", "bin", "python3")}
	t.Cleanup(func() { FrameworkCandidateGlobs = orig })

	cands := Candidates(Range{Min: [2]int{3, 10}})
	for _, c := range cands {
		if len(c) == 1 && c[0] == want {
			return
		}
	}
	t.Fatalf("the framework glob did not produce %q; candidates = %v", want, cands)
}

// itoa avoids importing strconv for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
