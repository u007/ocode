package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/snapshot"
)

// TestMain isolates every global-config read/write for the whole tui test
// binary by redirecting HOME (and the XDG/APPDATA/LOCALAPPDATA equivalents) to
// a temp dir. This guarantees tests that persist config — model/reasoning
// level, permissions, theme — never mutate the developer's real
// ~/.config/opencode.
//
// The global snapshot store is initialized once in config.init() using the
// REAL home (before TestMain runs), so we re-point it at the isolated config
// dir here, otherwise config saves would still back up under the live home.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "ocode-tui-test-")
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
