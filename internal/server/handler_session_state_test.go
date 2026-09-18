package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
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
	// context_current_tokens is the provider-reported value, not a chars/4
	// estimate over the seeded transcript: with no live agent there is no
	// usage reading, so the field is omitted (0) rather than fabricated.
	if snap.ContextCurrentTokens != 0 {
		t.Fatalf("context_current_tokens = %d, want 0 (no live provider usage)", snap.ContextCurrentTokens)
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
// broadcasts a session-tagged "status" event whose context fields carry the
// backend's provider-reported usage — so the web Context gauge moves without a
// tab switch. Regression guard for the bug where every session-tagged status
// broadcast omitted (and thus zeroed) the context fields.
func TestPublishTurnStatusSnapshotBroadcastsContext(t *testing.T) {
	h := NewHandler()
	if h.cfg != nil {
		h.cfg.Model = "gpt-4o-mini"
	}
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)

	// A live headless agent that has completed one LLM call reporting 777 input
	// tokens — the backend-authoritative context occupancy the snapshot must
	// carry (never a chars/4 estimate of the seeded transcript).
	ag := agent.NewAgent(usageReportingClient{prompt: 777}, nil, nil, nil)
	if _, err := ag.Step([]agent.Message{{Role: "user", Content: "hello"}}); err != nil {
		t.Fatalf("seed step: %v", err)
	}
	h.mu.Lock()
	h.agents[id] = &agentSession{agent: ag}
	h.mu.Unlock()

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
		if snap.ContextCurrentTokens != 777 {
			t.Errorf("context_current_tokens = %d, want 777 (provider-reported)", snap.ContextCurrentTokens)
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

// usageReportingClient is a fake LLM client that reports a fixed prompt-token
// count, so a Step records it as the agent's LastInputTokens.
type usageReportingClient struct{ prompt int64 }

func (c usageReportingClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	pt := c.prompt
	return &agent.Message{
		Role:    "assistant",
		Content: "hi",
		Usage:   &agent.TokenUsage{PromptTokens: &pt},
	}, nil
}
func (usageReportingClient) GetProvider() string { return "fake" }
func (usageReportingClient) GetModel() string    { return "fake-model" }

// TestHandleSessionStateSurfacesLivePendingAsk covers the recovery path for a
// session paused on a permission ask whose sentinel is NOT in the persisted
// transcript: the live agent's trailing tool round is the authoritative
// source, so GET /api/sessions/:id/state must carry pending_asks for the
// browser to hydrate the dialog from (and resolve the ask instead of being
// permanently blocked by ErrPermissionPending).
func TestHandleSessionStateSurfacesLivePendingAsk(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)

	payload, err := json.Marshal(agent.PermissionRequest{
		ToolName: "bash",
		Command:  "rm -rf /tmp/x",
		Rule:     "bash.rm",
		Scope:    agent.PermissionScopeBashPrefix,
		Prefix:   "rm",
	})
	if err != nil {
		t.Fatalf("marshal permission request: %v", err)
	}
	h.agents[id] = &agentSession{
		messages: []agent.Message{
			{Role: "assistant", Content: "working"},
			{Role: "tool", ToolID: "call-1", Content: tool.SentinelPermissionAsk + string(payload)},
		},
	}

	rec := httptest.NewRecorder()
	h.HandleSessionState(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/state", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("state status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var resp sessionStateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode state response: %v", err)
	}
	if resp.PendingAsks == nil || len(resp.PendingAsks.Permissions) != 1 {
		t.Fatalf("pending_asks = %+v, want one permission ask", resp.PendingAsks)
	}
	ask := resp.PendingAsks.Permissions[0]
	if ask.RequestID != "call-1" {
		t.Errorf("request_id = %q, want call-1", ask.RequestID)
	}
	if ask.Tool != "bash" || ask.Command != "rm -rf /tmp/x" {
		t.Errorf("ask = %+v, want bash rm -rf /tmp/x", ask)
	}
	if ask.Scope != "bash_prefix" || ask.Prefix != "rm" {
		t.Errorf("scope/prefix = %q/%q, want bash_prefix/rm", ask.Scope, ask.Prefix)
	}

	// Resolving the ask (sentinel replaced in place) clears pending_asks.
	h.agents[id].messages[1].Content = "ran"
	rec = httptest.NewRecorder()
	h.HandleSessionState(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/state", nil), id)
	resp = sessionStateResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resolved state response: %v", err)
	}
	if resp.PendingAsks != nil {
		t.Fatalf("pending_asks = %+v, want nil after resolution", resp.PendingAsks)
	}
}
