// Package paths provides cross-platform resolution of ocode's global data
// directories. All packages that store persistent state under the opencode
// namespace should use these functions instead of duplicating the logic.
package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// AppName is the directory name used under the platform data root.
const AppName = "opencode"

// gitToplevelCache memoizes `git rev-parse --show-toplevel` per working dir so
// repeated slug computations don't re-spawn git.
var gitToplevelMu sync.Mutex
var gitToplevelCache = map[string]string{}

// ProjectRoot resolves wd to the canonical project root: symlinks are resolved
// first, then `git rev-parse --show-toplevel` is used when wd is inside a git
// work tree. It falls back to wd itself when git is unavailable.
func ProjectRoot(wd string) string {
	if wd == "" {
		wd, _ = os.Getwd()
	}
	if resolved, err := filepath.EvalSymlinks(wd); err == nil {
		wd = resolved
	}
	return gitToplevel(wd)
}

// ProjectSlug returns an opaque, stable 12-hex-char identifier for the project
// containing wd. It is derived from the canonical project root (see
// ProjectRoot). On Windows the path is lower-cased before hashing so the slug
// is case-insensitive across drive-letter/volume casing differences. This is
// the same slug the session package uses to scope per-project state under
// GlobalDataDir()/project/{slug}/, but it lives here (rather than in
// internal/session) so callers that cannot import session due to an import
// cycle can still compute it.
func ProjectSlug(wd string) string {
	wd = ProjectRoot(wd)
	wd = filepath.Clean(wd)
	if runtime.GOOS == "windows" {
		wd = strings.ToLower(wd)
	}
	hash := sha256.Sum256([]byte(wd))
	return hex.EncodeToString(hash[:])[:12]
}

// gitToplevel resolves the git repository root for wd, memoized. Returns wd
// unchanged when wd is not inside a git work tree.
func gitToplevel(wd string) string {
	gitToplevelMu.Lock()
	defer gitToplevelMu.Unlock()
	if v, ok := gitToplevelCache[wd]; ok {
		return v
	}
	result := wd
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = wd
	if output, err := cmd.Output(); err == nil {
		result = strings.TrimSpace(string(output))
	}
	gitToplevelCache[wd] = result
	return result
}

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
