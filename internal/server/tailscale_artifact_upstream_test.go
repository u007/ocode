package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// artifactShareInstances returns every artifact exposure instance the Server
// builds. All four must carry the /p upstream: each one is a path to the same
// public /p mount.
func artifactShareInstances(srv *Server) map[string]*tailscaleShare {
	return map[string]*tailscaleShare{
		"artifactShare":          srv.artifactShare,
		"artifactTailscaleShare": srv.artifactTailscaleShare,
		"tsArtifactShare":        srv.tsArtifactShare,
		"artifactsShare":         srv.artifactsShare,
	}
}

// captureTarget wires a share so start() records the upstream target it hands
// to expose, with no CLI and no real exposure.
func captureTarget(share *tailscaleShare) *[]string {
	var targets []string
	share.exposeTargetFn = func(target string) { targets = append(targets, target) }
	share.exposeFn = (&tsStub{}).record
	share.removeFn = func(string) {}
	return &targets
}

// TestArtifactShareUpstreamKeepsPathPrefix pins the security fix: the artifact
// mount's upstream is localhost:<port>/p, not the server root. With the root
// upstream, `--set-path /p` strips the prefix and a public /p/api/... reaches
// the owner's authenticated API.
func TestArtifactShareUpstreamKeepsPathPrefix(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	isolateConfigDir(t)
	srv := New("127.0.0.1:1234", "user", "pass", nil)

	for name, share := range artifactShareInstances(srv) {
		if share.upstreamPath != "/p" {
			t.Errorf("%s.upstreamPath = %q, want /p", name, share.upstreamPath)
		}
		targets := captureTarget(share)
		share.start("serve", 1234)
		if len(*targets) != 1 || (*targets)[0] != "localhost:1234/p" {
			t.Errorf("%s serve target = %v, want [localhost:1234/p]", name, *targets)
		}
	}
}

// TestArtifactShareFunnelTargetKeepsPathPrefix covers the public (funnel)
// attempt too: the funnel and serve commands both receive the same target.
func TestArtifactShareFunnelTargetKeepsPathPrefix(t *testing.T) {
	share := newArtifactTailscaleShare()
	targets := captureTarget(share)

	share.start("full", 1234)
	if len(*targets) != 1 || (*targets)[0] != "localhost:1234/p" {
		t.Fatalf("funnel target = %v, want [localhost:1234/p]", *targets)
	}
}

// TestDesktopShareTargetUnchanged guards the non-artifact share: its upstream
// stays the bare server root, exactly as before the artifact fix.
func TestDesktopShareTargetUnchanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	isolateConfigDir(t)
	srv := New("127.0.0.1:1234", "user", "pass", nil)

	if srv.tsShare.upstreamPath != "" {
		t.Fatalf("desktop share upstreamPath = %q, want empty", srv.tsShare.upstreamPath)
	}
	if got := srv.tsShare.mountPath(); got != desktopTailscalePath {
		t.Fatalf("desktop mountPath = %q, want %q", got, desktopTailscalePath)
	}
	targets := captureTarget(srv.tsShare)
	srv.tsShare.start("serve", 1234)
	srv.tsShare.start("full", 1234)
	for _, got := range *targets {
		if got != "localhost:1234" {
			t.Fatalf("desktop target = %q, want localhost:1234", got)
		}
	}
	if len(*targets) == 0 {
		t.Fatal("desktop share never reached the expose seam")
	}
}

// TestUpstreamTargetTable pins the helper for both instance kinds, including a
// trailing slash on the input.
func TestUpstreamTargetTable(t *testing.T) {
	artifact := newArtifactTailscaleShare()
	desktop := &tailscaleShare{}
	cases := []struct {
		name   string
		share  *tailscaleShare
		target string
		want   string
	}{
		{"artifact", artifact, "localhost:4096", "localhost:4096/p"},
		{"artifact trailing slash", artifact, "localhost:4096/", "localhost:4096/p"},
		{"desktop unchanged", desktop, "localhost:4096", "localhost:4096"},
	}
	for _, tc := range cases {
		if got := tc.share.upstreamTarget(tc.target); got != tc.want {
			t.Errorf("%s: upstreamTarget(%q) = %q, want %q", tc.name, tc.target, got, tc.want)
		}
	}
}

// TestArtifactShareFunnelRefusedWithoutAuth: with no credentials configured, a
// public funnel request is refused with 403 and nothing is exposed. A tailnet
// serve request still succeeds, because it is not public.
func TestArtifactShareFunnelRefusedWithoutAuth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	isolateConfigDir(t)
	srv := New("127.0.0.1:1234", "", "", nil)
	stub := &tsStub{}
	srv.artifactShare.exposeFn = stub.record
	srv.artifactShare.removeFn = func(string) {}
	h := srv.serveHandler()

	for _, q := range []string{"?funnel=1", "?public=1"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/artifacts/expose/start"+q, nil))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("POST start%s without auth = %d, want 403 (body=%s)", q, rec.Code, rec.Body.String())
		}
	}
	if stub.count("full") != 0 || stub.count("serve") != 0 {
		t.Fatalf("refused funnel still reached expose: full=%d serve=%d", stub.count("full"), stub.count("serve"))
	}
	if st := srv.artifactShare.status(); st.Running {
		t.Fatalf("refused funnel left a running share: %+v", st)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/artifacts/expose/start", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("tailnet-only start without auth = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if stub.count("serve") != 1 {
		t.Fatalf("tailnet-only start did not expose: serve=%d", stub.count("serve"))
	}
}

// TestArtifactShareFunnelAllowedWithAuth: with credentials configured, an
// authenticated funnel request is allowed and reaches the funnel attempt.
func TestArtifactShareFunnelAllowedWithAuth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	isolateConfigDir(t)
	srv := New("127.0.0.1:1234", "user", "pass", nil)
	stub := &tsStub{}
	srv.artifactShare.exposeFn = stub.record
	srv.artifactShare.removeFn = func(string) {}
	h := srv.serveHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/artifacts/expose/start?funnel=1", nil)
	req.SetBasicAuth("user", "pass")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated funnel start = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if stub.count("full") != 1 {
		t.Fatalf("authenticated funnel did not attempt funnel: full=%d", stub.count("full"))
	}
}

// TestAuthConfigured pins the single definition shared by authMiddleware and
// the funnel gate.
func TestAuthConfigured(t *testing.T) {
	cases := []struct {
		name       string
		user, pass string
		remote     bool
		want       bool
	}{
		{"none", "", "", false, false},
		{"username only", "user", "", false, true},
		{"password only", "", "pass", false, true},
		{"both", "user", "pass", false, true},
		{"remote mode", "", "", true, true},
	}
	for _, tc := range cases {
		s := &Server{username: tc.user, password: tc.pass, remoteMode: tc.remote}
		if got := s.authConfigured(); got != tc.want {
			t.Errorf("%s: authConfigured() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
