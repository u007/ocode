package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestArtifactExposureMountsOnce proves the Mode A artifact exposure uses a
// single global /p/* mount: a second expose reuses the live one instead of
// clobbering it (which would orphan the first mount and its artifact routes).
func TestArtifactExposureMountsOnce(t *testing.T) {
	stub := &tsStub{}
	share := &tailscaleShare{mount: artifactTailscalePath}
	share.exposeFn = stub.record
	share.removeFn = func(string) {}

	st := share.start("serve", 1234)
	if !st.Running {
		t.Fatalf("first artifact expose should be running: %+v", st)
	}
	// Second expose (any kind that is not an upgrade) must reuse, not re-mount.
	share.start("serve", 1234)
	if stub.count("serve")+stub.count("full") != 1 {
		t.Fatalf("second expose re-mounted; calls=%d+%d, want 1",
			stub.count("serve"), stub.count("full"))
	}
}

// TestArtifactExposureFunnelOnlyOnExplicitConfirmation pins that an unattended
// (serve) exposure never touches funnel, and that funnel is only attempted on
// an explicit "full" request (user confirmation).
func TestArtifactExposureFunnelOnlyOnExplicitConfirmation(t *testing.T) {
	stub := &tsStub{}
	share := &tailscaleShare{mount: artifactTailscalePath}
	share.exposeFn = stub.record
	share.removeFn = func(string) {}

	// Unattended path: tailnet-only, no funnel.
	st := share.start("serve", 1234)
	if st.Kind != "serve" {
		t.Fatalf("unattended artifact expose kind = %q, want serve", st.Kind)
	}
	if stub.count("full") != 0 {
		t.Fatalf("unattended expose tried funnel: full calls = %d", stub.count("full"))
	}

	// A fresh share with an explicit full request may funnel.
	stub2 := &tsStub{}
	share2 := &tailscaleShare{mount: artifactTailscalePath}
	share2.exposeFn = stub2.record
	share2.removeFn = func(string) {}
	st2 := share2.start("full", 1234)
	if st2.Kind != "funnel" {
		t.Fatalf("explicit artifact expose kind = %q, want funnel", st2.Kind)
	}
	if stub2.count("full") != 1 {
		t.Fatalf("explicit expose did not funnel: full calls = %d", stub2.count("full"))
	}
}

// TestArtifactExposureRoutesRegistered drives the real mux so a missing route
// (which would surface as a 404 from the SPA fallback) is caught.
func TestArtifactExposureRoutesRegistered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	isolateConfigDir(t)

	srv := New("127.0.0.1:1234", "", "", nil)
	stub := &tsStub{}
	srv.artifactShare.exposeFn = stub.record
	srv.artifactShare.removeFn = func(string) {}
	srv.artifactTailscaleShare.exposeFn = stub.record
	srv.artifactTailscaleShare.removeFn = func(string) {}
	srv.tsArtifactShare.exposeFn = stub.record
	srv.tsArtifactShare.removeFn = func(string) {}
	srv.artifactsShare.exposeFn = stub.record
	srv.artifactsShare.removeFn = func(string) {}
	h := srv.serveHandler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/artifact-tailscale-share", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/artifact-tailscale-share = %d body=%s", rec.Code, rec.Body.String())
	}
	var st tailscaleShareStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.Running {
		t.Fatal("status before start reported running")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/artifact-tailscale-share/start", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST start = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if !st.Running {
		t.Fatalf("start response not running: %+v", st)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/artifact-tailscale-share/stop", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST stop = %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestArtifactExposureDoesNotClobberDesktopMount ensures the artifact mount is a
// separate instance from the desktop share, so exposing artifacts never
// overwrites /desktop.
func TestArtifactExposureDoesNotClobberDesktopMount(t *testing.T) {
	desktopStub := &tsStub{}
	artifactStub := &tsStub{}
	desktop := &tailscaleShare{}
	desktop.exposeFn = desktopStub.record
	desktop.removeFn = func(string) {}
	artifact := &tailscaleShare{mount: artifactTailscalePath}
	artifact.exposeFn = artifactStub.record
	artifact.removeFn = func(string) {}

	desktop.start("full", 1234)
	artifact.start("full", 1234)

	if desktopStub.count("full") != 1 {
		t.Fatalf("desktop expose count = %d, want 1", desktopStub.count("full"))
	}
	if artifactStub.count("full") != 1 {
		t.Fatalf("artifact expose count = %d, want 1", artifactStub.count("full"))
	}
}
