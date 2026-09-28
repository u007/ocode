package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Regression coverage for docs/gotchas/remote-project-path-trust-boundary.md:
// a saved remote (SSH/WSL) project's Path must never be usable as a LOCAL
// filesystem root. Before the fix, `allowedProjectRoots` and
// `isRegisteredProjectRoot` appended every saved project's Path regardless of
// Host, so a saved {host:"example.com", path:"/"} let a host-less request
// browse/run on the local machine at that path.

func hasRoot(roots []string, want string) bool {
	for _, r := range roots {
		if r == want {
			return true
		}
	}
	return false
}

// The local allowlist contains the workdir and local projects only; remote
// entries — including a broad "/" — are excluded.
func TestAllowedProjectRootsExcludesRemoteEntries(t *testing.T) {
	h := testProjectHandler(t)
	h.workDir = t.TempDir()
	localRoot := t.TempDir()

	if err := h.projects.Add(localRoot); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := h.projects.AddRemote("devbox", "/srv/remote"); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	// The documented exploit precondition: a remote project whose path is the
	// local filesystem root.
	if err := h.projects.AddRemote("evil", "/"); err != nil {
		t.Fatalf("AddRemote(/): %v", err)
	}

	roots := h.allowedProjectRoots()
	if !hasRoot(roots, h.workDir) {
		t.Errorf("workDir %q missing from roots %v", h.workDir, roots)
	}
	if !hasRoot(roots, localRoot) {
		t.Errorf("local project %q missing from roots %v", localRoot, roots)
	}
	if hasRoot(roots, "/srv/remote") {
		t.Errorf("remote path /srv/remote admitted as a local root: %v", roots)
	}
	if hasRoot(roots, "/") {
		t.Errorf("remote root \"/\" admitted as a local root: %v", roots)
	}
}

// isRegisteredProjectRoot is the git/fs-mutation gate; it must ignore remote
// entries for the same reason.
func TestIsRegisteredProjectRootExcludesRemote(t *testing.T) {
	h := testProjectHandler(t)
	localRoot := t.TempDir()
	remoteRoot := t.TempDir()

	if err := h.projects.Add(localRoot); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := h.projects.AddRemote("devbox", remoteRoot); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}

	if !h.isRegisteredProjectRoot(localRoot) {
		t.Errorf("local root %q not registered", localRoot)
	}
	if h.isRegisteredProjectRoot(remoteRoot) {
		t.Errorf("remote path %q treated as a local registered root", remoteRoot)
	}
}

// A host-less file-tree request for a path that is only registered as a
// remote project must be rejected (it would otherwise serve the LOCAL
// directory of that name; pre-fix this returned 200).
func TestLocalFileTreeRejectsRemoteProjectPath(t *testing.T) {
	h := testProjectHandler(t)
	h.workDir = t.TempDir()
	remoteDir := t.TempDir()
	if err := h.projects.AddRemote("devbox", remoteDir); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/files/tree?path="+url.QueryEscape(remoteDir), nil)
	rr := httptest.NewRecorder()
	h.HandleFileTree(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rr.Code, rr.Body.String())
	}
}

// A host-less git-status request for a path that is only registered as a
// remote project must be rejected as an unknown local project (pre-fix it
// resolved and ran git against the LOCAL directory).
func TestLocalGitStatusRejectsRemoteProjectPath(t *testing.T) {
	h := testProjectHandler(t)
	h.workDir = t.TempDir()
	remoteDir := t.TempDir()
	if err := h.projects.AddRemote("devbox", remoteDir); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/git/status?project="+url.QueryEscape(remoteDir), nil)
	rr := httptest.NewRecorder()
	h.HandleGitStatus(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rr.Code, rr.Body.String())
	}
}

// The local terminal history/process trust boundary (the same check the
// websocket path mirrors): no host + a remote-only path is forbidden, while
// the registered (host, path) pair is admitted.
func TestLocalTerminalRejectsRemoteProjectPath(t *testing.T) {
	h := testProjectHandler(t)
	remoteDir := t.TempDir()
	if err := h.projects.AddRemote("devbox", remoteDir); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}

	// Host-less local request: must be rejected.
	req := httptest.NewRequest(http.MethodGet, "/api/terminal/history?project="+url.QueryEscape(remoteDir), nil)
	if _, status, msg := h.resolveTerminalHistoryProject(req); status != http.StatusForbidden {
		t.Fatalf("host-less remote path status = %d (%s), want 403", status, msg)
	}

	// The registered remote identity is admitted.
	req = httptest.NewRequest(
		http.MethodGet,
		"/api/terminal/history?host=devbox&project="+url.QueryEscape(remoteDir),
		nil,
	)
	if got, status, msg := h.resolveTerminalHistoryProject(req); status != 0 {
		t.Fatalf("registered remote pair status = %d (%s), want 0", status, msg)
	} else if got != "devbox:"+remoteDir {
		t.Fatalf("resolved project = %q, want %q", got, "devbox:"+remoteDir)
	}
}

// A local and a remote project sharing one path are distinct identities: the
// shared path appears once (as the local project) and a same-path remote entry
// never makes the path local by itself.
func TestSharedPathLocalAndRemoteStayDistinct(t *testing.T) {
	h := testProjectHandler(t)
	h.workDir = ""
	shared := t.TempDir()
	if err := h.projects.Add(shared); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := h.projects.AddRemote("devbox", shared); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}

	roots := h.allowedProjectRoots()
	count := 0
	for _, r := range roots {
		if r == shared {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("shared path appears %d times in %v, want 1 (the local entry)", count, roots)
	}

	// Without the local entry, the same path is not a local root.
	h2 := testProjectHandler(t)
	h2.workDir = ""
	remoteOnly := t.TempDir()
	if err := h2.projects.AddRemote("devbox", remoteOnly); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	if hasRoot(h2.allowedProjectRoots(), remoteOnly) {
		t.Fatalf("remote-only path %q became a local root: %v", remoteOnly, h2.allowedProjectRoots())
	}
}
