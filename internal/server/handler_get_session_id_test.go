package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// GET /api/sessions/claude:<id> reads the on-disk claude-<id> file, but the
// response must echo the id the client asked for: the web client keys its tab,
// prefetch and revision baseline on the requested id.
func TestGetSessionEchoesRequestedClaudeID(t *testing.T) {
	h := NewHandler()
	workDir := t.TempDir()
	h.SetWorkDir(workDir)
	h.projects = newTestProjectStore(t, workDir)
	saveSessionToDir(t, workDir, "claude-abc123")

	const requested = "claude:abc123"
	rec := httptest.NewRecorder()
	h.HandleGetSession(rec, httptest.NewRequest("GET", "/api/sessions/"+requested, nil), requested)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d, want 200 (body %s)", requested, rec.Code, rec.Body.String())
	}
	var detail SessionDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode session detail: %v", err)
	}
	if detail.ID != requested {
		t.Fatalf("response id = %q, want the requested %q", detail.ID, requested)
	}
	if len(detail.Messages) != 1 {
		t.Fatalf("messages = %d, want 1 (canonical file must still load)", len(detail.Messages))
	}
}
