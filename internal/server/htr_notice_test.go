package server

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// TestResolveManagedHTROptionsNeverSilentlyEnablesHTR locks in the user-visible
// half of the missing-bundle contract.
//
// resolveManagedHTROptions turns a cdp.ResolveHTRAssetsForHost error into the
// notice shown to the user plus Enabled=false. An archive-less source build (no
// `htr` build tag) used to return empty asset paths with a nil error, so this
// function returned Enabled=true and an EMPTY notice — the browser panel then
// reported HTR as on with nothing to drive it, and nothing told the user.
//
// This runs in an untagged build only; with the tag the archive is present and
// resolution is expected to succeed.
func TestResolveManagedHTROptionsNeverSilentlyEnablesHTR(t *testing.T) {
	// A non-branded path skips both FindChrome and the branded-Chrome
	// compatibility notice, so the notice under test is unambiguously the
	// missing-bundle one rather than whatever Chrome happens to be installed.
	cfg := config.BrowserConfig{
		HTREnabled: true,
		ChromePath: filepath.Join(t.TempDir(), "chromium"),
	}
	opts, notice := resolveManagedHTROptions(cfg)
	if opts.Enabled {
		t.Fatalf("HTR must be disabled when its assets cannot be resolved, got Enabled=true notice=%q", notice)
	}
	if strings.TrimSpace(notice) == "" {
		t.Fatal("an unresolvable HTR configuration must carry a user-visible notice; an empty one hides the reason")
	}
	if !strings.Contains(notice, "HTR automation is unavailable") {
		t.Fatalf("notice must use the documented unavailable wording, got: %q", notice)
	}
	if !strings.Contains(notice, "Browsing continues without the HTR extension") {
		t.Fatalf("notice must state that browsing continues, got: %q", notice)
	}
}

// TestResolveManagedHTROptionsDisabledIsQuiet is the control: HTR switched off
// in config is a deliberate user choice and must stay silent, so the notice
// above cannot be satisfied by always emitting one.
func TestResolveManagedHTROptionsDisabledIsQuiet(t *testing.T) {
	opts, notice := resolveManagedHTROptions(config.BrowserConfig{HTREnabled: false})
	if opts.Enabled {
		t.Fatal("HTR must stay disabled when the user turned it off")
	}
	if notice != "" {
		t.Fatalf("user-disabled HTR must be quiet, got notice: %q", notice)
	}
}
