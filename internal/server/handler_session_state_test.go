package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/u007/ocode/internal/session"
)

// TestHandleSessionStateEndpoint covers Part 03 Task 5's reconcile endpoint:
// an unknown session 404s; a known session's state reflects the registry entry
// (bootstrap stage, turn-active, last_seq) through a full cycle.
func TestHandleSessionStateEndpoint(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)

	rec := httptest.NewRecorder()
	h.HandleSessionState(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/state", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("state status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var st SessionState
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if st.SessionID != id || st.BootstrapStage != "" || st.TurnActive {
		t.Fatalf("fresh state = %+v, want idle", st)
	}

	// Advance the entry through a bootstrap + turn cycle and re-read.
	h.sessions.SetBootstrapStage(id, "tools")
	h.sessions.SetBootstrapStage(id, "ready")
	h.sessions.setTurnActive(id, true)
	h.sessions.SetLastSeq(id, 42)

	rec = httptest.NewRecorder()
	h.HandleSessionState(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/state", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("state status %d, want 200", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if st.BootstrapStage != "ready" || !st.TurnActive || st.LastSeq != 42 {
		t.Fatalf("advanced state = %+v, want ready/turn-active/last_seq 42", st)
	}

	rec = httptest.NewRecorder()
	h.HandleSessionState(rec, httptest.NewRequest("GET", "/api/sessions/ses_missing/state", nil), "ses_missing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing session state status %d, want 404", rec.Code)
	}
}

// TestHandleSessionStatusCrossProject covers Part 03 Task 5's status endpoint:
// for a session in a non-workdir project the snapshot is session-tagged with
// the session's own cwd and context_* fields.
func TestHandleSessionStatusCrossProject(t *testing.T) {
	h := NewHandler()
	workDir := t.TempDir()
	otherProj := t.TempDir()
	h.SetWorkDir(workDir)
	h.projects = newTestProjectStore(t, workDir, otherProj)
	if h.cfg != nil {
		h.cfg.Model = "gpt-4o-mini"
	}

	id := session.NewSessionID()
	saveSessionToDir(t, otherProj, id)

	rec := httptest.NewRecorder()
	h.HandleSessionStatus(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/status", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var snap TUIStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if snap.SessionID != id {
		t.Fatalf("session_id = %q, want %q", snap.SessionID, id)
	}
	if snap.CWD != otherProj {
		t.Fatalf("cwd = %q, want session's project %q", snap.CWD, otherProj)
	}
	// The seeded session has one user message ("hello") → estimated tokens > 0.
	if snap.ContextCurrentTokens <= 0 {
		t.Fatalf("context_current_tokens = %d, want > 0", snap.ContextCurrentTokens)
	}
	if snap.ContextModel == "" || snap.ContextMaxTokens <= 0 {
		t.Fatalf("context = %s/%d, want model + window", snap.ContextModel, snap.ContextMaxTokens)
	}

	// A truly missing session 404s.
	rec = httptest.NewRecorder()
	h.HandleSessionStatus(rec, httptest.NewRequest("GET", "/api/sessions/ses_missing/status", nil), "ses_missing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing session status %d, want 404", rec.Code)
	}
}

// TestSessionStateAndStatusForBridgedSession covers Part 06 Task 1: after
// registration (as RegisterExternalSession performs), the state and status
// endpoints resolve the bridged TUI session like any other session.
func TestSessionStateAndStatusForBridgedSession(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.sessions.Register("sess-rc", proj)
	h.rc = &RCBridge{SessionID: "sess-rc", Model: "rc-model"}

	rec := httptest.NewRecorder()
	h.HandleSessionState(rec, httptest.NewRequest("GET", "/api/sessions/sess-rc/state", nil), "sess-rc")
	if rec.Code != http.StatusOK {
		t.Fatalf("bridged state status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.HandleSessionStatus(rec, httptest.NewRequest("GET", "/api/sessions/sess-rc/status", nil), "sess-rc")
	if rec.Code != http.StatusOK {
		t.Fatalf("bridged status status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var snap TUIStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if snap.SessionID != "sess-rc" || snap.CWD != proj {
		t.Fatalf("bridged snapshot = %+v, want session sess-rc cwd %s", snap, proj)
	}
}

// TestPublishTurnStatusSnapshotBroadcastsContext covers the post-turn sidebar
// refresh: after a headless turn completes, publishTurnStatusSnapshot
// broadcasts a session-tagged "status" event whose context fields were
// computed from the just-persisted transcript — so the web Context gauge
// moves without a tab switch. Regression guard for the bug where every
// session-tagged status broadcast omitted (and thus zeroed) the context
// fields.
func TestPublishTurnStatusSnapshotBroadcastsContext(t *testing.T) {
	h := NewHandler()
	if h.cfg != nil {
		h.cfg.Model = "gpt-4o-mini"
	}
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)

	sub := h.subscribeHeadless()
	defer h.unsubscribeHeadless(sub)

	h.publishTurnStatusSnapshot(id)

	select {
	case ev := <-sub:
		if ev.Event != "status" {
			t.Fatalf("event = %q, want status", ev.Event)
		}
		snap, ok := ev.Data.(TUIStatus)
		if !ok {
			t.Fatalf("status data type = %T, want TUIStatus", ev.Data)
		}
		if snap.SessionID != id {
			t.Errorf("SessionID = %q, want %q", snap.SessionID, id)
		}
		if snap.CWD != proj {
			t.Errorf("CWD = %q, want owning project %q", snap.CWD, proj)
		}
		if snap.ContextCurrentTokens <= 0 {
			t.Errorf("context_current_tokens = %d, want > 0 from the seeded transcript", snap.ContextCurrentTokens)
		}
		if snap.ContextModel != "gpt-4o-mini" {
			t.Errorf("context_model = %q, want cfg model", snap.ContextModel)
		}
		if snap.ContextMaxTokens <= 0 {
			t.Errorf("context_max_tokens = %d, want model window", snap.ContextMaxTokens)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no status event broadcast")
	}
}
