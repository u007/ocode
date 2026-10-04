package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// newAutoShareHandler builds a Handler wired the way New wires the real one:
// the in-memory cfg plus a cached-exposure snapshot that MUST NOT be allowed to
// start an exposure.
func newAutoShareHandler(enabled bool, url string) *Handler {
	h := &Handler{}
	h.cfg = &config.Config{}
	h.cfg.Ocode.AutoShareOnStart = enabled
	h.tailscaleShareSnapshot = func() (string, string) { return url, "" }
	return h
}

func putAutoShare(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("PUT", "/api/config/ocode/auto-share", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleSetAutoShareConfig(rec, r)
	return rec
}

// TestGetAutoShareConfigReportsPersistedToggle is the read path the Settings UI
// depends on.
func TestGetAutoShareConfigReportsPersistedToggle(t *testing.T) {
	isolateConfigDir(t)

	for _, want := range []bool{false, true} {
		h := newAutoShareHandler(want, "")
		rec := httptest.NewRecorder()
		h.HandleGetAutoShareConfig(rec, httptest.NewRequest("GET", "/api/config/ocode/auto-share", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"enabled":`+boolStr(want)) {
			t.Fatalf("body %s missing enabled=%v", rec.Body.String(), want)
		}
	}
}

// TestGetAutoShareConfigNeverStartsAnExposure is the safety property that makes
// this endpoint safe to poll.
//
// If a read started the exposure, merely OPENING Settings would publish the
// instance — the precise surprise the opt-in default exists to prevent. The
// snapshot seam is wired to panic if it is asked to do anything but report, and
// the assertion is that the endpoint is happy with a plain cached value.
func TestGetAutoShareConfigNeverStartsAnExposure(t *testing.T) {
	isolateConfigDir(t)

	h := newAutoShareHandler(true, "https://host.ts.net/desktop")
	// A server with NO exposure at all must still answer 200: the toggle is
	// meaningful on a machine without tailscale.
	h2 := newAutoShareHandler(true, "")

	for _, hh := range []*Handler{h, h2} {
		rec := httptest.NewRecorder()
		hh.HandleGetAutoShareConfig(rec, httptest.NewRequest("GET", "/api/config/ocode/auto-share", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 even with no exposure", rec.Code)
		}
	}
}

// TestSetAutoShareConfigPersistsAndEchoes covers the write path, including that
// the response reflects what was STORED rather than what was requested.
func TestSetAutoShareConfigPersistsAndEchoes(t *testing.T) {
	isolateConfigDir(t)

	h := newAutoShareHandler(false, "")
	rec := putAutoShare(t, h, `{"enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Fatalf("body %s should echo enabled=true", rec.Body.String())
	}

	// Persisted, not just in-memory: a fresh read from disk must see it.
	fresh, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !fresh.AutoShareOnStart {
		t.Fatal("PUT did not persist auto_share_on_start=true")
	}

	// And the in-memory cfg the server keeps must agree.
	if !h.cfg.Ocode.AutoShareOnStart {
		t.Fatal("in-memory cfg still reports auto-share off after a successful PUT")
	}
}

// TestSetAutoShareConfigTurnsOff proves the toggle is writable in both
// directions, not a one-way latch.
func TestSetAutoShareConfigTurnsOff(t *testing.T) {
	isolateConfigDir(t)

	h := newAutoShareHandler(true, "")
	if rec := putAutoShare(t, h, `{"enabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	fresh, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if fresh.AutoShareOnStart {
		t.Fatal("auto_share_on_start persisted as true after disabling")
	}
}

// TestSetAutoShareConfigRejectsBadBody keeps a malformed PUT from silently
// reading as "disable everything".
func TestSetAutoShareConfigRejectsBadBody(t *testing.T) {
	isolateConfigDir(t)

	h := newAutoShareHandler(false, "")
	// Establish a real persisted ON value first. Setting only h.cfg would make
	// the guard vacuous: the on-disk default is false, so "unchanged" would
	// look identical to "never turned on".
	if rec := putAutoShare(t, h, `{"enabled":true}`); rec.Code != http.StatusOK {
		t.Fatalf("setup PUT failed: %d %s", rec.Code, rec.Body.String())
	}

	rec := putAutoShare(t, h, `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	fresh, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !fresh.AutoShareOnStart {
		t.Fatal("a rejected PUT must not change the stored value")
	}
	if !h.cfg.Ocode.AutoShareOnStart {
		t.Fatal("a rejected PUT must not change the in-memory cfg")
	}
}

// isolateConfigDir points config writes at a temp dir so these tests never touch
// the developer's real ocodeconfig.json.
func isolateConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("OPENCODE_CONFIG_DIR", t.TempDir())
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
