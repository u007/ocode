package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestGetThemeDefault(t *testing.T) {
	h := NewHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/theme", nil)
	h.HandleGetTheme(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result ThemeColorsResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Name == "" {
		t.Error("expected non-empty theme name")
	}
	if result.Colors.Background == "" {
		t.Error("expected non-empty background color")
	}
}

// TestGetThemeUnknownFallsBackToDefault asserts that an unknown theme name in
// config still returns 200 with the default theme's colors (tokyonight) rather
// than erroring or returning an empty palette — deliberate config resolution.
func TestGetThemeUnknownFallsBackToDefault(t *testing.T) {
	h := NewHandler()
	h.cfg = &config.Config{}
	h.cfg.Ocode.TUI.Theme = "totally-not-a-real-theme"

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/theme", nil)
	h.HandleGetTheme(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result ThemeColorsResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// Name echoes the configured (unknown) name; colors fall back to default.
	if result.Name != "totally-not-a-real-theme" {
		t.Errorf("expected name to echo configured theme, got %q", result.Name)
	}
	// tokyonight default background.
	if result.Colors.Background != "#1a1b26" {
		t.Errorf("expected default tokyonight background #1a1b26, got %q", result.Colors.Background)
	}
}
