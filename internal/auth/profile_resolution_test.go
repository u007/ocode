package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// ocodeDataDirForTest mirrors paths.OcodeGlobalDataDir's platform split: on
// darwin and Windows the XDG variable is IGNORED and the path is derived from
// $HOME (or %LOCALAPPDATA%); only Linux honours XDG_DATA_HOME.
func ocodeDataDirForTest(t *testing.T, home string) string {
	t.Helper()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, ".local", "share", "ocode")
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "ocode")
		}
		return filepath.Join(home, "AppData", "Local", "ocode")
	default:
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "ocode")
		}
		return filepath.Join(home, ".local", "share", "ocode")
	}
}

// TestActiveProfileReadsOcodeDataDirWindowState proves that the profile a
// NEW CLIENT resolves (auth.ActiveProfile -> activeProfileForResolution) is
// read from <OcodeGlobalDataDir>/window-state.json, and that it is the SAME
// file the desktop server persists to when the user switches a window's
// profile (internal/server/handler_profiles.go calls config.SetActiveProfile,
// which writes exactly this file).
//
// This is the regression guard for the failure where a sub-agent built with
// NewClient() resolved profile "" (base credentials) while the server's main
// agent resolved the window profile — so the sub-agent used a DIFFERENT
// (expired) provider key and every context-agent dispatch died with
// `anthropic error (401): api_key_expired` while the main agent worked.
func TestActiveProfileReadsOcodeDataDirWindowState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	// OCODE_PROFILE wins over the file; make sure it is not set.
	t.Setenv("OCODE_PROFILE", "")
	writeWindowStateForTest(t, home, "ocode2")
	setWindowStateFallbackForTest(t, true)

	got := ActiveProfile()
	if got != "ocode2" {
		t.Fatalf("ActiveProfile() = %q, want %q — the window-state file is not being read from the ocode data dir", got, "ocode2")
	}
}

func writeWindowStateForTest(t *testing.T, home, profile string) {
	t.Helper()
	dir := ocodeDataDirForTest(t, home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	body, err := json.Marshal(map[string]any{
		"windows": map[string]any{"win-1": map[string]any{"activeProfile": profile}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "window-state.json"), body, 0o600); err != nil {
		t.Fatalf("write window state: %v", err)
	}
}

func setWindowStateFallbackForTest(t *testing.T, on bool) {
	t.Helper()
	prev := windowStateFallback.Load()
	windowStateFallback.Store(on)
	t.Cleanup(func() { windowStateFallback.Store(prev) })
}

// A TUI/CLI process never enables the fallback, so a stale desktop window
// profile must not leak into it; OCODE_PROFILE is still honoured.
func TestActiveProfileIgnoresWindowStateOutsideServer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("OCODE_PROFILE", "")
	writeWindowStateForTest(t, home, "ocode2")
	setWindowStateFallbackForTest(t, false)

	if got := ActiveProfile(); got != "" {
		t.Fatalf("ActiveProfile() = %q outside the server, want base (empty)", got)
	}
	t.Setenv("OCODE_PROFILE", "explicit")
	if got := ActiveProfile(); got != "explicit" {
		t.Fatalf("ActiveProfile() = %q, want OCODE_PROFILE value", got)
	}
}
