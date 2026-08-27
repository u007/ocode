package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWithinWorkdir(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler()

	t.Run("relative inside is allowed", func(t *testing.T) {
		got, err := h.resolveWithinWorkdir("handler_open.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != filepath.Join(wd, "handler_open.go") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("parent traversal is rejected", func(t *testing.T) {
		if _, err := h.resolveWithinWorkdir("../../etc/passwd"); err == nil {
			t.Fatal("expected traversal to be rejected")
		}
	})

	t.Run("absolute outside workdir is rejected", func(t *testing.T) {
		if _, err := h.resolveWithinWorkdir("/etc/passwd"); err == nil {
			t.Fatal("expected absolute outside path to be rejected")
		}
	})

	t.Run("uses h.workDir, not process cwd, when they differ", func(t *testing.T) {
		other := t.TempDir()
		h2 := NewHandler()
		h2.SetWorkDir(other)
		if _, err := os.Create(filepath.Join(other, "marker.txt")); err != nil {
			t.Fatal(err)
		}
		got, err := h2.resolveWithinWorkdir("marker.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != filepath.Join(other, "marker.txt") {
			t.Errorf("got %q, want a path under %q (the handler's workDir, not process cwd)", got, other)
		}
		// A path that is inside the process's real cwd but outside h2's
		// configured workDir must still be rejected — this is the exact
		// containment guarantee a Finder-launched desktop app (cwd "/")
		// would otherwise silently lose.
		if _, err := h2.resolveWithinWorkdir(filepath.Join(wd, "handler_open.go")); err == nil {
			t.Fatal("expected a path outside h2.workDir to be rejected even though it's under process cwd")
		}
	})
}

func TestHandleOpenFileValidation(t *testing.T) {
	h := NewHandler()

	cases := []struct {
		name string
		body string
		want int
	}{
		{"empty path", `{"path":""}`, http.StatusBadRequest},
		{"bad json", `{`, http.StatusBadRequest},
		{"traversal", `{"path":"../../etc/passwd"}`, http.StatusBadRequest},
		{"nonexistent", `{"path":"does/not/exist.go"}`, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/files/open", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.HandleOpenFile(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body=%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
