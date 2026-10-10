package server

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// The htrcli bearer token used throughout this file. It exists so the tests can
// assert it never appears in any response body: htr_token_set is a boolean
// precisely so the secret does not cross the wire, and a boolean that quietly
// grew a sibling field carrying the value would be a real leak.
const htrTestToken = "htr-secret-token-do-not-leak"

// setHTRBrowserConfig writes HTR browser fields into the handler's in-memory
// config. testConfigHandler gives each test a temp HOME, so nothing here can
// touch the developer's real ocodeconfig.json.
func setHTRBrowserConfig(t *testing.T, h *Handler, mutate func(*config.BrowserConfig)) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		h.cfg = &config.Config{}
	}
	mutate(&h.cfg.Ocode.Browser)
}

// writeHTRcliConfig writes a readable htrcli config into home, which is what
// lets shared mode resolve real coordinates instead of collapsing to
// adopt-only. Returns the path written.
func writeHTRcliConfig(t *testing.T, home, body string) string {
	t.Helper()
	dir := filepath.Join(home, ".htrcli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func decodeJSONBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal %s: %v", rec.Body.String(), err)
	}
	return body
}

// A shared config that cannot be read leaves ocode adopt-only: it may use a
// daemon the user runs, but it must never claim it can start one. The response
// has to say so AND name the action only the user can take, or the settings UI
// can only render a Start button that cannot succeed.
//
// Catches: dropping AdoptOnly from the response, and reporting the refusal as a
// bare error with no adopt-only explanation.
func TestStartHTRRefusesInAdoptOnlyWithReason(t *testing.T) {
	h := testConfigHandler(t)
	stubHTRSeams(t)
	htrOptionsFn = func(config.BrowserConfig) (cdp.HTROptions, string) {
		return cdp.HTROptions{Enabled: true, Shared: cdp.SharedDaemon{Mode: "shared", AdoptOnly: true, ConfigPath: "/nope/.htrcli/config.json"}}, ""
	}

	rec := httptest.NewRecorder()
	h.HandleStartHTR(rec, httptest.NewRequest("POST", "/api/config/ocode/htr/start", nil))
	body := decodeJSONBody(t, rec)

	if body["adopt_only"] != true {
		t.Errorf("adopt_only = %v, want true", body["adopt_only"])
	}
	notice, _ := body["notice"].(string)
	if !strings.Contains(notice, "htrcli serve") {
		t.Errorf("notice %q must tell the user to start `htrcli serve` themselves", notice)
	}
	if where, _ := body["config_path"].(string); where != "/nope/.htrcli/config.json" {
		t.Errorf("config_path = %q, want the path resolution reported", where)
	}
	if mode, _ := body["mode"].(string); mode != "shared" {
		t.Errorf("mode = %q, want shared", mode)
	}
}

// ocode stops only a daemon it spawned. cdp.StopHTRServe signals that by
// handing back a still-running status with a nil error, so the API has to
// translate "still running" into an explicit stopped:false plus a reason. A 200
// with no such field is indistinguishable from success to a caller.
//
// Catches: dropping the Stopped field, and a stop path that never consults the
// stop result (e.g. deciding from a fresh probe, which races the refusal).
func TestStopHTRRefusesForeignDaemonWithReason(t *testing.T) {
	h := testConfigHandler(t)
	stubHTRSeams(t)
	stopHTRServeFn = func(*tool.ProcessSupervisor, int, *log.Logger) (cdp.HTRStatus, error) {
		return cdp.HTRStatus{Running: true, Addr: "127.0.0.1:3845"}, nil
	}

	rec := httptest.NewRecorder()
	h.HandleStopHTR(rec, httptest.NewRequest("POST", "/api/config/ocode/htr/stop", nil))
	body := decodeJSONBody(t, rec)

	if body["stopped"] != false {
		t.Errorf("stopped = %v, want false", body["stopped"])
	}
	if reason, _ := body["reason"].(string); reason == "" {
		t.Error("a refused stop must carry a reason")
	}
}

// The complement of the refusal: a daemon that really went down must report
// stopped:true and no reason. Without this, a stop path that hardcodes
// stopped:false (or omits the field entirely and lets the UI guess) would still
// pass the refusal test.
func TestStopHTRReportsStoppedWhenTheDaemonGoesDown(t *testing.T) {
	h := testConfigHandler(t)
	stubHTRSeams(t)

	rec := httptest.NewRecorder()
	h.HandleStopHTR(rec, httptest.NewRequest("POST", "/api/config/ocode/htr/stop", nil))
	body := decodeJSONBody(t, rec)

	if body["stopped"] != true {
		t.Errorf("stopped = %v, want true", body["stopped"])
	}
	if reason, _ := body["reason"].(string); reason != "" {
		t.Errorf("a completed stop must not carry a refusal reason, got %q", reason)
	}
}

// A plain status poll must not claim a stop was attempted. `stopped` is
// omitempty precisely so the UI can tell "the daemon is up" from "I asked and it
// refused"; emitting stopped:false here would make a running daemon look
// refused.
//
// Catches: making Stopped a non-pointer bool, which is the obvious way to write
// this field and silently breaks exactly this distinction.
func TestHTRStatusPollOmitsStopped(t *testing.T) {
	h := testConfigHandler(t)
	stubHTRSeams(t)

	rec := httptest.NewRecorder()
	h.HandleGetHTRStatus(rec, httptest.NewRequest("GET", "/api/config/ocode/htr", nil))
	body := decodeJSONBody(t, rec)

	if _, present := body["stopped"]; present {
		t.Errorf("a status poll must omit stopped, got %v", body["stopped"])
	}
	if _, present := body["reason"]; present {
		t.Errorf("a status poll must omit reason, got %v", body["reason"])
	}
}

// The settings UI gates its Stop button on started_by_ocode, so the field has to
// reach it truthfully in both directions. A live daemon ocode may stop reports
// its pid and started_by_ocode:true; one it may not reports the pid and false.
//
// Catches: inverting the flag, hardcoding it from `managed` (which is true for
// any ocode-marked daemon, including another instance's), and dropping the
// provenance seam so both sides read false.
func TestHTRStatusReportsDaemonPIDAndStartedByOcode(t *testing.T) {
	for _, tc := range []struct {
		name          string
		provenance    cdp.HTRProvenance
		wantStartedBy bool
	}{
		{"ocode spawned it", cdp.HTRProvenance{DaemonPID: 4242, StartedByOcode: true}, true},
		{"another instance owns it", cdp.HTRProvenance{DaemonPID: 777, StartedByOcode: false}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testConfigHandler(t)
			stubHTRSeams(t)
			htrProvenanceFn = func(int) cdp.HTRProvenance { return tc.provenance }
			// A running daemon is the precondition for consulting provenance at
			// all: a marker whose pid is gone names a process that no longer
			// exists, and its number would read as a live daemon.
			htrDaemonStatusFn = func(port int, socketPath, _ string) cdp.HTRDaemonInfo {
				return cdp.HTRDaemonInfo{Running: true, Managed: true, Addr: "127.0.0.1:3846", Port: 3846}
			}

			rec := httptest.NewRecorder()
			h.HandleGetHTRStatus(rec, httptest.NewRequest("GET", "/api/config/ocode/htr", nil))
			body := decodeJSONBody(t, rec)

			if got := body["daemon_pid"]; got != float64(tc.provenance.DaemonPID) {
				t.Errorf("daemon_pid = %v, want %d", got, tc.provenance.DaemonPID)
			}
			if got := body["started_by_ocode"]; got != tc.wantStartedBy {
				t.Errorf("started_by_ocode = %v, want %v", got, tc.wantStartedBy)
			}
		})
	}
}

// A stopped daemon has no live pid, so a leftover marker must not surface one:
// the UI would read it as a running process.
//
// Catches: consulting provenance unconditionally (i.e. dropping the
// info.Running gate), which is the simplification that looks harmless.
func TestHTRStatusHidesDaemonPIDWhenStopped(t *testing.T) {
	h := testConfigHandler(t)
	stubHTRSeams(t)
	provenanceAsked := false
	htrProvenanceFn = func(int) cdp.HTRProvenance {
		provenanceAsked = true
		return cdp.HTRProvenance{DaemonPID: 4242, StartedByOcode: true}
	}
	htrDaemonStatusFn = func(port int, socketPath, _ string) cdp.HTRDaemonInfo {
		return cdp.HTRDaemonInfo{Running: false, Addr: "127.0.0.1:3846", Port: 3846}
	}

	rec := httptest.NewRecorder()
	h.HandleGetHTRStatus(rec, httptest.NewRequest("GET", "/api/config/ocode/htr", nil))
	body := decodeJSONBody(t, rec)

	if got := body["daemon_pid"]; got != float64(0) {
		t.Errorf("daemon_pid = %v, want 0 for a stopped daemon", got)
	}
	if provenanceAsked {
		t.Error("provenance must not be consulted for a stopped daemon; it costs a health probe on every settings poll")
	}
}

// The effective values must come from htrcli's own config, not from the legacy
// ocode fields: a stale htr_port of 3846 is still persisted while the shared
// daemon answers on 3845, and echoing the legacy number back is what makes the
// settings page look broken.
//
// Catches: wiring effective_port to bcfg.HTRPort, and reporting the legacy port
// as the effective one. Note the deliberate mismatch — the configured port is
// 3846 and the effective one is 3845, so swapping them fails.
func TestGetBrowserConfigReportsSharedProvenanceAndEffectiveValues(t *testing.T) {
	// testConfigHandler installs the temp HOME, so it must run first: setting
	// HOME here and then calling it would write the htrcli config into a
	// directory the handler never reads, and every shared field would resolve
	// adopt-only for the wrong reason.
	h := testConfigHandler(t)
	home := os.Getenv("HOME")
	// HOME covers macOS (paths.OcodeGlobalDataDir is HOME-derived there);
	// XDG_DATA_HOME covers Linux CI, which would otherwise read the real one.
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))
	cfgPath := writeHTRcliConfig(t, home, `{"server":"http://127.0.0.1:3845","token":"`+htrTestToken+`"}`)

	setHTRBrowserConfig(t, h, func(b *config.BrowserConfig) {
		b.HTRShared = true
		b.HTRPort = 3846 // legacy value shared mode ignores
	})

	rec := httptest.NewRecorder()
	h.HandleGetBrowserConfig(rec, httptest.NewRequest("GET", "/api/config/ocode/browser", nil))
	body := decodeJSONBody(t, rec)

	if body["htr_shared"] != true {
		t.Errorf("htr_shared = %v, want true", body["htr_shared"])
	}
	if got := body["htr_port"]; got != float64(3846) {
		t.Errorf("htr_port = %v, want the configured 3846 so the legacy field stays editable", got)
	}
	if got := body["effective_port"]; got != float64(3845) {
		t.Errorf("effective_port = %v, want 3845 from htrcli's config", got)
	}
	if got, _ := body["effective_socket"].(string); got != filepath.Join(home, ".htrcli", "daemon.sock") {
		t.Errorf("effective_socket = %q, want htrcli's own socket path", got)
	}
	if got, _ := body["token_source"].(string); got != "htrcli-config" {
		t.Errorf("token_source = %q, want htrcli-config", got)
	}
	if got, _ := body["config_path"].(string); got != cfgPath {
		t.Errorf("config_path = %q, want %q", got, cfgPath)
	}
	if body["adopt_only"] != false {
		t.Errorf("adopt_only = %v, want false for a readable config with a token", body["adopt_only"])
	}
}

// The bearer token is the whole reason htr_token_set is a boolean. This asserts
// the negative across the WHOLE response rather than one field: a leak would
// most plausibly arrive as a new `htr_token` sibling, and a per-field assertion
// would not notice it.
//
// Catches: adding a token field to the response, and any provenance field that
// accidentally carries the token value instead of its source label.
func TestGetBrowserConfigNeverEmitsTheHTRToken(t *testing.T) {
	h := testConfigHandler(t)
	home := os.Getenv("HOME")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))
	writeHTRcliConfig(t, home, `{"server":"http://127.0.0.1:3845","token":"`+htrTestToken+`"}`)

	setHTRBrowserConfig(t, h, func(b *config.BrowserConfig) {
		b.HTRShared = true
		b.HTRToken = htrTestToken // the ocode-side override, same secret value
	})

	rec := httptest.NewRecorder()
	h.HandleGetBrowserConfig(rec, httptest.NewRequest("GET", "/api/config/ocode/browser", nil))
	decodeJSONBody(t, rec)

	if strings.Contains(rec.Body.String(), htrTestToken) {
		t.Errorf("the response leaks the htrcli bearer token: %s", rec.Body.String())
	}
	if body := decodeJSONBody(t, rec); body["htr_token_set"] != true {
		t.Errorf("htr_token_set = %v, want true (a boolean, never the value)", body["htr_token_set"])
	}
}

// htr_token_set is a boolean precisely so the token value never crosses the
// wire, which makes it the client's ONLY signal for whether a token exists. A
// hardcoded `true` would satisfy every other case in this file, so the unset
// direction is asserted separately.
//
// Catches: htr_token_set pinned to a constant. (Mutation-verified: the
// always-true version of this field survived every other test in the file.)
func TestGetBrowserConfigReportsWhetherATokenIsConfigured(t *testing.T) {
	for _, tc := range []struct {
		name  string
		token string
		want  bool
	}{
		{"no token configured", "", false},
		{"ocode-config token set", htrTestToken, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testConfigHandler(t)
			home := os.Getenv("HOME")
			t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))
			// A readable htrcli config is what keeps the resolution out of
			// adopt-only, so the assertion below is about the token flag alone.
			writeHTRcliConfig(t, home, `{"server":"http://127.0.0.1:3845","token":"`+htrTestToken+`"}`)

			setHTRBrowserConfig(t, h, func(b *config.BrowserConfig) {
				b.HTRShared = true
				b.HTRToken = tc.token
			})

			rec := httptest.NewRecorder()
			h.HandleGetBrowserConfig(rec, httptest.NewRequest("GET", "/api/config/ocode/browser", nil))
			body := decodeJSONBody(t, rec)

			if body["htr_token_set"] != tc.want {
				t.Errorf("htr_token_set = %v, want %v", body["htr_token_set"], tc.want)
			}
			if strings.Contains(rec.Body.String(), htrTestToken) {
				t.Errorf("the response leaks the htrcli bearer token: %s", rec.Body.String())
			}
		})
	}
}

// The status endpoint carries the same shared provenance, because the Stop
// button's disabled state is decided there. Without token_source/config_path a
// user who cannot stop the daemon has no way to learn where it came from.
//
// Catches: populating the shared fields on the browser-config endpoint only.
func TestHTRStatusCarriesSharedProvenance(t *testing.T) {
	h := testConfigHandler(t)
	home := os.Getenv("HOME")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))
	cfgPath := writeHTRcliConfig(t, home, `{"server":"http://127.0.0.1:3845","token":"`+htrTestToken+`"}`)

	setHTRBrowserConfig(t, h, func(b *config.BrowserConfig) { b.HTRShared = true })
	stubHTRSeams(t)

	rec := httptest.NewRecorder()
	h.HandleGetHTRStatus(rec, httptest.NewRequest("GET", "/api/config/ocode/htr", nil))
	body := decodeJSONBody(t, rec)

	if got, _ := body["mode"].(string); got != "shared" {
		t.Errorf("mode = %q, want shared", got)
	}
	if got, _ := body["token_source"].(string); got != "htrcli-config" {
		t.Errorf("token_source = %q, want htrcli-config", got)
	}
	if got, _ := body["config_path"].(string); got != cfgPath {
		t.Errorf("config_path = %q, want %q", got, cfgPath)
	}
	if strings.Contains(rec.Body.String(), htrTestToken) {
		t.Errorf("the status response leaks the htrcli bearer token: %s", rec.Body.String())
	}
}
