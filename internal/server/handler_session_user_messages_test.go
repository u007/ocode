package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// userMessagesSeed builds a transcript mixing the message kinds the
// user-message index must distinguish: real user turns, assistant replies,
// tool output, slash-command echoes, and a user message with only
// whitespace before the slash.
func userMessagesSeed() []agent.Message {
	return []agent.Message{
		{Role: "user", Content: "first prompt"},          // 0 counted
		{Role: "assistant", Content: "reply"},            // 1
		{Role: "tool", Content: "output", ToolID: "t1"},  // 2
		{Role: "user", Content: "/theme dark"},           // 3 slash echo, not counted
		{Role: "assistant", Content: "themed"},           // 4
		{Role: "user", Content: "second prompt"},         // 5 counted
		{Role: "user", Content: "  \n  /compact  "},      // 6 whitespace-prefixed slash, not counted
		{Role: "user", Content: "third prompt"},          // 7 counted
		{Role: "assistant", Content: "", ToolCalls: nil}, // 8
		{Role: "user", Content: "   "},                   // 9 whitespace-only prompt, counted
	}
}

// userMessagesHandler seeds one session under a temp project and returns the
// handler. The project must be registered with the handler because Resolve
// only finds sessions under a KNOWN project root.
func userMessagesHandler(t *testing.T, msgs []agent.Message) (*Handler, string) {
	t.Helper()
	workDir := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(workDir)
	h.projects = newTestProjectStore(t, workDir)

	id := session.NewSessionID()
	if err := session.SaveForDir(workDir, id, "user jump fixture", msgs, nil); err != nil {
		t.Fatalf("save session: %v", err)
	}
	h.sessions.Register(id, workDir)
	return h, id
}

func doListUserMessages(t *testing.T, h *Handler, id, limitQuery string) sessionUserMessagesResponse {
	t.Helper()
	url := "/api/sessions/" + id + "/user-messages"
	if limitQuery != "" {
		url += "?" + limitQuery
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", url, nil)
	h.HandleListSessionUserMessages(rec, req, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("user-messages status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var resp sessionUserMessagesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func TestListSessionUserMessagesReturnsOnlyUserTurns(t *testing.T) {
	h, id := userMessagesHandler(t, userMessagesSeed())
	resp := doListUserMessages(t, h, id, "")

	// 0, 5, 7, 9 — the assistant, tool, and slash-echo rows are excluded.
	want := []int{0, 5, 7, 9}
	if len(resp.Indices) != len(want) {
		t.Fatalf("indices = %v, want %v (total %d)", resp.Indices, want, resp.Total)
	}
	for i := range want {
		if resp.Indices[i] != want[i] {
			t.Fatalf("indices[%d] = %d, want %d (full %v)", i, resp.Indices[i], want[i], resp.Indices)
		}
	}
	if resp.Total != len(want) {
		t.Fatalf("total = %d, want %d", resp.Total, len(want))
	}
	if resp.Truncated {
		t.Fatal("a 10-message transcript should not report truncated")
	}
	if resp.Scanned != 10 {
		t.Fatalf("scanned = %d, want 10", resp.Scanned)
	}
}

func TestListSessionUserMessagesLimitKeepsTotalExact(t *testing.T) {
	h, id := userMessagesHandler(t, userMessagesSeed())
	resp := doListUserMessages(t, h, id, "limit=2")

	// Total must stay the real count even when the index slice is capped, so
	// the client readout can say "msg 1/4" while only 2 targets are listed.
	if resp.Total != 4 {
		t.Fatalf("total = %d, want 4 (kept counting past the cap)", resp.Total)
	}
	if len(resp.Indices) != 2 {
		t.Fatalf("indices = %v, want 2 entries", resp.Indices)
	}
	if !resp.Truncated {
		t.Fatal("a capped scan must report truncated")
	}
}

func TestListSessionUserMessagesLimitClampedToMax(t *testing.T) {
	h, id := userMessagesHandler(t, userMessagesSeed())
	// An absurd cap must not be honoured verbatim; the handler clamps it and
	// still returns every real message.
	resp := doListUserMessages(t, h, id, "limit=99999999")
	if resp.Total != 4 {
		t.Fatalf("total = %d, want 4", resp.Total)
	}
	if resp.Truncated {
		t.Fatal("4 messages cannot exceed a 50000 cap")
	}
}

func TestListSessionUserMessagesBadLimitFallsBackToDefault(t *testing.T) {
	h, id := userMessagesHandler(t, userMessagesSeed())
	// A malformed cap must not error or zero the list — it falls back to the
	// default, which still covers this transcript.
	resp := doListUserMessages(t, h, id, "limit=banana")
	if resp.Total != 4 {
		t.Fatalf("total = %d, want 4", resp.Total)
	}
}

func TestListSessionUserMessagesUnknownSession404(t *testing.T) {
	h, _ := userMessagesHandler(t, userMessagesSeed())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/sessions/ses_does_not_exist/user-messages", nil)
	h.HandleListSessionUserMessages(rec, req, "ses_does_not_exist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestIsCountableUserMessage(t *testing.T) {
	tests := []struct {
		name    string
		role    string
		content string
		want    bool
	}{
		{"plain user turn", "user", "do the thing", true},
		{"assistant", "assistant", "sure", false},
		{"tool output", "tool", "result", false},
		{"slash echo", "user", "/theme", false},
		{"slash echo after whitespace", "user", "   \n /compact", false},
		{"whitespace-only prompt", "user", "   ", true},
		{"empty content", "user", "", true},
		{"slash mid-string is fine", "user", "explain /theme to me", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCountableUserMessage(tc.role, tc.content); got != tc.want {
				t.Fatalf("isCountableUserMessage(%q, %q) = %v, want %v", tc.role, tc.content, got, tc.want)
			}
		})
	}
}
