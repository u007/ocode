//go:build !windows

package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// The local terminal history/process trust boundary (the same check the
// websocket path mirrors): no host + a remote-only path is forbidden, while
// the registered (host, path) pair is admitted.
//
// It lives here because it calls h.resolveTerminalHistoryProject, which is
// Unix-only (terminal_history.go is //go:build !windows); keeping it in the
// untagged file broke the Windows build of this package's tests.
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
