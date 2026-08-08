package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/u007/ocode/internal/projects"
)

func newTestProjectStore(t *testing.T, paths ...string) *projects.Store {
	t.Helper()
	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("projects.NewStoreAt: %v", err)
	}
	for _, p := range paths {
		if err := store.Add(p); err != nil {
			t.Fatalf("store.Add(%q): %v", p, err)
		}
	}
	return store
}

func TestMatchSavedProject(t *testing.T) {
	store := newTestProjectStore(t,
		"/work/root",
		"/work/root/sub",
	)

	tests := []struct {
		name string
		cwd  string
		want string // "" => nil
	}{
		{"exact match", "/work/root", "/work/root"},
		{"longest prefix wins", "/work/root/sub/deep", "/work/root/sub"},
		{"ancestor match", "/work/root/sub", "/work/root/sub"},
		{"trailing slash normalized", "/work/root/", "/work/root"},
		{"outside", "/other/proj", ""},
		{"sibling with common prefix", "/work/roots", ""},
		{"filesystem root", "/", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchSavedProject(store, tt.cwd)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("matchSavedProject(%q) = %q, want nil", tt.cwd, got.Path)
				}
				return
			}
			if got == nil || got.Path != tt.want {
				t.Fatalf("matchSavedProject(%q) = %+v, want path %q", tt.cwd, got, tt.want)
			}
		})
	}
}

func TestMatchSavedProjectNilStore(t *testing.T) {
	if got := matchSavedProject(nil, "/work/root"); got != nil {
		t.Fatalf("nil store returned %+v, want nil", got)
	}
}

func TestIsAutoAddCandidate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME env override is not honored on windows")
	}

	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}

	markerDir := filepath.Join(root, "withmarker")
	if err := os.MkdirAll(filepath.Join(markerDir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	goModDir := filepath.Join(root, "gomod")
	if err := os.MkdirAll(goModDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goModDir, "go.mod"), []byte("module x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	plainDir := filepath.Join(root, "plain")
	if err := os.MkdirAll(plainDir, 0755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		cwd  string
		want bool
	}{
		{"empty", "", false},
		{"filesystem root", "/", false},
		{"home dir skipped", homeDir, false},
		{"git marker", markerDir, true},
		{"go.mod marker", goModDir, true},
		{"no marker", plainDir, false},
		{"nonexistent dir", filepath.Join(root, "missing"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAutoAddCandidate(tt.cwd); got != tt.want {
				t.Fatalf("isAutoAddCandidate(%q) = %v, want %v", tt.cwd, got, tt.want)
			}
		})
	}
}

func TestHandleGetCurrentProject(t *testing.T) {
	root := t.TempDir()
	projDir := filepath.Join(root, "proj")
	if err := os.MkdirAll(filepath.Join(projDir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	plainDir := filepath.Join(root, "plain")
	if err := os.MkdirAll(plainDir, 0755); err != nil {
		t.Fatal(err)
	}

	t.Run("matches saved project from subdirectory", func(t *testing.T) {
		h := testHandlerWithConfig(t)
		h.projects = newTestProjectStore(t, projDir)
		h.workDir = filepath.Join(projDir, "sub")
		rr := httptest.NewRecorder()
		h.HandleGetCurrentProject(rr, httptest.NewRequest(http.MethodGet, "/api/projects/current", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rr.Code)
		}
		var resp struct {
			Project *projects.Project `json:"project"`
			CWD     string            `json:"cwd"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Project == nil || resp.Project.Path != projDir {
			t.Fatalf("project = %+v, want path %q", resp.Project, projDir)
		}
		if resp.CWD != filepath.Join(projDir, "sub") {
			t.Fatalf("cwd = %q, want %q", resp.CWD, filepath.Join(projDir, "sub"))
		}
	})

	t.Run("auto-adds a project root with marker", func(t *testing.T) {
		h := testHandlerWithConfig(t)
		h.projects = newTestProjectStore(t) // empty store = first launch
		h.workDir = projDir
		rr := httptest.NewRecorder()
		h.HandleGetCurrentProject(rr, httptest.NewRequest(http.MethodGet, "/api/projects/current", nil))
		var resp struct {
			Project *projects.Project `json:"project"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Project == nil || resp.Project.Path != projDir {
			t.Fatalf("project = %+v, want auto-added %q", resp.Project, projDir)
		}
		if got := findProject(h.projects, projDir); got == nil {
			t.Fatal("auto-added project missing from store")
		}
	})

	t.Run("returns null for marker-less dir", func(t *testing.T) {
		h := testHandlerWithConfig(t)
		h.projects = newTestProjectStore(t)
		h.workDir = plainDir
		rr := httptest.NewRecorder()
		h.HandleGetCurrentProject(rr, httptest.NewRequest(http.MethodGet, "/api/projects/current", nil))
		var resp struct {
			Project *projects.Project `json:"project"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Project != nil {
			t.Fatalf("project = %+v, want null", resp.Project)
		}
	})

	t.Run("nil project store stays graceful", func(t *testing.T) {
		h := testHandlerWithConfig(t)
		h.projects = nil
		h.workDir = projDir
		rr := httptest.NewRecorder()
		h.HandleGetCurrentProject(rr, httptest.NewRequest(http.MethodGet, "/api/projects/current", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (not a 500)", rr.Code)
		}
		var resp struct {
			Project *projects.Project `json:"project"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Project != nil {
			t.Fatalf("project = %+v, want null", resp.Project)
		}
	})
}

func TestFindProject(t *testing.T) {
	store := newTestProjectStore(t, "/a/b")
	if got := findProject(store, "/a/b"); got == nil || got.Path != "/a/b" {
		t.Fatalf("findProject = %+v, want /a/b", got)
	}
	if got := findProject(store, "/a/c"); got != nil {
		t.Fatalf("findProject(/a/c) = %+v, want nil", got)
	}
	if got := findProject(nil, "/a/b"); got != nil {
		t.Fatalf("findProject(nil) = %+v, want nil", got)
	}
}
