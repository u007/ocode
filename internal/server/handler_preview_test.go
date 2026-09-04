package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleFileRawServesPreviewable(t *testing.T) {
	dir := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(dir)

	cases := []struct {
		name    string
		file    string
		payload []byte
		wantCT  string
	}{
		{"pdf", "report.pdf", []byte("%PDF-1.4 fake"), "application/pdf"},
		{"xlsx", "book.xlsx", []byte("PK\x03\x04 fake"), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"xls", "legacy.xls", []byte("\xd0\xcf\x11\xe0 fake"), "application/vnd.ms-excel"},
		{"csv", "data.csv", []byte("a,b\n1,2\n"), "text/csv; charset=utf-8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, tc.file), tc.payload, 0o644); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("GET", "/api/files/raw?path="+url.QueryEscape(tc.file), nil)
			rec := httptest.NewRecorder()
			h.HandleFileRaw(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != tc.wantCT {
				t.Errorf("content-type = %q, want %q", ct, tc.wantCT)
			}
			if rec.Body.String() != string(tc.payload) {
				t.Error("body bytes mismatch")
			}
		})
	}
}

func TestHandleFileRawRejects(t *testing.T) {
	dir := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(dir)

	exe := filepath.Join(dir, "app.exe")
	if err := os.WriteFile(exe, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		query string
		want  int
	}{
		{"missing path", "/api/files/raw", http.StatusBadRequest},
		{"unknown ext", "/api/files/raw?path=" + url.QueryEscape("app.exe"), http.StatusBadRequest},
		{"traversal", "/api/files/raw?path=" + url.QueryEscape("../../etc/passwd"), http.StatusBadRequest},
		{"not found", "/api/files/raw?path=" + url.QueryEscape("missing.pdf"), http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.HandleFileRaw(rec, httptest.NewRequest("GET", tc.query, nil))
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body=%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestHandleOpenFileRejectsBadMode(t *testing.T) {
	dir := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(dir)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.HandleOpenFile(rec, httptest.NewRequest("POST", "/api/files/open", strings.NewReader(`{"path":"a.txt","mode":"bogus"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestResolveOpenPathProjectRoot(t *testing.T) {
	dir := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(dir)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Unknown project root is rejected.
	if _, err := h.resolveOpenPath("a.txt", t.TempDir()); err == nil {
		t.Error("expected rejection for unknown project root")
	}
	// Escape from the project root is rejected.
	if _, err := h.resolveOpenPath("../outside.txt", dir); err == nil {
		t.Error("expected rejection for path outside project root")
	}
	// Inside the root resolves.
	got, err := h.resolveOpenPath("a.txt", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Join(dir, "a.txt") {
		t.Errorf("got %q", got)
	}
}

// TestPreviewRawCoversOfficeSet pins the office formats servable as raw
// preview bytes. It must stay synchronized with previewOpenKinds in
// internal/tool/preview.go (AI tool allowlist) and kindByExt in
// web/src/lib/previewKind.ts (renderer routing). Legacy .doc/.ppt are
// intentionally absent: they go through the OS-open fallback, and the raw
// endpoint must never become an arbitrary-binary channel.
func TestPreviewRawCoversOfficeSet(t *testing.T) {
	office := map[string]string{
		".pdf":  "application/pdf",
		".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		".xls":  "application/vnd.ms-excel",
		".csv":  "text/csv; charset=utf-8",
	}
	for ext, wantCT := range office {
		got, ok := previewRawTypes[ext]
		if !ok {
			t.Errorf("previewRawTypes missing %q", ext)
			continue
		}
		if got != wantCT {
			t.Errorf("previewRawTypes[%q] = %q, want %q", ext, got, wantCT)
		}
	}
	for _, legacy := range []string{".doc", ".ppt", ".exe", ".mkv"} {
		if _, ok := previewRawTypes[legacy]; ok {
			t.Errorf("previewRawTypes must not serve %q", legacy)
		}
	}
}
