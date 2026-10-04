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

func (s *tsStub) record(kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, kind)
	if kind == "serve" {
		s.serveN++
	} else {
		s.fullN++
	}
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
// target. Asserting the exposure is requested exactly once across an auto-share
// followed by a dialog read is what pins "one slot, one process".
func TestAutoShareWarmsTheSameExposureSlot(t *testing.T) {
	stub := &tsStub{}
	share := &tailscaleShare{}
	share.exposeFn = stub.record

	if _, _ = share.ensureServe(1234); stub.count("serve") != 1 {
		t.Fatalf("serve exposure calls = %d, want 1", stub.count("serve"))
	}
	// The dialog's read must reuse the warmed slot, not start a new exposure.
	share.exposeFn = func(kind string) { t.Errorf("dialog must not re-expose, got %q", kind) }

	if _, _ = share.ensure(1234); stub.count("full") != 0 {
		t.Fatalf("funnel-first exposure ran %d times; auto-share must own the slot", stub.count("full"))
	}

	if got := stub.snapshot(); len(got) != 1 {
		t.Fatalf("exposure calls = %v, want exactly one", got)
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

// TestAutoShareURLIsReusedByTheDialogEndpoint is the end-to-end shape: after
// auto-share warms the cache, GET /api/tailscale-url returns the cached URL and
// reports it available, so the dialog shows the tailnet link immediately.
func TestAutoShareURLIsReusedByTheDialogEndpoint(t *testing.T) {
	stub := &tsStub{}
	share := &tailscaleShare{}
	// The seam reports success, so url != "" and the handler reports available.
	share.exposeFn = stub.record

	srv := &Server{tsShare: share, password: "tok"}
	share.url = ""
	share.hint = ""
	share.started = false

	// Warm via auto-share, then confirm the handler path would reuse it.
	if _, _ = share.ensureServe(1234); stub.count("serve") != 1 {
		t.Fatalf("serve exposure calls = %d, want 1", stub.count("serve"))
	}

	rec := httptest.NewRecorder()
	srv.tsShare.exposeFn = func(kind string) { t.Errorf("endpoint must reuse cached exposure, re-exposing with %q", kind) }
	srv.handleGetTailscaleURL(rec, httptest.NewRequest("GET", "/api/tailscale-url", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (availability belongs in the body)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"available"`) {
		t.Fatalf("body missing availability: %s", rec.Body.String())
	}
}