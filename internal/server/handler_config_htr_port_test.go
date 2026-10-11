package server

import (
	"encoding/json"
	"log"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// recordHTRPorts stubs the four HTR seams that take a port and records the
// port each was called with, so a test can assert WHICH daemon ocode addressed.
//
// Every stub is restored by the returned cleanup func; the seams are package
// vars shared by the whole test binary, so a test that installs one without
// restoring it corrupts every later test in the package.
func recordHTRPorts(t *testing.T) *struct {
	Status, Provenance, Stop, Tabs int
	StatusSocket, StopSocket       string
} {
	t.Helper()
	got := &struct {
		Status, Provenance, Stop, Tabs int
		StatusSocket, StopSocket       string
	}{Status: -1, Provenance: -1, Stop: -1, Tabs: -1}

	origEnsure, origStop, origStatus := ensureHTRServeFn, stopHTRServeFn, htrDaemonStatusFn
	origTabs, origProv, origOpts := listHTRTabsFn, htrProvenanceFn, htrOptionsFn
	t.Cleanup(func() {
		ensureHTRServeFn, stopHTRServeFn, htrDaemonStatusFn = origEnsure, origStop, origStatus
		listHTRTabsFn, htrProvenanceFn, htrOptionsFn = origTabs, origProv, origOpts
	})

	ensureHTRServeFn = func(*tool.ProcessSupervisor, cdp.HTROptions, *log.Logger) (cdp.HTRStatus, error) {
		return cdp.HTRStatus{Running: true}, nil
	}
	htrDaemonStatusFn = func(port int, socketPath, _ string) cdp.HTRDaemonInfo {
		got.Status, got.StatusSocket = port, socketPath
		return cdp.HTRDaemonInfo{Running: true, Managed: true, Port: port}
	}
	htrProvenanceFn = func(port int) cdp.HTRProvenance {
		got.Provenance = port
		return cdp.HTRProvenance{DaemonPID: 4242, StartedByOcode: true}
	}
	stopHTRServeFn = func(_ *tool.ProcessSupervisor, port int, _ *log.Logger) (cdp.HTRStatus, error) {
		got.Stop = port
		return cdp.HTRStatus{Running: false}, nil
	}
	listHTRTabsFn = func(port int, _ string) ([]cdp.HTRTab, error) {
		got.Tabs = port
		return nil, nil
	}
	// Start is not under test here, but it shares the seams: pin it to the
	// resolved port too so the test states the whole invariant rather than only
	// the three endpoints it happens to call.
	htrOptionsFn = func(browser config.BrowserConfig) (cdp.HTROptions, string) {
		shared := cdp.ResolveSharedDaemon(cdp.HTRSharedInput{
			Token:        browser.HTRToken,
			Shared:       browser.HTRShared,
			LegacyPort:   browser.HTRPort,
			LegacySocket: browser.HTRSocketPath,
		})
		return cdp.HTROptions{Enabled: browser.HTREnabled, Port: shared.Port, SocketPath: shared.Socket, Shared: shared}, ""
	}
	return got
}

// In shared mode the daemon listens on htrcli's port (3845 by default), not on
// browser.htr_port. startManagedHTR has always started it there, because
// resolveManagedHTROptions overwrites the port with the resolved one — so every
// SETTINGS endpoint that addressed browser.htr_port was looking at a port
// nothing served. The visible symptom is a live daemon reported as Stopped, a
// "List tabs" that always errors, and a Stop that returns stopped:true while
// the daemon keeps running.
//
// Catches: htrDaemonStatusFn, htrProvenanceFn, stopHTRServeFn or listHTRTabsFn
// called with bcfg.HTRPort instead of the resolved shared.Port.
func TestHTRSettingsEndpointsTargetResolvedPortInSharedMode(t *testing.T) {
	h := testConfigHandler(t)
	got := recordHTRPorts(t)
	// Default shared config: htr_shared on, the legacy 3846 left in place (the
	// documented private-mode port), and no htrcli config in the temp HOME, so
	// resolution falls back to cdp.DefaultHTRCLIPort.
	setHTRBrowserConfig(t, h, func(b *config.BrowserConfig) {
		b.HTREnabled = true
		b.HTRShared = true
		b.HTRPort = 3846
	})

	h.HandleGetHTRStatus(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/config/ocode/htr", nil))
	h.HandleStopHTR(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/config/ocode/htr/stop", nil))
	h.HandleListHTRTabs(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/config/ocode/htr/tabs", nil))

	if want := cdp.DefaultHTRCLIPort; got.Status != want {
		t.Errorf("htrDaemonStatusFn port = %d, want the resolved %d (not the configured 3846)", got.Status, want)
	}
	if want := cdp.DefaultHTRCLIPort; got.Provenance != want {
		t.Errorf("htrProvenanceFn port = %d, want the resolved %d", got.Provenance, want)
	}
	if want := cdp.DefaultHTRCLIPort; got.Stop != want {
		t.Errorf("stopHTRServeFn port = %d, want the resolved %d", got.Stop, want)
	}
	if want := cdp.DefaultHTRCLIPort; got.Tabs != want {
		t.Errorf("listHTRTabsFn port = %d, want the resolved %d", got.Tabs, want)
	}
}

// Private mode must be unaffected: ResolveSharedDaemon echoes the legacy port
// and socket verbatim, so routing the settings endpoints through the resolved
// descriptor has to keep addressing exactly what the config says. This is the
// half of the fix that would be silently wrong if the resolver ever stopped
// echoing — it would start reporting the wrong port for the legacy daemon
// instead of only the shared one.
func TestHTRSettingsEndpointsKeepConfiguredPortInPrivateMode(t *testing.T) {
	h := testConfigHandler(t)
	got := recordHTRPorts(t)
	setHTRBrowserConfig(t, h, func(b *config.BrowserConfig) {
		b.HTREnabled = true
		b.HTRShared = false
		b.HTRPort = 3846
		b.HTRSocketPath = "/tmp/htr.sock"
	})

	h.HandleGetHTRStatus(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/config/ocode/htr", nil))

	if got.Status != 3846 {
		t.Errorf("htrDaemonStatusFn port = %d, want the configured 3846 in private mode", got.Status)
	}
	if got.StatusSocket != "/tmp/htr.sock" {
		t.Errorf("htrDaemonStatusFn socket = %q, want the configured /tmp/htr.sock in private mode", got.StatusSocket)
	}
}

// A htrcli config that names its own loopback port moves the daemon again, so
// the fallback default is not the only coordinate that has to be honoured. The
// settings endpoints must follow the resolver, not a hardcoded constant.
func TestHTRSettingsEndpointsFollowHtrcliConfiguredPort(t *testing.T) {
	h := testConfigHandler(t)
	got := recordHTRPorts(t)
	// resolveHTRShared reads os.UserHomeDir, so the config has to be written
	// into the home this test installs, not a throwaway directory.
	home := t.TempDir()
	writeHTRcliConfig(t, home, `{"server":"http://127.0.0.1:9911","token":"tok"}`)
	t.Setenv("HOME", home)
	setHTRBrowserConfig(t, h, func(b *config.BrowserConfig) {
		b.HTREnabled = true
		b.HTRShared = true
		b.HTRPort = 3846
	})

	h.HandleGetHTRStatus(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/config/ocode/htr", nil))

	if got.Status != 9911 {
		t.Errorf("htrDaemonStatusFn port = %d, want 9911 from htrcli's config", got.Status)
	}
}

// The status response's own Port field is read by the settings UI, so it must
// describe the daemon that was actually probed rather than the legacy one the
// user typed into the form a moment earlier.
func TestHTRStatusReportsResolvedPort(t *testing.T) {
	h := testConfigHandler(t)
	recordHTRPorts(t)
	setHTRBrowserConfig(t, h, func(b *config.BrowserConfig) {
		b.HTREnabled = true
		b.HTRShared = true
		b.HTRPort = 3846
	})

	rec := httptest.NewRecorder()
	h.HandleGetHTRStatus(rec, httptest.NewRequest("GET", "/api/config/ocode/htr", nil))
	var st htrStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Port != cdp.DefaultHTRCLIPort {
		t.Errorf("status.port = %d, want the resolved %d", st.Port, cdp.DefaultHTRCLIPort)
	}
	if st.Mode != "shared" {
		t.Errorf("status.mode = %q, want shared", st.Mode)
	}
}
