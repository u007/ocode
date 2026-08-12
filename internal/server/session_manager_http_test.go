package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/session"
)

// TestCrossProjectSessionHandlers is the Task 3 acceptance test: a session
// whose on-disk home is a registered project different from the server's own
// workdir must load via GET /api/sessions/:id and /api/sessions/:id/context
// (previously these 404'd because session loading resolved against the
// server's workdir only). A truly-missing session still 404s.
func TestCrossProjectSessionHandlers(t *testing.T) {
	h := NewHandler()
	// The server's own workdir gets one session; a second, distinct project
	// root gets another. The handler must resolve both.
	workDir := t.TempDir()
	otherProj := t.TempDir()
	h.SetWorkDir(workDir)
	h.projects = newTestProjectStore(t, workDir, otherProj)

	workDirID := session.NewSessionID()
	otherProjID := session.NewSessionID()
	saveSessionToDir(t, workDir, workDirID)
	saveSessionToDir(t, otherProj, otherProjID)

	// Session in the server's own workdir.
	get := httptest.NewRequest("GET", "/api/sessions/"+workDirID, nil)
	rec := httptest.NewRecorder()
	h.HandleGetSession(rec, get, workDirID)
	if rec.Code != http.StatusOK {
		t.Fatalf("workdir session GET: status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// Session in the OTHER project — the cross-project case.
	get = httptest.NewRequest("GET", "/api/sessions/"+otherProjID, nil)
	rec = httptest.NewRecorder()
	h.HandleGetSession(rec, get, otherProjID)
	if rec.Code != http.StatusOK {
		t.Fatalf("cross-project session GET: status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var detail SessionDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode session detail: %v", err)
	}
	if len(detail.Messages) != 1 {
		t.Fatalf("cross-project session messages = %d, want 1", len(detail.Messages))
	}

	// Context endpoint resolves the same way.
	rec = httptest.NewRecorder()
	h.HandleSessionContext(rec, httptest.NewRequest("GET", "/api/sessions/"+otherProjID+"/context", nil), otherProjID)
	if rec.Code != http.StatusOK {
		t.Fatalf("cross-project context: status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var ctx struct {
		MessageCount int `json:"message_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ctx); err != nil {
		t.Fatalf("decode context: %v", err)
	}
	if ctx.MessageCount != 1 {
		t.Fatalf("cross-project context message_count = %d, want 1", ctx.MessageCount)
	}

	// A session that exists in no registered project 404s.
	rec = httptest.NewRecorder()
	h.HandleGetSession(rec, httptest.NewRequest("GET", "/api/sessions/ses_missing", nil), "ses_missing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing session GET: status %d, want 404", rec.Code)
	}
}

// TestChatRegistersProjectBinding verifies POST /api/chat with a project_path
// binds the new session to that project root (the registry entry carries it)
// AND that the async path is persist-then-202: the 202 returns with the
// message already durable on disk under the owning project, before the agent
// bootstrap has even succeeded (a model that can never build a client is
// deliberately used, so only the persist could have produced the 202).
func TestChatRegistersProjectBinding(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)

	rec := chatRequest(t, h, map[string]any{
		"content":      "hi",
		"project_path": proj,
		"async":        true,
		// A model whose client can never build (no local server): the 202 must
		// still arrive (message persisted) and the bootstrap failure must not
		// lose the message.
		"model": "local/definitely-not-running",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("chat status %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	var resp ChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode chat response: %v", err)
	}
	if resp.SessionID == "" {
		t.Fatal("chat response missing session id")
	}
	entry := h.sessions.Lookup(resp.SessionID)
	if entry == nil {
		t.Fatalf("no registry entry for %s", resp.SessionID)
	}
	if entry.ProjectRoot != proj {
		t.Fatalf("entry project root = %q, want %q", entry.ProjectRoot, proj)
	}
	// The message must already be on disk in the session's own project at 202
	// time — a later bootstrap failure can never lose it.
	s, err := session.LoadForDir(proj, resp.SessionID)
	if err != nil {
		t.Fatalf("load persisted session: %v", err)
	}
	if len(s.Messages) != 1 || s.Messages[0].Content != "hi" {
		t.Fatalf("persisted messages = %+v, want [user: hi]", s.Messages)
	}
}
