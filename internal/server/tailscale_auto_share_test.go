package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// tsStub records which tailscale exposure was requested and how many times, so
// a test can prove two callers share ONE exposure slot (and therefore one
// process / one global mount).
type tsStub struct {
	mu     sync.Mutex
	calls  []string
	serveN int
	fullN  int
}

// record is the exposeFn seam. It returns a URL and the PROVEN kind ("serve"
// for tailnet-only, "funnel" for public) so callers see a live exposure; a test
// that needs a failure installs its own func.
func (s *tsStub) record(kind string) (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, kind)
	if kind == "serve" {
		s.serveN++
		return "https://host.ts.net/desktop", "serve"
	}
	s.fullN++
	return "https://host.ts.net/desktop", "funnel"
}

func (s *tsStub) count(kind string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind == "serve" {
		return s.serveN
	}
	return s.fullN
}

func (s *tsStub) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

// TestAutoShareWarmsTheSameExposureSlot is the core idempotency guard.
//
// `tailscale serve --bg --set-path /desktop` is a single GLOBAL mount per node.
// If auto-share at boot used a slot separate from the Share dialog's, opening
// the dialog would start a second process that silently OVERWRITES the mount's
// target. Asserting the boot exposure is requested exactly once pins "one slot,
// one process".
//
// The dialog's later Start is the ONE deliberate exception: it upgrades the
// warm tailnet-only mount to a public funnel, so a node whose auto-share warmed
// `serve` can still deliver the public URL the Share button advertises. The
// upgrade mounts funnel on its own port and leaves the warm serve mount in place
// (Stop clears both), so a failed funnel attempt never destroys a working share.
func TestAutoShareWarmsTheSameExposureSlot(t *testing.T) {
	stub := &tsStub{}
	share := &tailscaleShare{}
	share.exposeFn = stub.record
	var removed []string
	share.removeFn = func(path string) { removed = append(removed, path) }

	if _, _ = share.ensureServe(1234); stub.count("serve") != 1 {
		t.Fatalf("serve exposure calls = %d, want 1", stub.count("serve"))
	}

	url, _ := share.ensure(1234)
	if stub.count("full") != 1 {
		t.Fatalf("funnel-first exposure ran %d times, want 1 (the serve -> funnel upgrade)", stub.count("full"))
	}
	if st := share.status(); !st.Running || st.Kind != "funnel" {
		t.Fatalf("after the upgrade = %+v, want running/funnel", st)
	}
	if st := share.status(); st.URL != url {
		t.Fatalf("status url %q != ensure url %q", st.URL, url)
	}
	// The upgrade must NOT tear the warm serve mount down first: funnel mounts
	// on its own port, and removing before the funnel is proven would destroy a
	// working share if the funnel attempt fails.
	if len(removed) != 0 {
		t.Fatalf("upgrade remove calls = %v, want none (the warm mount survives until Stop)", removed)
	}
}

// TestManualUpgradeKeepsServeWhenFunnelFails pins the no-downtime half of the
// upgrade: when the funnel re-expose proves nothing (funnel not enabled on the
// node, or the attempt fails), the share must fall back to a serve mount rather
// than be left torn down. StartExposeWithKind's internal serve fallback is what
// provides this, so the seam reports it as kind "serve".
func TestManualUpgradeKeepsServeWhenFunnelFails(t *testing.T) {
	share := &tailscaleShare{}
	share.removeFn = func(string) {}
	// Warm with serve; the upgrade's funnel attempt then falls back to serve, as
	// StartExposeWithKind does when funnel is not enabled on the node.
	share.exposeFn = func(string) (string, string) {
		return "https://host.ts.net/desktop", "serve"
	}

	if st := share.start("serve", 1234); st.Kind != "serve" {
		t.Fatalf("precondition kind = %q, want serve", st.Kind)
	}
	st := share.start("full", 1234)
	if !st.Running || st.Kind != "serve" {
		t.Fatalf("failed upgrade must leave a running serve mount, got %+v", st)
	}
}

// TestAutoShareNeverDowngradesWarmedFunnel pins the other direction: once an
// explicit share has gone public, a later auto-share call must not withdraw it
// by replacing the funnel with a tailnet-only mount.
func TestAutoShareNeverDowngradesWarmedFunnel(t *testing.T) {
	stub := &tsStub{}
	share := &tailscaleShare{}
	share.exposeFn = stub.record
	share.removeFn = func(string) {}

	if st := share.start("full", 1234); st.Kind != "funnel" {
		t.Fatalf("precondition kind = %q, want funnel", st.Kind)
	}
	st := share.start("serve", 1234)
	if st.Kind != "funnel" {
		t.Fatalf("auto-share downgraded a public share to %q; it must reuse the warm funnel", st.Kind)
	}
	if stub.count("serve") != 0 {
		t.Fatalf("auto-share re-exposed serve %d times over a warm funnel", stub.count("serve"))
	}
}

// TestEnsureServeCachesLikeEnsure pins that the serve-only path caches exactly
// like the existing one: a second call is served from memory.
func TestEnsureServeCachesLikeEnsure(t *testing.T) {
	stub := &tsStub{}
	share := &tailscaleShare{}
	share.exposeFn = stub.record

	first, _ := share.ensureServe(1234)
	second, _ := share.ensureServe(9999) // different port: must still be ignored
	if stub.count("serve") != 1 {
		t.Fatalf("serve exposure calls = %d, want 1", stub.count("serve"))
	}
	if first != second {
		t.Fatalf("cached url changed: %q then %q", first, second)
	}
}

// TestCleanupReleasesTheAutoStartedShare proves the boot-started exposure is
// torn down with the server, so a quit never leaves an orphaned serve mount
// pointing at a dead port.
func TestCleanupReleasesTheAutoStartedShare(t *testing.T) {
	stub := &tsStub{}
	share := &tailscaleShare{}
	share.exposeFn = stub.record
	share.removeFn = func(string) { stub.record("remove") }

	if _, _ = share.ensureServe(1234); stub.count("serve") != 1 {
		t.Fatalf("serve exposure calls = %d, want 1", stub.count("serve"))
	}
	if share.path == "" {
		t.Fatal("a successful exposure must record its mount path for cleanup")
	}

	share.cleanup()

	if share.started {
		t.Fatal("cleanup must reset started so a later exposure can start")
	}
	if share.path != "" {
		t.Fatalf("cleanup left mount path %q behind", share.path)
	}
	var removed bool
	for _, c := range stub.snapshot() {
		if c == "remove" {
			removed = true
		}
	}
	if !removed {
		t.Fatal("cleanup did not remove the --set-path mount")
	}
}

// TestStartAutoShareUnconfiguredServerIsNoOp keeps the boot hook safe on a
// server with no exposure subsystem: it reports unavailable instead of
// panicking, so boot can never be blocked by the feature.
func TestStartAutoShareUnconfiguredServerIsNoOp(t *testing.T) {
	srv := New("127.0.0.1:0", "ocode", "tok", nil)
	srv.tsShare = nil // simulate a server built without the subsystem

	url, hint := srv.StartAutoShare()
	if url != "" || hint != "" {
		t.Fatalf("StartAutoShare on a server with no exposure = %q/%q, want empty", url, hint)
	}
}

// TestAutoShareStatusEndpointReusesTheWarmedExposure is the end-to-end shape:
// after auto-share warms the cache, GET /api/tailscale-share reports the cached
// URL as running, and reading it never starts a second exposure.
func TestAutoShareStatusEndpointReusesTheWarmedExposure(t *testing.T) {
	stub := &tsStub{}
	share := &tailscaleShare{}
	share.exposeFn = stub.record

	srv := &Server{tsShare: share, password: "tok"}

	if _, _ = share.ensureServe(1234); stub.count("serve") != 1 {
		t.Fatalf("serve exposure calls = %d, want 1", stub.count("serve"))
	}

	srv.tsShare.exposeFn = func(kind string) (string, string) {
		t.Errorf("status read must not re-expose, got %q", kind)
		return "", ""
	}
	rec := httptest.NewRecorder()
	srv.handleGetTailscaleShare(rec, httptest.NewRequest("GET", "/api/tailscale-share", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (availability belongs in the body)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"running":true`) {
		t.Fatalf("body should report the warmed exposure as running: %s", body)
	}
	if !strings.Contains(body, `"kind":"serve"`) {
		t.Fatalf("body should report the tailnet-only kind: %s", body)
	}
}
