package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/server"
)

// isolateConfig points every ocode config lookup at a temp dir for one test.
//
// The desktop package has no TestMain isolation of its own, so a test that
// read or wrote the real ~/.config/opencode/ocodeconfig.json would mutate the
// developer's live config. OPENCODE_CONFIG_DIR alone is NOT enough: internal/config
// resolves its path from HOME/XDG_CONFIG_HOME, and its own TestMain rewrites
// those at package init. HOME + XDG_CONFIG_HOME are the load-bearing pair, and
// the control assertion in each test below proves the real path was the one
// exercised.
func isolateConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("OPENCODE_CONFIG_DIR", dir)
	return dir
}

// writeConfig writes raw bytes to the ACTUAL resolved ocodeconfig.json path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p, err := config.ActiveOcodeConfigPath()
	if err != nil {
		t.Fatalf("resolve config path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
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
// readable in both directions, not a one-way latch).
//
// The control run is what makes this test honest: it writes a config that
// enables auto-share and asserts the loader SEES it, so a later false result
// can only mean the hook's logic is wrong, never that the write missed.
func TestAutoShareEnabledAtBootReadsPersistedToggle(t *testing.T) {
	isolateConfig(t)

	writeConfig(t, `{"auto_share_on_start":true}`)
	seen, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("control load: %v", err)
	}
	if !seen.AutoShareOnStart {
		t.Fatal("control failed: loader did not see the persisted true; test setup is wrong")
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

// TestAutoShareEnabledAtBootFailsSafeOnUnreadableConfig covers the branch that
// actually makes auto-share safe.
//
// config.LoadOcodeConfigCopy is a STRICT loader: it returns an error for corrupt
// JSON, truncated JSON, a directory where the file belongs, and an unreadable
// file. (An earlier version of this test wrongly claimed the loader was tolerant;
// that came from a probe whose OPENCODE_CONFIG_DIR was overridden away by
// internal/config's TestMain, so it never read the corrupt file at all.)
//
// The fail-safe therefore lives HERE, in autoShareEnabledAtBoot's err != nil
// branch, not in the loader. Without it, a single malformed byte in
// ocodeconfig.json would decide whether the instance is published to the tailnet.
func TestAutoShareEnabledAtBootFailsSafeOnUnreadableConfig(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"corrupt json", "{not json at all"},
		{"truncated json", `{"auto_share_on_start":`},
		{"wrong type", `{"auto_share_on_start": "yes"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfig(t)
			writeConfig(t, tc.body)

			// Control: this really is an unreadable config for the loader.
			if _, err := config.LoadOcodeConfigCopy(); err == nil {
				t.Fatalf("control failed: loader accepted %q, so this case proves nothing", tc.body)
			}

			if autoShareEnabledAtBoot() {
				t.Fatalf("auto-share read as ON with an unreadable config (%q); must fail safe to OFF", tc.body)
			}
		})
	}
}

// TestAutoShareEnabledAtBootFailsSafeWhenConfigIsADirectory covers the
// non-JSON read failures: the loader cannot even read the path.
func TestAutoShareEnabledAtBootFailsSafeWhenConfigIsADirectory(t *testing.T) {
	isolateConfig(t)

	p, err := config.ActiveOcodeConfigPath()
	if err != nil {
		t.Fatalf("resolve config path: %v", err)
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir in place of config: %v", err)
	}
	if _, err := config.LoadOcodeConfigCopy(); err == nil {
		t.Fatal("control failed: loader accepted a directory as config")
	}

	if autoShareEnabledAtBoot() {
		t.Fatal("auto-share read as ON with a directory in place of the config; must fail safe to OFF")
	}
}

// TestStartAutoShareIfEnabledSpawnsNothingWhenOff is the zero-spawn assertion.
//
// autoShareEnabledAtBoot on its own only returns a bool; it cannot show that no
// `tailscale serve` process was started. startAutoShareIfEnabled is the whole
// gate — it returns BEFORE launching crashguard.Go when the config does not opt
// in — so a false return proves the goroutine was never created and therefore no
// tailscale process was spawned.
//
// Scope, stated honestly: the spawn happens inside a crashguard.Go goroutine, so
// this test proves the NEGATIVE case only (nothing is launched). It does not
// exercise the positive path, which would need a fake tailscale CLI that
// package desktop cannot install (internal/tailscale resolves its binary from
// PATH and absolute candidates). The positive path is covered one layer down in
// internal/server: TestAutoShareWarmsTheSameExposureSlot asserts exactly one
// exposure per slot.
func TestStartAutoShareIfEnabledSpawnsNothingWhenOff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	isolateConfig(t)

	srv := server.New("127.0.0.1:0", "u", "tok", nil)

	if startAutoShareIfEnabled(srv) {
		t.Fatal("startAutoShareIfEnabled returned true with auto-share off; " +
			"the boot gate would have spawned a tailscale serve process nobody asked for")
	}
}

// TestStartAutoShareIfEnabledSpawnsNothingOnUnreadableConfig is the same
// zero-spawn guarantee for the corruption path, where the decision comes from
// the error branch rather than a false field.
func TestStartAutoShareIfEnabledSpawnsNothingOnUnreadableConfig(t *testing.T) {
	for _, body := range []string{"{not json", `{"auto_share_on_start":`} {
		t.Run(body, func(t *testing.T) {
			isolateConfig(t)
			writeConfig(t, body)
			if _, err := config.LoadOcodeConfigCopy(); err == nil {
				t.Fatalf("control failed: loader accepted %q", body)
			}

			srv := server.New("127.0.0.1:0", "u", "tok", nil)
			if startAutoShareIfEnabled(srv) {
				t.Fatalf("unreadable config (%q) started auto-share; must fail safe to no spawn", body)
			}
		})
	}
}
