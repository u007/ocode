package server

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// isolateHTRHome points HOME and every data-dir override at a fresh temp dir.
//
// resolveManagedHTROptions extracts the embedded HTR archive and
// cdp.EnsureHTRServe writes lease/owner markers, all under
// paths.OcodeGlobalDataDir — which is HOME-derived on darwin, so XDG_DATA_HOME
// alone would leave this test writing into the developer's real
// ~/.local/share/opencode and reading their real ~/.htrcli.
func isolateHTRHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "localappdata"))
	// htrcli's own override would redirect the health probe off the resolved
	// coordinates, making the expected error depend on the host's daemon.
	t.Setenv("HTR_PORT", "")
	t.Setenv("HTRCLI_PATH", "")
	t.Setenv("OCODE_HTRCLI_PATH", "")
}

// A user who switched HTR off must be distinguishable from a failure: the TUI
// startup hook logs the two differently, and only the sentinel lets it.
//
// Mutant: returning a plain errors.New("HTR is disabled") so errors.Is cannot
// match.
func TestEnsureSharedHTRDaemonReportsDisabledAsTheSentinel(t *testing.T) {
	st, err := EnsureSharedHTRDaemon(nil, config.BrowserConfig{HTREnabled: false}, nil)
	if !errors.Is(err, ErrHTRDisabled) {
		t.Fatalf("err = %v, want ErrHTRDisabled so a caller can tell 'switched off' from 'failed'", err)
	}
	// cdp.HTRStatus carries a func field, so it is not comparable with ==; the
	// fields a caller could act on are checked individually instead. A non-empty
	// Addr here would mean the disabled branch still ran the ensure.
	if st.Running || st.Owned || st.StartedByOcode || st.Addr != "" || st.Socket != "" || st.Binary != "" || st.Notice != "" || st.Release != nil {
		t.Errorf("status = %+v, want the zero value when HTR is disabled", st)
	}
}

// A resolution gate that fails must surface its notice verbatim, so the user is
// told what to fix. Branded Chrome is the gate used here because it is decided
// purely from the configured path — no filesystem probing, so the assertion does
// not depend on which browsers the host happens to have installed.
//
// Mutant: swallowing the notice and reporting ErrHTRDisabled instead, or
// truncating it.
func TestEnsureSharedHTRDaemonSurfacesTheResolutionNoticeVerbatim(t *testing.T) {
	isolateHTRHome(t)
	browser := config.BrowserConfig{
		HTREnabled: true,
		HTRShared:  true,
		ChromePath: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	}
	_, want := resolveManagedHTROptions(browser)
	if want == "" {
		t.Skip("branded-Chrome notice did not fire on this platform; the gate is path-based and should not depend on the host")
	}
	st, err := EnsureSharedHTRDaemon(nil, browser, nil)
	if err == nil {
		t.Fatalf("expected the resolution notice to fail the ensure, got status %+v", st)
	}
	if errors.Is(err, ErrHTRDisabled) {
		t.Fatal("a failed gate must not be reported as ErrHTRDisabled; the user disabled nothing")
	}
	if err.Error() != want {
		t.Errorf("err = %q, want the resolution notice verbatim: %q", err.Error(), want)
	}
}

// The wrapper must actually reach cdp.EnsureHTRServe with the resolved options —
// this is the seam that gives a plain TUI session the same daemon the desktop
// app starts. With no supervisor and no htrcli config the resolution is
// adopt-only, so the ensure refuses with its adopt-only message; reaching that
// message is the proof the options were threaded through rather than swallowed.
//
// Mutant: returning early on a successful resolution instead of calling
// cdp.EnsureHTRServe.
func TestEnsureSharedHTRDaemonReachesTheEnsureWithResolvedOptions(t *testing.T) {
	isolateHTRHome(t)
	browser := config.BrowserConfig{
		HTREnabled:        true,
		HTRShared:         true,
		ChromePath:        "/Applications/Chromium.app/Contents/MacOS/Chromium",
		HTRNativeHostName: "com.ocode.htrcontrol",
	}
	opts, notice := resolveManagedHTROptions(browser)
	if !opts.Enabled {
		t.Fatalf("resolution failed on this host: %q; the rest of this test is about the enabled path", notice)
	}
	if !opts.Shared.AdoptOnly {
		t.Fatalf("expected adopt-only with no htrcli config in the temp HOME, got %+v", opts.Shared)
	}

	st, err := EnsureSharedHTRDaemon(nil, browser, nil)
	if err == nil {
		t.Fatalf("expected an adopt-only refusal with no supervisor, got status %+v", st)
	}
	if errors.Is(err, ErrHTRDisabled) {
		t.Fatalf("err = %v, want the adopt-only refusal from cdp.EnsureHTRServe", err)
	}
	// The port is the load-bearing part: shared mode resolved to htrcli's own
	// 3845, not ocode's legacy 3846, so seeing 3845 in the refusal proves the
	// *shared* coordinates reached cdp.EnsureHTRServe rather than the private
	// defaults. The adopt-only wording proves the AdoptOnly resolution did too.
	if !strings.Contains(err.Error(), "127.0.0.1:3845") {
		t.Errorf("err = %q, want the shared port 3845, which proves the resolved options reached cdp.EnsureHTRServe", err.Error())
	}
	if !strings.Contains(err.Error(), "adopt-only") {
		t.Errorf("err = %q, want the adopt-only refusal, which proves AdoptOnly reached cdp.EnsureHTRServe", err.Error())
	}
}
