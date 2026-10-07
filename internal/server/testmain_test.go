package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/snapshot"
)

// setHomeTree points HOME and every XDG/Windows equivalent at ONE temp tree so
// path resolution is identical on darwin and Linux.
//
// A bare t.Setenv("HOME", …) is not enough: paths.GlobalConfigDir() ignores
// XDG_CONFIG_HOME on darwin but honours it on Linux, so such a test writes to
// the package-wide XDG dir seeded by TestMain and then asserts under its own
// $HOME — passing on macOS and failing on Linux. internal/config and
// internal/agent carry the identical helper.
func setHomeTree(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
}

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
	// Each XDG variable gets its OWN subtree. Pointing them all at `tmp` made
	// GlobalConfigDir() and GlobalDataDir() the SAME directory on Linux (both
	// "<xdg>/opencode"), which the self-escalation guard reads as "writing to
	// the data dir", and it also disagreed with tests that assert under
	// $HOME/.config/opencode. On darwin XDG_CONFIG_HOME is ignored, so the
	// collapse only ever showed up on Linux.
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	os.Setenv("XDG_DATA_HOME", filepath.Join(tmp, ".local", "share"))
	os.Setenv("XDG_STATE_HOME", filepath.Join(tmp, ".local", "state"))
	os.Setenv("APPDATA", filepath.Join(tmp, "AppData", "Roaming"))
	os.Setenv("LOCALAPPDATA", filepath.Join(tmp, "AppData", "Local"))

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
