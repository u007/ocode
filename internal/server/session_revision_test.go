package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

func getSessionStateRevision(t *testing.T, h *Handler, id string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleSessionState(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/state", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("state status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	return body.Revision
}

func getSessionDetailRevision(t *testing.T, h *Handler, id string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleGetSession(rec, httptest.NewRequest("GET", "/api/sessions/"+id, nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	return body.Revision
}

// TestSessionRevisionExposedAndMoves is the server-side half of the
// cross-process sync contract: GET /state and GET /:id must both carry the
// stored-transcript revision, they must agree, and an out-of-process write
// must move the token so a polling client knows to refetch.
func TestSessionRevisionExposedAndMoves(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	if err := session.SaveForDir(proj, id, "rev", []agent.Message{{Role: "user", Content: "q0"}}, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h.sessions.Register(id, proj)

	stateRev := getSessionStateRevision(t, h, id)
	if stateRev == "" {
		t.Fatal("state revision is empty")
	}
	if detailRev := getSessionDetailRevision(t, h, id); detailRev != stateRev {
		t.Fatalf("detail revision %q != state revision %q", detailRev, stateRev)
	}

	// Another process appends to (or compacts) the stored transcript.
	if err := session.SaveForDir(proj, id, "rev", []agent.Message{
		{Role: "user", Content: "q0"},
		{Role: "assistant", Content: "a0"},
	}, nil); err != nil {
		t.Fatalf("external append: %v", err)
	}
	if got := getSessionStateRevision(t, h, id); got == stateRev {
		t.Fatalf("state revision did not move after an external write (%q)", stateRev)
	}
	if got := getSessionDetailRevision(t, h, id); got == stateRev {
		t.Fatalf("detail revision did not move after an external write (%q)", stateRev)
	}
}
