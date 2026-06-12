// Package paths provides cross-platform resolution of ocode's global data
// directories. All packages that store persistent state under the opencode
// namespace should use these functions instead of duplicating the logic.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// AppName is the directory name used under the platform data root.
const AppName = "opencode"

// GlobalDataDir returns the cross-platform data directory for ocode state.
//
// Resolution order:
//  1. Windows:  %LOCALAPPDATA%\opencode
//  2. macOS:    ~/.local/share/opencode
//  3. Linux/other (XDG): $XDG_DATA_HOME/opencode  (falls back to ~/.local/share/opencode)
//
// The directory is created if it does not exist.
func GlobalDataDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", fmt.Errorf("LOCALAPPDATA not set")
		}
		return ensureDir(filepath.Join(base, AppName))

	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return ensureDir(filepath.Join(home, ".local", "share", AppName))

	default: // linux, freebsd, …
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return ensureDir(filepath.Join(xdg, AppName))
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return ensureDir(filepath.Join(home, ".local", "share", AppName))
	}
}

// ProjectSessionsDir returns the per-project sessions directory under the
// global data dir. The slug is an opaque identifier derived from the project
// root (e.g. a SHA-256 prefix).
func ProjectSessionsDir(slug string) (string, error) {
	base, err := GlobalDataDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(base, "project", slug, "sessions"))
}

// UsageDir returns the usage data directory under the global data dir.
func UsageDir() (string, error) {
	base, err := GlobalDataDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(base, "usage"))
}

func ensureDir(path string) (string, error) {
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", fmt.Errorf("create directory %s: %w", path, err)
	}
	return path, nil
}
