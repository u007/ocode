package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/snapshot"
)

// TestMain isolates every global-config read/write for the whole server test
// binary by redirecting HOME (and the XDG/APPDATA/LOCALAPPDATA equivalents) to
// a temp dir. This guarantees tests that persist config — model/reasoning
// level, agent sessions, profiles — never mutate the developer's real
// ~/.config/opencode.
//
// The global snapshot store is initialized once in config.init() using the
// REAL home (before TestMain runs), so we re-point it at the isolated config
// dir here, otherwise config saves would still back up under the live home.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "ocode-server-test-")
	if err != nil {
		panic(err)
	}

	orig := map[string]string{
		"HOME":            os.Getenv("HOME"),
		"USERPROFILE":     os.Getenv("USERPROFILE"),
		"HOMEDRIVE":       os.Getenv("HOMEDRIVE"),
		"HOMEPATH":        os.Getenv("HOMEPATH"),
		"XDG_CONFIG_HOME": os.Getenv("XDG_CONFIG_HOME"),
		"XDG_DATA_HOME":   os.Getenv("XDG_DATA_HOME"),
		"XDG_STATE_HOME":  os.Getenv("XDG_STATE_HOME"),
		"APPDATA":         os.Getenv("APPDATA"),
		"LOCALAPPDATA":    os.Getenv("LOCALAPPDATA"),
	}

	os.Setenv("HOME", tmp)
	os.Setenv("USERPROFILE", tmp)
	os.Setenv("HOMEDRIVE", tmp)
	os.Setenv("HOMEPATH", tmp)
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Setenv("XDG_DATA_HOME", tmp)
	os.Setenv("XDG_STATE_HOME", tmp)
	os.Setenv("APPDATA", tmp)
	os.Setenv("LOCALAPPDATA", tmp)

	// Provider API-key env vars are process-global as well, and they WIN over a
	// stored credential (see auth.Provider.EnvVar). A developer machine that
	// exports e.g. OPENCODE_API_KEY — which is how ocode is normally used — made
	// every "build a client from a stored credential" test resolve the real key
	// instead of the fixture's, so the suite failed on the user's own machine and
	// passed in CI. Clear them next to the HOME/XDG isolation and restore them
	// below. Derived from the registry, so a new provider cannot reintroduce the
	// leak. Tests that genuinely need a key set it themselves with t.Setenv.
	for _, p := range auth.Providers {
		if p.EnvVar == "" {
			continue
		}
		if _, seen := orig[p.EnvVar]; seen {
			continue
		}
		orig[p.EnvVar] = os.Getenv(p.EnvVar)
		if err := os.Unsetenv(p.EnvVar); err != nil {
			panic(err)
		}
	}

	// Re-point the snapshot store now that HOME is isolated.
	if p, err := config.ActiveOcodeConfigPath(); err == nil {
		snapshot.SetGlobalBaseDir(filepath.Join(filepath.Dir(p), "snapshots"))
	}

	code := m.Run()

	// Restore the original environment.
	for k, v := range orig {
		if v == "" {
			_ = os.Unsetenv(k)
		} else {
			os.Setenv(k, v)
		}
	}
	// os.Exit skips deferred funcs, so remove the temp dir explicitly.
	_ = os.RemoveAll(tmp)

	os.Exit(code)
}
