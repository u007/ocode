package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// isolateConfig points every ocode config lookup at a temp dir for one test.
//
// The desktop package has no TestMain isolation of its own, so a test that
// read or wrote the real ~/.config/opencode/ocodeconfig.json would mutate the
// developer's live config. OPENCODE_CONFIG_DIR is what portFilePath and the
// config package both honour, so it is the single knob needed here.
func isolateConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("OPENCODE_CONFIG_DIR", dir)
	return dir
}

// TestAutoShareEnabledAtBootDefaultsOff is the core safety assertion: with no
// config written at all, auto-share must read as OFF. Getting this wrong means
// a fresh install publishes itself to the tailnet with nobody asking.
func TestAutoShareEnabledAtBootDefaultsOff(t *testing.T) {
	isolateConfig(t)

	if autoShareEnabledAtBoot() {
		t.Fatal("auto-share defaulted to enabled; it must be opt-in")
	}
}

// TestAutoShareEnabledAtBootReadsPersistedToggle proves the hook honours the
// persisted setting, and that OFF survives a previous ON (the toggle must be
// readable in both directions, not one-way).
func TestAutoShareEnabledAtBootReadsPersistedToggle(t *testing.T) {
	isolateConfig(t)

	if err := config.SaveAutoShareOnStart(true); err != nil {
		t.Fatalf("save enabled: %v", err)
	}
	if !autoShareEnabledAtBoot() {
		t.Fatal("auto-share read as off after being saved on")
	}

	if err := config.SaveAutoShareOnStart(false); err != nil {
		t.Fatalf("save disabled: %v", err)
	}
	if autoShareEnabledAtBoot() {
		t.Fatal("auto-share read as on after being saved off")
	}
}

// TestAutoShareEnabledAtBootMalformedConfigIsOff is the fail-safe direction.
//
// An unreadable or corrupt config must resolve to OFF, never to ON. Defaulting a
// parse failure to "share my instance on the network" would turn a broken file
// into a security event.
func TestAutoShareEnabledAtBootMalformedConfigIsOff(t *testing.T) {
	dir := isolateConfig(t)

	if err := os.MkdirAll(filepath.Join(dir, "opencode"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	bad := filepath.Join(dir, "opencode", "ocodeconfig.json")
	if err := os.WriteFile(bad, []byte("{not json at all"), 0o600); err != nil {
		t.Fatalf("write bad config: %v", err)
	}

	if autoShareEnabledAtBoot() {
		t.Fatal("a malformed config must read as auto-share OFF, never ON")
	}
}