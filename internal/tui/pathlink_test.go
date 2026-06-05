package tui

import "testing"

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
