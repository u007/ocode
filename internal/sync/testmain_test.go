package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/auth"
)

// TestMain points HOME and every data/config variable at ONE temp root before
// any test runs, so no test in this package can read or write the developer's
// real ~/.local/share/opencode/auth.json. The sync tests drive the credential
// store through internal/auth, and an unisolated run would touch the real file.
//
// The variables get distinct subtrees under the same home, the way setHomeTree
// does. The root is removed after the run. Before m.Run the resolved AuthPath
// must sit inside the root; if it does not, the run stops with a non-zero exit
// rather than letting a test touch a real file.
func TestMain(m *testing.M) {
	os.Exit(runIsolated(m))
}

func runIsolated(m *testing.M) int {
	root, err := os.MkdirTemp("", "ocode-sync-test-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sync tests: create temp root: %v\n", err)
		return 1
	}
	defer func() {
		if err := os.RemoveAll(root); err != nil {
			fmt.Fprintf(os.Stderr, "sync tests: remove temp root: %v\n", err)
		}
	}()

	home := filepath.Join(root, "home")
	env := map[string]string{
		"HOME":            home,
		"USERPROFILE":     home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_DATA_HOME":   filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME":  filepath.Join(home, ".local", "state"),
		"APPDATA":         filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA":    filepath.Join(home, "AppData", "Local"),
	}
	for k, v := range env {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintf(os.Stderr, "sync tests: set %s: %v\n", k, err)
			return 1
		}
	}

	authPath, err := auth.AuthPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sync tests: resolve AuthPath: %v\n", err)
		return 1
	}
	if !insideDir(authPath, root) {
		fmt.Fprintf(os.Stderr, "sync tests: refusing to run: AuthPath resolves outside the isolated temp root (%s)\n", root)
		return 1
	}
	return m.Run()
}

// insideDir reports whether path lies under dir. Both are compared as cleaned
// paths; an empty path is outside every directory.
func insideDir(path, dir string) bool {
	if path == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// setHomeTree points every global-config root at one directory tree, so a test
// that overrides HOME gets the same isolation on macOS, Linux and Windows. It is
// the internal/config and internal/auth helper, copied here because sync has no
// shared test package. Never replace it with a bare t.Setenv("HOME", …).
//
// Each XDG variable points at the matching XDG-default subdirectory of home, so
// the resolved paths agree on every platform:
//
//	XDG_CONFIG_HOME=<home>/.config       → <home>/.config/opencode
//	XDG_DATA_HOME=<home>/.local/share    → <home>/.local/share/opencode
//	XDG_STATE_HOME=<home>/.local/state   → <home>/.local/state
func setHomeTree(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	// Windows resolves from these two instead of the XDG variables.
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
}
