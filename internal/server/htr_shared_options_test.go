package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestSharedModeAdoptsHTRcliConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".htrcli")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"),
		[]byte(`{"server":"http://127.0.0.1:3845","token":"tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	opts, _ := resolveManagedHTROptions(config.BrowserConfig{
		HTREnabled: true,
		HTRShared:  true,
		HTRPort:    3846, // legacy value that shared mode must ignore
	})
	if !opts.Enabled {
		t.Skip("host browser gate disabled HTR on this machine")
	}
	if opts.Shared.Mode != "shared" {
		t.Fatalf("mode = %q, want shared", opts.Shared.Mode)
	}
	if opts.Port != 3845 {
		t.Errorf("port = %d, want 3845 (the htrcli config port, not the legacy 3846)", opts.Port)
	}
	if opts.Shared.AdoptOnly {
		t.Error("a readable config with a token must not be AdoptOnly")
	}
	if want := filepath.Join(home, ".htrcli", "daemon.sock"); opts.SocketPath != want {
		t.Errorf("socket = %q, want %q", opts.SocketPath, want)
	}
}

func TestSharedModeMissingConfigIsAdoptOnlyWithNamedPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	opts, _ := resolveManagedHTROptions(config.BrowserConfig{
		HTREnabled: true,
		HTRShared:  true,
	})
	if !opts.Enabled {
		t.Skip("host browser gate disabled HTR on this machine")
	}
	if !opts.Shared.AdoptOnly {
		t.Fatal("a missing htrcli config must produce AdoptOnly")
	}
	want := filepath.Join(home, ".htrcli", "config.json")
	if !strings.Contains(opts.Shared.Notice, want) {
		t.Errorf("notice %q must name the config path %q", opts.Shared.Notice, want)
	}
}

// LoadBrowseOptions and StartBrowse relay BrowserConfig field by field. If
// either drops HTRShared, every user silently resolves to the private daemon
// and nothing fails.
//
// Both htr_shared values are asserted, because one is not enough. A dropped
// field leaves the struct literal's zero value false, which is indistinguishable
// from a correctly relayed false: asserting only "false in the file gives false
// out" passes with the field removed. The true case is what catches the drop
// (zero value false != true), and the false case is what catches a relay that
// ignores the file and hardcodes the default.
func TestBrowseOptionsCarryHTRSharedAndToken(t *testing.T) {
	for _, tc := range []struct {
		name    string
		inFile  string
		wantOut bool
	}{
		{name: "shared on", inFile: "true", wantOut: true},
		{name: "shared off", inFile: "false", wantOut: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("HOME", tmp)
			// GlobalConfigDir prefers $XDG_CONFIG_HOME off darwin and $APPDATA on
			// Windows; point all three at the same temp root so this test cannot
			// accidentally read the developer's real config on a future platform.
			t.Setenv("XDG_CONFIG_HOME", tmp)
			t.Setenv("APPDATA", tmp)
			// Ask config where it will look rather than assuming a layout.
			cfgDir, err := config.GlobalConfigDir()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(cfgDir, 0o755); err != nil {
				t.Fatal(err)
			}
			body := `{"browser":{"htr_shared":` + tc.inFile + `,"htr_token":"htr_secret"}}`
			if err := os.WriteFile(filepath.Join(cfgDir, "ocodeconfig.json"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			opts := LoadBrowseOptions(nil)
			if opts.HTRShared != tc.wantOut {
				t.Fatalf("LoadBrowseOptions relayed htr_shared=%s as %v, want %v",
					tc.inFile, opts.HTRShared, tc.wantOut)
			}
			if opts.HTRToken != "htr_secret" {
				t.Fatalf("LoadBrowseOptions dropped htr_token: %q", opts.HTRToken)
			}
		})
	}
}

// browserConfigFromBrowseOptions is the literal StartBrowse hands to
// resolveManagedHTROptions, so this walks the exact path a real startup takes:
// options -> config -> resolution. Dropping HTRShared at either hop lands in
// private mode, which is the silent failure this whole file exists to catch.
func TestBrowseOptionsReachSharedResolution(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".htrcli")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"),
		[]byte(`{"server":"http://127.0.0.1:3845","token":"htrcli_tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	opts := &BrowseOptions{HTREnabled: true, HTRShared: true, HTRToken: "htr_secret"}
	resolved, _ := resolveManagedHTROptions(browserConfigFromBrowseOptions(opts))
	if !resolved.Enabled {
		t.Skip("host browser gate disabled HTR on this machine")
	}
	if resolved.Shared.Mode != "shared" {
		t.Fatalf("mode = %q, want shared: the StartBrowse relay dropped htr_shared", resolved.Shared.Mode)
	}
	if resolved.Shared.Token != "htr_secret" {
		t.Fatalf("token = %q, want the ocode-config override", resolved.Shared.Token)
	}
	if resolved.Shared.TokenSource != "ocode-config" {
		t.Errorf("token source = %q, want ocode-config", resolved.Shared.TokenSource)
	}
	if resolved.Port != 3845 {
		t.Errorf("port = %d, want 3845 from the htrcli config", resolved.Port)
	}
}

// ensureSharedSocketDir backs the shared socket with a directory. cdp's
// resolver is pure, so nothing else creates one, and a daemon that cannot bind
// its socket never becomes healthy.
func TestEnsureSharedSocketDir(t *testing.T) {
	socket := filepath.Join(t.TempDir(), ".htrcli", "daemon.sock")
	if err := ensureSharedSocketDir(socket, false); err != nil {
		t.Fatalf("ensureSharedSocketDir: %v", err)
	}
	if st, err := os.Stat(filepath.Dir(socket)); err != nil || !st.IsDir() {
		t.Fatalf("socket dir not created: err=%v", err)
	}

	// Adopt-only means ocode never spawns, so it must not create a directory in
	// the user's home for a daemon it will not launch.
	adoptOnlySocket := filepath.Join(t.TempDir(), "untouched", "daemon.sock")
	if err := ensureSharedSocketDir(adoptOnlySocket, true); err != nil {
		t.Fatalf("ensureSharedSocketDir(AdoptOnly): %v", err)
	}
	if _, err := os.Stat(filepath.Dir(adoptOnlySocket)); !os.IsNotExist(err) {
		t.Errorf("AdoptOnly must not create %s", filepath.Dir(adoptOnlySocket))
	}

	// An empty socket resolves to nothing; the managed path has its own creation.
	if err := ensureSharedSocketDir("  ", false); err != nil {
		t.Errorf("empty socket must be a no-op, got %v", err)
	}
}
