package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/snapshot"
)

// testRealHome captures the developer's actual home before TestMain redirects
// it, so regression tests can prove global config never lands under the real
// home directory (where the selected model and reasoning/effort level live).
var testRealHome string

// TestMain isolates every global-config read/write for the whole config test
// binary. Functions such as SaveLastModel / SaveLastThinkingBudget persist the
// selected model and reasoning level to ~/.config/opencode/ocodeconfig.json;
// without this guard a single non-isolated test could clobber the developer's
// live config. Setting HOME (and the XDG/APPDATA/LOCALAPPDATA equivalents) to a
// temp dir makes getGlobal(Ocode)ConfigPath resolve under that temp dir, so
// all writes — including session storage, snapshot backups, and anything that
// derives from GlobalDataDir — stay off the real home.
//
// The global snapshot store is initialized once in init() using the REAL home
// (before TestMain runs), so we must re-point it at the isolated config dir
// here, otherwise config saves would still back up under the live home.
func TestMain(m *testing.M) {
	if h, err := os.UserHomeDir(); err == nil {
		testRealHome = h
	} else {
		testRealHome = os.Getenv("HOME")
	}

	tmp, err := os.MkdirTemp("", "ocode-config-test-")
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
	if p, err := getGlobalOcodeConfigPath(); err == nil {
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

// underDir reports whether p is the base dir or lives beneath it.
func underDir(p, base string) bool {
	absP, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	absB, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absB, absP)
	if err != nil {
		return false
	}
	return rel == "." || !strings.HasPrefix(rel, "..")
}

// TestGlobalConfigNeverTouchesRealHome proves that, while running under
// `go test`, the global ocode config path (which holds the model name and
// reasoning/effort level) resolves under the isolated temp HOME and never
// under the developer's real home directory.
func TestGlobalConfigNeverTouchesRealHome(t *testing.T) {
	if testRealHome == "" {
		t.Skip("real HOME was not captured")
	}

	glob, err := getGlobalOcodeConfigPath()
	if err != nil {
		t.Fatalf("getGlobalOcodeConfigPath: %v", err)
	}
	if underDir(glob, testRealHome) {
		t.Fatalf("global ocode config would write to real home: %s", glob)
	}
	if !underDir(glob, os.TempDir()) {
		t.Fatalf("global ocode config not isolated under temp dir: %s", glob)
	}

	gcfg, err := getGlobalConfigPath()
	if err != nil {
		t.Fatalf("getGlobalConfigPath: %v", err)
	}
	if underDir(gcfg, testRealHome) {
		t.Fatalf("global config would write to real home: %s", gcfg)
	}
	if !underDir(gcfg, os.TempDir()) {
		t.Fatalf("global config not isolated under temp dir: %s", gcfg)
	}
}

// TestSaveLastModelIsolated proves a model/reasoning write during a test lands
// under the isolated temp root, never the real home config.
func TestSaveLastModelIsolated(t *testing.T) {
	if err := SaveLastModel("anthropic/test-model"); err != nil {
		t.Fatalf("SaveLastModel: %v", err)
	}
	if got := GetLastModel(); got != "anthropic/test-model" {
		t.Fatalf("GetLastModel = %q, want anthropic/test-model", got)
	}
	if err := SaveLastThinkingBudget(0); err != nil {
		t.Fatalf("SaveLastThinkingBudget: %v", err)
	}
	if got := GetLastThinkingBudget(); got != 0 {
		t.Fatalf("GetLastThinkingBudget = %d, want 0", got)
	}
}

// TestConfigSaveSnapshotIsolated proves the snapshot backup created on a config
// write also lands beneath the isolated temp root, never the developer's real
// home snapshots directory.
func TestConfigSaveSnapshotIsolated(t *testing.T) {
	if testRealHome == "" {
		t.Skip("real HOME was not captured")
	}
	if err := SaveLastModel("anthropic/snapshot-test-model"); err != nil {
		t.Fatalf("SaveLastModel: %v", err)
	}
	if files := snapshot.ChangedFiles(); len(files) == 0 {
		t.Fatalf("expected a snapshot backup to be recorded for the config write")
	}
	snapBase := filepath.Join(filepath.Dir(mustGlobalOcodeConfigPath(t)), "snapshots")
	if underDir(snapBase, testRealHome) {
		t.Fatalf("snapshot base would be under real home: %s", snapBase)
	}
	if !underDir(snapBase, os.TempDir()) {
		t.Fatalf("snapshot base not isolated under temp dir: %s", snapBase)
	}
	info, err := os.Stat(snapBase)
	if err != nil {
		t.Fatalf("snapshot base dir not created under temp root: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("snapshot base is not a directory: %s", snapBase)
	}
}

func mustGlobalOcodeConfigPath(t *testing.T) string {
	t.Helper()
	p, err := getGlobalOcodeConfigPath()
	if err != nil {
		t.Fatalf("getGlobalOcodeConfigPath: %v", err)
	}
	return p
}
