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

	t.Run("relative inside is allowed", func(t *testing.T) {
		got, err := resolveWithinWorkdir("handler_open.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != filepath.Join(wd, "handler_open.go") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("parent traversal is rejected", func(t *testing.T) {
		if _, err := resolveWithinWorkdir("../../etc/passwd"); err == nil {
			t.Fatal("expected traversal to be rejected")
		}
	})

	t.Run("absolute outside workdir is rejected", func(t *testing.T) {
		if _, err := resolveWithinWorkdir("/etc/passwd"); err == nil {
			t.Fatal("expected absolute outside path to be rejected")
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
