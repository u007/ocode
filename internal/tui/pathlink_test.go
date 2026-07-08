package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathLinkAtCol(t *testing.T) {
	// workDir is the package dir; main.go lives two levels up. Use repo-relative
	// paths that exist from the module root passed explicitly as workDir.
	wd := "../.." // module root relative to internal/tui

	cases := []struct {
		name     string
		line     string
		col      int
		wantOK   bool
		wantPath string // suffix match
		wantLine int
	}{
		{"relative go file", "edit internal/tui/pathlink.go now", 12, true, "internal/tui/pathlink.go", 0},
		{"with line number", "see internal/tui/pathlink.go:42 there", 12, true, "internal/tui/pathlink.go", 42},
		{"col outside token", "edit internal/tui/pathlink.go now", 0, false, "", 0},
		{"nonexistent file", "open does/not/exist.go here", 8, false, "", 0},
		{"plain word", "this is just prose text", 5, false, "", 0},
		{"url not linked", "visit https://example.com/a/b now", 20, false, "", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, ok := pathLinkAtCol(tc.line, tc.col, wd)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (region=%+v)", ok, tc.wantOK, r)
			}
			if !ok {
				return
			}
			if tc.wantPath != "" && !hasSuffix(r.path, tc.wantPath) {
				t.Errorf("path = %q, want suffix %q", r.path, tc.wantPath)
			}
			if r.lineNo != tc.wantLine {
				t.Errorf("lineNo = %d, want %d", r.lineNo, tc.wantLine)
			}
			if r.startCol >= r.endCol {
				t.Errorf("startCol %d >= endCol %d", r.startCol, r.endCol)
			}
		})
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// TestPathLinkAtColTilde verifies that a leading "~"/"~/" is expanded to the
// user's home directory so shell-style paths become clickable links instead of
// being joined onto workDir (which would make the stat fail).
func TestPathLinkAtColTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir available: %v", err)
	}
	// Create a real file under home so the resolution succeeds.
	name := ".ocode-pathlink-test-" + filepath.Base(t.Name())
	target := filepath.Join(home, name)
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Skipf("cannot create test file under home: %v", err)
	}
	t.Cleanup(func() { os.Remove(target) })

	line := "see ~/.ocode-pathlink-test-whatever.md here"
	// Build the line with the actual generated name so the suffix matches.
	line = "see ~/" + name + " here"
	col := strings.Index(line, "~")

	r, ok := pathLinkAtCol(line, col, "../..")
	if !ok {
		t.Fatalf("expected ~ path to resolve to a link, got ok=false (region=%+v)", r)
	}
	want := filepath.Join(home, name)
	if r.path != want {
		t.Errorf("path = %q, want %q", r.path, want)
	}

	// A bare "~" alone should resolve to the home dir but be rejected as a link
	// because it is a directory, not a regular file.
	if _, ok := pathLinkAtCol("~", 0, "../.."); ok {
		t.Errorf("bare ~ should not become a link (it is a directory)")
	}
}

func TestSplitPathLine(t *testing.T) {
	cases := []struct {
		in       string
		wantPath string
		wantLine int
	}{
		{"foo.go", "foo.go", 0},
		{"foo.go:12", "foo.go", 12},
		{"foo.go:12:5", "foo.go", 12},
		{"a/b/c.ts", "a/b/c.ts", 0},
	}
	for _, tc := range cases {
		p, n := splitPathLine(tc.in)
		if p != tc.wantPath || n != tc.wantLine {
			t.Errorf("splitPathLine(%q) = (%q,%d), want (%q,%d)", tc.in, p, n, tc.wantPath, tc.wantLine)
		}
	}
}

func TestPathLinkProbeCache(t *testing.T) {
	wd := "../.."
	line := "edit internal/tui/pathlink.go now"
	var c pathLinkProbeCache

	r1, ok1 := c.probe(line, 12, wd)
	if !ok1 {
		t.Fatalf("first probe miss: %+v", r1)
	}
	// Poison the cached path; a hit within the same token span must return the
	// cached value without re-running pathLinkAtCol.
	c.r.path = "CACHED"
	if r2, ok2 := c.probe(line, r1.startCol, wd); !ok2 || r2.path != "CACHED" {
		t.Fatalf("expected cached hit within token span, got ok=%v r=%+v", ok2, r2)
	}
	// Outside the cached span → fresh probe (no token there → miss).
	if _, ok3 := c.probe(line, 0, wd); ok3 {
		t.Fatal("expected miss outside token span")
	}
	// Different line content with same col must not hit the stale cache.
	if _, ok4 := c.probe("totally different prose here", 12, wd); ok4 {
		t.Fatal("expected miss on different line content")
	}
	// Negative results over a path-like token are cached too: probe a
	// nonexistent path, then verify the span is recorded so motion within the
	// token skips re-statting.
	missLine := "open does/not/exist.go here"
	if _, ok := c.probe(missLine, 8, wd); ok {
		t.Fatal("expected miss for nonexistent file")
	}
	if c.rawLine != missLine || c.endCol <= c.startCol {
		t.Fatalf("negative probe span not cached: %+v", c)
	}
}
