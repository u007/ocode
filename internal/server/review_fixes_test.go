package server

// Regression tests for the HIGH review findings: sync_url watcher
// invalidation, project-rebind rejection (see also
// TestHandlerChatRejectsSessionProjectRebind in server_test.go), partial
// transcript persistence on turn failure, and shutdown admission control.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	ocodesync "github.com/u007/ocode/internal/sync"
)

// Changing sync_url must stop the old-endpoint watcher and drop the cached
// client immediately: otherwise the config-save event from the change itself
// triggers a push of secret-bearing blobs to the previous server.
func TestSetSyncURLStopsWatcherAndDropsClient(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	h := NewHandler()
	stopped := false
	h.syncMu.Lock()
	h.syncStop = func() { stopped = true }
	h.syncClientInst = ocodesync.NewClient("http://localhost:1")
	h.syncConfiguredURL = ""
	h.syncMu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/sync/url", strings.NewReader(`{"sync_url":"https://new.example"}`))
	h.HandleSetSyncURLConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	if !stopped {
		t.Errorf("changing sync_url did not stop the old-endpoint watcher")
	}
	if h.syncStop != nil {
		t.Errorf("syncStop was not cleared after URL change")
	}
	if h.syncClientInst != nil {
		t.Errorf("cached sync client was not dropped after URL change")
	}
}

// commitPartialTranscript (used by runTurn and the legacy SSE endpoint's
// error branch) must synchronously persist whatever the turn produced before
// failing, so a reload never loses streamed rounds — or the opening user
// message — to the asynchronous live writer.
func TestCommitPartialTranscriptPersistsSynchronously(t *testing.T) {
	proj := t.TempDir()
	h := NewHandler()
	const id = "sess-partial-persist"
	h.sessions.Register(id, proj)

	as := &agentSession{messages: []agent.Message{{Role: "user", Content: "hi"}}}
	partial := append(append([]agent.Message(nil), as.messages...),
		agent.Message{Role: "assistant", Content: "partial reply"})
	h.commitPartialTranscript(id, as, partial, false)

	if len(as.messages) != 2 || as.messages[1].Content != "partial reply" {
		t.Fatalf("in-memory transcript not updated: %+v", as.messages)
	}
	s, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("LoadForDir: %v", err)
	}
	if len(s.Messages) != 2 || s.Messages[0].Content != "hi" || s.Messages[1].Content != "partial reply" {
		t.Fatalf("partial transcript not persisted: %+v", s.Messages)
	}
}

// Once shutdown has begun, dispatchTurn must refuse new turns: a turn
// dispatched after the join starts could create plugin/model processes,
// register an agent, and write after shutdown began.
func TestDispatchTurnRefusedAfterShutdownBegins(t *testing.T) {
	h := NewHandler()
	h.shutdownMu.Lock()
	h.shutdownStarted = true
	h.shutdownMu.Unlock()

	if _, err := h.dispatchTurn("sess-shutdown-refuse", "model", "hi", turnOptions{}); err == nil {
		t.Fatalf("expected dispatchTurn to fail after shutdown began")
	}
}
