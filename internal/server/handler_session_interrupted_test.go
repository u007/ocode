package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// seedSessionForHandler plants an arbitrary transcript for an id under proj's
// storage dir, so a handler test can pin the stored tail the interrupted rule
// classifies.
func seedSessionForHandler(t *testing.T, proj, id string, msgs []agent.Message) {
	t.Helper()
	if err := session.SaveForDir(proj, id, "", msgs, nil); err != nil {
		t.Fatalf("save session %s under %s: %v", id, proj, err)
	}
}

// interruptedTail is the reported shape: an assistant dispatched a tool, the
// user answered the ask, and no reply ever followed.
func interruptedTail() []agent.Message {
	return []agent.Message{
		{Role: "user", Content: "deploy this"},
		{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
		{Role: "tool", ToolID: "call-1", Content: `{"answers":[{"label":"Production"}]}`},
	}
}

// dismissedQuestionTail is the cancel shape: the assistant asked a question,
// the user CANCELLED it (no answer), and no continuation round runs. This is a
// deliberate stop, so it must NOT read as an interrupted turn (the reported
// "cancel the question, it should not continue" bug).
func dismissedQuestionTail() []agent.Message {
	return []agent.Message{
		{Role: "user", Content: "deploy this"},
		{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
		{Role: "tool", ToolID: "call-1", Content: tool.QuestionDismissedResult},
	}
}

// stateResponseFor calls GET /state and decodes the response struct itself, so
// the JSON field name is pinned by the wire contract rather than a
// hand-written mirror.
func stateResponseFor(t *testing.T, h *Handler, id string) sessionStateResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleSessionState(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/state", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("state status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var resp sessionStateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	return resp
}

// stateInterrupted returns the decoded `interrupted` flag.
func stateInterrupted(t *testing.T, h *Handler, id string) bool {
	t.Helper()
	return stateResponseFor(t, h, id).Interrupted
}

// TestSessionStateInterruptedTail covers the settled-store cases: an
// answered-ask tail reads as interrupted, a landed reply does not, and an
// unanswered ask is the dialog's business (not an interruption).
func TestSessionStateInterruptedTail(t *testing.T) {
	tests := []struct {
		name string
		msgs []agent.Message
		want bool
	}{
		{"answered-ask tail", interruptedTail(), true},
		{"dismissed-question tail", dismissedQuestionTail(), false},
		{"completed turn", []agent.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "done"},
		}, false},
		{"unanswered question", []agent.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
			{Role: "tool", ToolID: "call-1", Content: tool.SentinelQuestionPrompt + `[{"header":"H","question":"Q"}]` + "\n\n" + tool.SentinelWaitingForUser},
		}, false},
		{"unanswered permission", []agent.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
			{Role: "tool", ToolID: "call-1", Content: tool.SentinelPermissionAsk + `{"toolName":"bash"}`},
		}, false},
		{"tool_calls-only assistant", []agent.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			h := NewHandler()
			proj := t.TempDir()
			h.projects = newTestProjectStore(t, proj)
			id := session.NewSessionID()
			seedSessionForHandler(t, proj, id, tt.msgs)

			if got := stateInterrupted(t, h, id); got != tt.want {
				t.Fatalf("interrupted = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSessionStateInterruptedFailOpen: an absent or unreadable store, and a
// session with no resident agent whose transcript never got a tail, must never
// be reported as interrupted.
func TestSessionStateInterruptedFailOpen(t *testing.T) {
	t.Run("absent session file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		h := NewHandler()
		proj := t.TempDir()
		h.projects = newTestProjectStore(t, proj)
		id := session.NewSessionID()
		h.sessions.Register(id, proj)

		if got := stateInterrupted(t, h, id); got {
			t.Fatal("absent store reported interrupted, want false (fail open)")
		}
	})

	t.Run("unreadable sqlite", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		h := NewHandler()
		proj := t.TempDir()
		h.projects = newTestProjectStore(t, proj)
		id := session.NewSessionID()
		h.sessions.Register(id, proj)
		dir, err := session.GetStorageDirForPath(proj)
		if err != nil {
			t.Fatalf("storage dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, id+".sqlite"), []byte("not a database"), 0o644); err != nil {
			t.Fatalf("write bogus sqlite: %v", err)
		}

		if got := stateInterrupted(t, h, id); got {
			t.Fatal("unreadable store reported interrupted, want false (fail open)")
		}
	})
}

// TestSessionStateInterruptedTurnActive pins the active-turn gate.
func TestSessionStateInterruptedTurnActive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	id := session.NewSessionID()
	seedSessionForHandler(t, proj, id, interruptedTail())
	h.sessions.Register(id, proj)
	h.sessions.setTurnActive(id, true)

	resp := stateResponseFor(t, h, id)
	if !resp.TurnActive {
		t.Fatal("test setup: turn_active not set")
	}
	if resp.Interrupted {
		t.Fatal("turn_active session reported interrupted, want false")
	}
}

// TestSessionStateInterruptedTurnJobInFlight covers false-positive window 1:
// executeTurnJob holds the per-session turn lock from before the user row is
// persisted through the whole bootstrap+turn, so a stored user-row tail must
// NOT read as interrupted while the job is in flight. The lock is held
// directly here — that is the same lock the real job holds for its duration.
func TestSessionStateInterruptedTurnJobInFlight(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	id := session.NewSessionID()
	// The shape the naive rule fires on: the job persisted the user row and is
	// bootstrapping, so the stored tail is a user row and no agent exists yet.
	seedSessionForHandler(t, proj, id, []agent.Message{{Role: "user", Content: "hi"}})

	lock := h.sessionTurnLock(id)
	lock.Lock()
	defer lock.Unlock()

	if got := stateInterrupted(t, h, id); got {
		t.Fatal("in-flight turn job reported interrupted, want false (window 1)")
	}
}

// TestSessionStateInterruptedAgentLockHeld covers false-positive window 2: the
// question/permission-answer continuation holds the agent lock while it
// persists the answer (and before the continuation's setTurnActive). A settled
// tail must not read as interrupted while that lock is held.
func TestSessionStateInterruptedAgentLockHeld(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	id := session.NewSessionID()
	seedSessionForHandler(t, proj, id, interruptedTail())

	as := &agentSession{messages: interruptedTail()}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()

	as.mu.Lock()
	defer as.mu.Unlock()

	if got := stateInterrupted(t, h, id); got {
		t.Fatal("agent lock held reported interrupted, want false (window 2)")
	}
}

// TestSessionStateInterruptedResidentAgentTail: with a resident agent the
// in-memory transcript is authoritative, so an unfinished memory tail reads as
// interrupted even when the stored copy is already a landed reply.
func TestSessionStateInterruptedResidentAgentTail(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	id := session.NewSessionID()
	seedSessionForHandler(t, proj, id, []agent.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "stored reply"},
	})

	as := &agentSession{messages: interruptedTail()}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()

	if got := stateInterrupted(t, h, id); !got {
		t.Fatal("unfinished resident-agent tail reported not interrupted, want true")
	}

	// A landed reply in memory clears it.
	as.mu.Lock()
	as.messages = []agent.Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "done"}}
	as.mu.Unlock()
	if got := stateInterrupted(t, h, id); got {
		t.Fatal("completed resident-agent tail reported interrupted, want false")
	}
}
