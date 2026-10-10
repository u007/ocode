package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

// TestShareStatusNeverStartsAnExposure is the safety property the status
// endpoint exists for: merely opening the Share dialog / Settings must not
// publish the instance. status() must not touch the exposure seam at all.
func TestShareStatusNeverStartsAnExposure(t *testing.T) {
	share := &tailscaleShare{}
	share.exposeFn = func(kind string) (string, string) {
		t.Fatalf("status() started an exposure (%q); reads must be side-effect free", kind)
		return "", ""
	}

	st := share.status()
	if st.Running {
		t.Fatal("a fresh share must report not running")
	}
	if st.URL != "" {
		t.Fatalf("fresh share reported a url %q", st.URL)
	}
}

// TestManualStartIsFunnelFirstAndReportsPublic pins the user-chosen semantics:
// the explicit Start button keeps funnel-first behaviour, and the status says
// "public" so the UI can warn about it.
func TestManualStartIsFunnelFirstAndReportsPublic(t *testing.T) {
	stub := &tsStub{}
	share := &tailscaleShare{}
	share.exposeFn = stub.record

	st := share.start("full", 1234)
	if st.Running != true {
		t.Fatalf("started share should be running: %+v", st)
	}
	if st.Kind != "funnel" {
		t.Fatalf("manual Start kind = %q, want funnel (public)", st.Kind)
	}
	if stub.count("full") != 1 {
		t.Fatalf("funnel-first exposure calls = %d, want 1", stub.count("full"))
	}

	// A second Start must reuse the live exposure, not start another.
	share.start("full", 1234)
	if stub.count("full") != 1 {
		t.Fatalf("second Start re-exposed; calls = %d, want 1", stub.count("full"))
	}
}

// TestAutoShareStartStaysTailnetOnly pins that the boot path never goes public,
// and reports the tailnet-only kind.
func TestAutoShareStartStaysTailnetOnly(t *testing.T) {
	stub := &tsStub{}
	share := &tailscaleShare{}
	share.exposeFn = stub.record

	st := share.start("serve", 1234)
	if st.Kind != "serve" {
		t.Fatalf("auto-share kind = %q, want serve (tailnet-only)", st.Kind)
	}
	if stub.count("full") != 0 {
		t.Fatalf("auto-share must never try funnel; full calls = %d", stub.count("full"))
	}
}

// TestStatusTreatsUnprovenDNSNameURLAsNotRunning: StartExpose can return a bare
// DNS-name URL with an empty kind when neither funnel nor serve proved a mount.
// Presenting that as a live share would hand out a dead link, so it must read
// as "not running" and omit the url.
func TestStatusTreatsUnprovenDNSNameURLAsNotRunning(t *testing.T) {
	share := &tailscaleShare{}
	share.exposeFn = func(kind string) (string, string) { return "https://host.ts.net/desktop", "" }

	st := share.start("full", 1234)
	if st.Running {
		t.Fatalf("an unproven DNS-name URL must not report running: %+v", st)
	}
	if st.URL != "" {
		t.Fatalf("an unproven URL must be omitted from status, got %q", st.URL)
	}
}

// TestStopRemovesMountAndClearsState is the revocation guard: killing the
// process is not enough — the --set-path mount must be removed or the public
// funnel stays live.
func TestStopRemovesMountAndClearsState(t *testing.T) {
	stub := &tsStub{}
	share := &tailscaleShare{}
	share.exposeFn = stub.record
	var removed []string
	share.removeFn = func(path string) { removed = append(removed, path) }

	if st := share.start("full", 1234); !st.Running {
		t.Fatalf("precondition: share should be running: %+v", st)
	}

	if stopped := share.stop(); !stopped {
		t.Fatal("stop() reported nothing was running")
	}
	if len(removed) != 1 {
		t.Fatalf("remove calls = %v, want exactly one", removed)
	}

	st := share.status()
	if st.Running || st.URL != "" || st.Kind != "" {
		t.Fatalf("status after stop = %+v, want cleared", st)
	}

	// Idempotent: a second Stop is a no-op and must not remove an unrelated
	// mount again.
	if stopped := share.stop(); stopped {
		t.Fatal("second stop() reported something was running")
	}
	if len(removed) != 1 {
		t.Fatalf("second stop removed a mount again: %v", removed)
	}
}

// TestStopLeavesAutoShareSettingUntouched pins the contract that Stop is a
// runtime action only. If Stop also disabled auto_share_on_start, a user who
// stops to free a port would silently lose their next-launch share.
func TestStopLeavesAutoShareSettingUntouched(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	isolateConfigDir(t)

	if err := config.SaveAutoShareOnStart(true); err != nil {
		t.Fatalf("seed auto-share: %v", err)
	}

	srv := &Server{tsShare: &tailscaleShare{}}
	srv.tsShare.exposeFn = (&tsStub{}).record
	// The remove seam matters as much as the expose one: without it, Stop in this
	// test shells out to the REAL tailscale CLI and deletes the developer's live
	// `--set-path /desktop` mount (which it did, twice).
	srv.tsShare.removeFn = func(string) {}
	srv.tsShare.start("full", 1234)

	rec := httptest.NewRecorder()
	srv.handleStopTailscaleShare(rec, httptest.NewRequest(http.MethodPost, "/api/tailscale-share/stop", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("stop handler = %d, body=%s", rec.Code, rec.Body.String())
	}

	fresh, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if !fresh.AutoShareOnStart {
		t.Fatal("Stop disabled auto_share_on_start; it must leave the persisted setting alone")
	}
}

// TestShareRoutesAreRegistered drives the REAL mux rather than the handlers.
// A handler test cannot catch a missing route (the failure presents as a 404
// from the SPA fallback), which is exactly how the new status/start/stop
// endpoints would silently break.
func TestShareRoutesAreRegistered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	isolateConfigDir(t)

	srv := New("127.0.0.1:1234", "", "", nil)
	// Replace BOTH seams so no test ever shells out to tailscale: expose alone is
	// not enough, because this test drives Stop, whose mount removal otherwise
	// runs the real CLI against the developer's live node config.
	stub := &tsStub{}
	srv.tsShare.exposeFn = stub.record
	srv.tsShare.removeFn = func(string) {}
	h := srv.serveHandler()

	// Status read: 200, not running, and no exposure started.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tailscale-share", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/tailscale-share = %d body=%s (404 means the route is missing)", rec.Code, rec.Body.String())
	}
	var st tailscaleShareStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode status: %v body=%s", err, rec.Body.String())
	}
	if st.Running {
		t.Fatal("status before Start reported running")
	}
	if stub.count("full")+stub.count("serve") != 0 {
		t.Fatal("a status read started an exposure")
	}

	// Start (POST) exposes and reports running.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tailscale-share/start", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/tailscale-share/start = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if !st.Running || st.Kind != "funnel" {
		t.Fatalf("start response = %+v, want running/funnel", st)
	}

	// Stop (POST) tears it down.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tailscale-share/stop", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/tailscale-share/stop = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode stop: %v", err)
	}
	if st.Running {
		t.Fatalf("stop response = %+v, want not running", st)
	}
}

// TestConcurrentStartsShareOneExposure pins the opMu serialization: two
// concurrent starts must NOT both run the (process-spawning) exposure — the
// global `--set-path /desktop` mount can only belong to one of them. The seam
// blocks until released, so with opMu removed the second start enters the
// exposure too and this test fails (and -race flags the concurrent cache
// writes). Without serialization a Stop could also be resurrected by a racing
// start, which is the bug this guards.
func TestConcurrentStartsShareOneExposure(t *testing.T) {
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	share := &tailscaleShare{}
	share.exposeFn = func(kind string) (string, string) {
		entered <- struct{}{}
		<-release
		return "https://host.ts.net/desktop", "funnel"
	}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			share.start("full", 1234)
		}()
	}

	// Exactly one start may reach the exposure; the other must wait on opMu.
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("no start reached the exposure")
	}
	select {
	case <-entered:
		close(release)
		t.Fatal("two starts ran the exposure concurrently; opMu is not serializing them")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	wg.Wait()

	// The waiting start must have reused the first's cache, not exposed again.
	select {
	case <-entered:
		t.Fatal("start is not idempotent: a second exposure ran after the first finished")
	default:
	}
}
