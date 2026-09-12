package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/wallpaper"
)

func TestHandleGetWallpaper_ServesSVGWithScriptBlockingHeaders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("LOCALAPPDATA", home)
	if _, err := wallpaper.GenerateBuiltinWallpapers(); err != nil {
		t.Fatalf("generate builtins: %v", err)
	}
	h := &Handler{}
	rec := httptest.NewRecorder()
	h.HandleGetWallpaper(rec, httptest.NewRequest(http.MethodGet, "/api/wallpaper/dark-gradient", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'none'") {
		t.Fatalf("missing script-blocking CSP, got %q", csp)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing nosniff")
	}
}

func TestHandleGetWallpaper_RejectsTraversalID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("LOCALAPPDATA", home)
	h := &Handler{}
	rec := httptest.NewRecorder()
	h.HandleGetWallpaper(rec, httptest.NewRequest(http.MethodGet, "/api/wallpaper/upload-../../../etc/passwd", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
