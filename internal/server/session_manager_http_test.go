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
// binds the new session to that project root (the registry entry carries it).
func TestChatRegistersProjectBinding(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)

	rec := chatRequest(t, h, map[string]any{"content": "hi", "project_path": proj})
	if rec.Code != http.StatusOK && rec.Code != http.StatusAccepted {
		t.Fatalf("chat status %d, want 200/202 (body %s)", rec.Code, rec.Body.String())
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
}
