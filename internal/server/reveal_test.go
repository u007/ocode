package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// revealCommand is pure, so the per-platform argv shape can be pinned without
// launching a real file manager. This is the contract the web "Open/Show in
// Finder/Explorer/File Manager" action depends on.
func TestRevealCommand(t *testing.T) {
	cases := []struct {
		name     string
		goos     string
		path     string
		isDir    bool
		wantName string
		wantArgs []string
	}{
		{
			name:     "darwin directory opens in Finder",
			goos:     "darwin",
			path:     "/Users/me/proj/src",
			isDir:    true,
			wantName: "open",
			wantArgs: []string{"/Users/me/proj/src"},
		},
		{
			name:     "darwin file is selected with open -R",
			goos:     "darwin",
			path:     "/Users/me/proj/src/a.ts",
			isDir:    false,
			wantName: "open",
			wantArgs: []string{"-R", "/Users/me/proj/src/a.ts"},
		},
		{
			name:     "windows directory opens in Explorer",
			goos:     "windows",
			path:     `C:\proj\src`,
			isDir:    true,
			wantName: "explorer",
			wantArgs: []string{`C:\proj\src`},
		},
		{
			name:     "windows file is selected with /select,",
			goos:     "windows",
			path:     `C:\proj\src\a.ts`,
			isDir:    false,
			wantName: "explorer",
			wantArgs: []string{`/select,C:\proj\src\a.ts`},
		},
		{
			name:     "linux directory opens with xdg-open",
			goos:     "linux",
			path:     "/home/me/proj/src",
			isDir:    true,
			wantName: "xdg-open",
			wantArgs: []string{"/home/me/proj/src"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, args := revealCommand(tc.goos, tc.path, tc.isDir)
			if name != tc.wantName {
				t.Fatalf("name = %q, want %q", name, tc.wantName)
			}
			if len(args) != len(tc.wantArgs) {
				t.Fatalf("args = %v, want %v", args, tc.wantArgs)
			}
			for i := range args {
				if args[i] != tc.wantArgs[i] {
					t.Fatalf("args[%d] = %q, want %q (args=%v)", i, args[i], tc.wantArgs[i], args)
				}
			}
		})
	}
}

// A Linux file reveal targets the freedesktop FileManager1 interface (the
// cross-desktop "select this file" call used by Nautilus/Thunar/Dolphin/Nemo)
// with a percent-encoded file:// URI.
func TestRevealCommandLinuxFileUsesFileManager1(t *testing.T) {
	name, args := revealCommand("linux", "/home/me/my proj/a b.ts", false)
	if name != "dbus-send" {
		t.Fatalf("name = %q, want dbus-send", name)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "org.freedesktop.FileManager1.ShowItems") {
		t.Fatalf("args missing ShowItems method: %v", args)
	}
	// url.URL escapes spaces; the URI must not contain a literal space.
	if !strings.Contains(joined, "file:///home/me/my%20proj/a%20b.ts") {
		t.Fatalf("args missing percent-encoded file URI: %v", args)
	}
}

// The handler must accept a directory in reveal mode (unlike every other
// mode, which 404s a directory) and route it through the reveal seam.
func TestHandleOpenFileRevealAllowsDirectory(t *testing.T) {
	wd := t.TempDir()
	dir := filepath.Join(wd, "sub")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.ts"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewHandler()
	h.SetWorkDir(wd)

	var gotPath string
	var gotIsDir bool
	orig := revealPathFn
	revealPathFn = func(absPath string, isDir bool) error {
		gotPath, gotIsDir = absPath, isDir
		return nil
	}
	t.Cleanup(func() { revealPathFn = orig })

	t.Run("directory", func(t *testing.T) {
		body := `{"path":"sub","mode":"reveal"}`
		rec := httptest.NewRecorder()
		h.HandleOpenFile(rec, httptest.NewRequest("POST", "/api/files/open", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		if gotPath != dir || !gotIsDir {
			t.Fatalf("reveal called with (%q, isDir=%v), want (%q, true)", gotPath, gotIsDir, dir)
		}
	})

	t.Run("file", func(t *testing.T) {
		body := `{"path":"sub/a.ts","mode":"reveal"}`
		rec := httptest.NewRecorder()
		h.HandleOpenFile(rec, httptest.NewRequest("POST", "/api/files/open", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		if gotPath != filepath.Join(dir, "a.ts") || gotIsDir {
			t.Fatalf("reveal called with (%q, isDir=%v), want (%q, false)", gotPath, gotIsDir, filepath.Join(dir, "a.ts"))
		}
	})
}

// Regression guard: the reveal mode must not loosen the LFI containment or the
// directory rule for the OTHER modes.
func TestHandleOpenFileRevealKeepsContainment(t *testing.T) {
	wd := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(wd)

	// traversal in reveal mode is still rejected.
	rec := httptest.NewRecorder()
	h.HandleOpenFile(rec, httptest.NewRequest("POST", "/api/files/open",
		strings.NewReader(`{"path":"../../etc","mode":"reveal"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("traversal status = %d, want 400", rec.Code)
	}

	// a directory in editor mode is still a 404.
	dir := filepath.Join(wd, "sub")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.HandleOpenFile(rec, httptest.NewRequest("POST", "/api/files/open",
		strings.NewReader(`{"path":"sub","mode":"editor"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("editor-on-dir status = %d, want 404", rec.Code)
	}
}
