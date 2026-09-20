package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// TestHandleResetSessionIDRekeysAndDeletesOld is the server-side regression for
// /reset-id: the endpoint returns the old/new pair, the registry resolves the
// new id with the transcript preserved, and the old id no longer resolves.
func TestHandleResetSessionIDRekeysAndDeletesOld(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)

	id := session.NewSessionID()
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })
	if err := session.Save(id, "Keep me", []agent.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
	}, map[string]any{"spend": 0.5}); err != nil {
		t.Fatal(err)
	}
	// Bind the registry so Resolve finds the owning project.
	if _, err := h.sessions.BindNewOrVerify(id, proj, ""); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.HandleResetSessionID(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/reset-id", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var raw map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if raw["old_id"] != id {
		t.Errorf("old_id = %q, want %q", raw["old_id"], id)
	}
	newID := raw["new_id"]
	if newID == "" || newID == id {
		t.Fatalf("new_id = %q, want a fresh id", newID)
	}

	// The registry must resolve the new id...
	entry, err := h.sessions.Resolve(newID)
	if err != nil {
		t.Fatalf("registry does not resolve new id: %v", err)
	}
	if entry.ProjectRoot != proj {
		t.Errorf("new entry project root = %q, want %q", entry.ProjectRoot, proj)
	}
	// ...and no longer resolve the old id.
	if _, err := h.sessions.Resolve(id); err == nil {
		t.Fatalf("registry still resolves old id %s", id)
	}

	// The transcript must have followed the id.
	moved, err := session.LoadForDir(proj, newID)
	if err != nil {
		t.Fatalf("load rekeyed transcript: %v", err)
	}
	if moved.Title != "Keep me" || len(moved.Messages) != 2 {
		t.Fatalf("transcript not preserved: title=%q msgs=%d", moved.Title, len(moved.Messages))
	}
	if moved.Metadata == nil || moved.Metadata["spend"] != 0.5 {
		t.Fatalf("metadata not preserved: %#v", moved.Metadata)
	}
	if _, err := session.LoadForDir(proj, id); err == nil {
		t.Fatal("old transcript still loads after reset")
	}
}

// TestHandleResetSessionIDRejectsActiveTurn pins the quiesce guard: a rekey
// must never race a running turn (the copy would be mid-step and a queued live
// write could resurrect the deleted old id).
func TestHandleResetSessionIDRejectsActiveTurn(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	if _, err := h.sessions.BindNewOrVerify(id, proj, ""); err != nil {
		t.Fatal(err)
	}
	h.sessions.setTurnActive(id, true)
	t.Cleanup(func() { h.sessions.setTurnActive(id, false) })

	rec := httptest.NewRecorder()
	h.HandleResetSessionID(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/reset-id", nil), id)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
}

// TestHandleResetSessionIDReleasesLiveAgent pins the quiesce ordering: a
// resident agent must be released (its h.agents mirror + turn lock dropped)
// before the disk rekey, so no queued live write can resurrect the old id and
// the new id starts with a clean (rebuilt-on-demand) agent.
func TestHandleResetSessionIDReleasesLiveAgent(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	if _, err := h.sessions.BindNewOrVerify(id, proj, ""); err != nil {
		t.Fatal(err)
	}
	ag := agent.NewAgent(instantClient{}, nil, nil, nil)
	h.registerAgentSession(id, &agentSession{agent: ag, model: "fake-model"}, proj)
	if h.lookupAgentSession(id) == nil {
		t.Fatal("precondition: live agent not registered")
	}

	rec := httptest.NewRecorder()
	h.HandleResetSessionID(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/reset-id", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var raw map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	newID := raw["new_id"]
	if h.lookupAgentSession(id) != nil {
		t.Fatal("old id still has a resident agent after reset")
	}
	// No agent is left under the new id either — it rebuilds on the next turn
	// from the rekeyed transcript.
	if h.lookupAgentSession(newID) != nil {
		t.Fatal("reset should not carry the agent under the new id")
	}
}

// TestHandleResetSessionIDUnknownSession pins the 404 path.
func TestHandleResetSessionIDUnknownSession(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	rec := httptest.NewRecorder()
	h.HandleResetSessionID(rec, httptest.NewRequest("POST", "/api/sessions/ses_missing/reset-id", nil), "ses_missing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}
