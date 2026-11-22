package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain points HOME and every data/config variable at ONE temp root before
// any test runs, so no test in this package can read or write the developer's
// real ~/.local/share/opencode/auth.json. Several tests drive the process-global
// credential cache, and before this they wrote the real file.
//
// The variables get distinct subtrees under the same home, the way setHomeTree
// does: XDG_CONFIG_HOME and XDG_DATA_HOME must not resolve to one directory on
// Linux (see internal/config/testmain_test.go). The root is removed after the run.
//
// Before m.Run the resolved AuthPath, ProfileAuthPath and legacy opencode path
// must all sit inside the root. If any does not, the run stops with a non-zero
// exit rather than letting a test touch a real file.
func TestMain(m *testing.M) {
	os.Exit(runIsolated(m))
}

func runIsolated(m *testing.M) int {
	root, err := os.MkdirTemp("", "ocode-auth-test-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth tests: create temp root: %v\n", err)
		return 1
	}
	defer func() {
		if err := os.RemoveAll(root); err != nil {
			fmt.Fprintf(os.Stderr, "auth tests: remove temp root: %v\n", err)
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
			fmt.Fprintf(os.Stderr, "auth tests: set %s: %v\n", k, err)
			return 1
		}
	}

	authPath, err := AuthPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth tests: resolve AuthPath: %v\n", err)
		return 1
	}
	profilePath, err := ProfileAuthPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth tests: resolve ProfileAuthPath: %v\n", err)
		return 1
	}
	for _, p := range []string{authPath, profilePath, opencodeLegacyAuthPath()} {
		if !insideDir(p, root) {
			fmt.Fprintf(os.Stderr, "auth tests: refusing to run: a credential path resolves outside the isolated temp root (%s)\n", root)
			return 1
		}
	}
	return m.Run()
}

// insideDir reports whether path lies under dir. Both are compared as cleaned
// absolute paths; an empty path is outside every directory.
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
