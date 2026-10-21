package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

func postToolDigest(h *Handler, sessionID, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/tool-call-digest", strings.NewReader(body))
	h.HandleToolCallDigest(w, r, sessionID)
	return w
}

// TestHandleToolCallDigestReturnsTheExplanation is the happy path: the client's
// parameters reach the model and its prose comes back with the model id.
func TestHandleToolCallDigestReturnsTheExplanation(t *testing.T) {
	h := testConfigHandler(t)
	stub := &speechSummaryStubClient{reply: "Lists the working directory."}
	plantSpeechSummarySession(t, h, "ses_digest", stub)

	w := postToolDigest(h, "ses_digest",
		`{"tool":"bash","command":"ls -la","args":{"command":"ls -la"},"rule":"bash.prefix.ls","scope":"bash_prefix"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["summary"] != "Lists the working directory." {
		t.Errorf("summary = %q", got["summary"])
	}
	if got["model"] != "fake/fake-model" {
		t.Errorf("model = %q, want the provider-qualified id", got["model"])
	}
	if !strings.Contains(stub.user, "ls -la") {
		t.Errorf("command missing from the model payload: %q", stub.user)
	}
}

// TestHandleToolCallDigestPrefersThePendingAsk proves the server does not trust
// client-posted parameters when the ask is live: request_id wins.
func TestHandleToolCallDigestPrefersThePendingAsk(t *testing.T) {
	h := testConfigHandler(t)
	stub := &speechSummaryStubClient{reply: "Deletes the build directory."}
	plantSpeechSummarySession(t, h, "ses_ask", stub)

	askReq := agent.PermissionRequest{
		ToolName: "bash",
		Command:  "rm -rf build",
		Rule:     "bash.prefix.rm",
		Scope:    agent.PermissionScopeBashPrefix,
	}
	payload, err := json.Marshal(askReq)
	if err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	as := h.agents[agentKey("ses_ask")]
	h.mu.Unlock()
	as.messages = []agent.Message{
		{Role: "assistant", Content: "Cleaning up."},
		{Role: "tool", ToolID: "call_1", Content: tool.SentinelPermissionAsk + string(payload)},
	}

	// The posted tool intentionally disagrees with the ask.
	w := postToolDigest(h, "ses_ask", `{"request_id":"call_1","tool":"read","command":"cat README.md"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(stub.user, "rm -rf build") {
		t.Errorf("the pending ask's command must be explained, payload=%q", stub.user)
	}
	if strings.Contains(stub.user, "cat README.md") {
		t.Errorf("client-posted parameters overrode the authoritative ask: %q", stub.user)
	}
}

// TestHandleToolCallDigestAcceptsAStaleRequestID covers the click-after-resolve
// race: the ask is gone, so the posted parameters are used.
func TestHandleToolCallDigestAcceptsAStaleRequestID(t *testing.T) {
	h := testConfigHandler(t)
	stub := &speechSummaryStubClient{reply: "Reads the file."}
	plantSpeechSummarySession(t, h, "ses_stale", stub)

	w := postToolDigest(h, "ses_stale", `{"request_id":"gone","tool":"read","args":{"path":"README.md"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(stub.user, "README.md") {
		t.Errorf("posted args missing from the payload: %q", stub.user)
	}
}

func TestHandleToolCallDigestRejectsMissingParameters(t *testing.T) {
	h := testConfigHandler(t)
	plantSpeechSummarySession(t, h, "ses_missing", &speechSummaryStubClient{reply: "x"})

	w := postToolDigest(h, "ses_missing", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "tool or a pending request_id") {
		t.Errorf("body should name the missing field: %s", w.Body.String())
	}
}

func TestHandleToolCallDigestUnknownSession(t *testing.T) {
	h := testConfigHandler(t)
	w := postToolDigest(h, "ses_nope", `{"tool":"bash"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", w.Code, w.Body.String())
	}
}

// TestHandleToolCallDigestReportsModelFailure pins fail-fast: a provider error
// is an HTTP error with the reason, not a 200 carrying an `error` field.
func TestHandleToolCallDigestReportsModelFailure(t *testing.T) {
	h := testConfigHandler(t)
	plantSpeechSummarySession(t, h, "ses_fail", &speechSummaryStubClient{err: errors.New("upstream exploded")})

	w := postToolDigest(h, "ses_fail", `{"tool":"bash","command":"ls"}`)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "upstream exploded") {
		t.Errorf("error body should carry the provider reason: %s", w.Body.String())
	}
}

// TestHandleToolCallDigestRefusesABusySession proves the handler never blocks
// on a running turn's lock.
func TestHandleToolCallDigestRefusesABusySession(t *testing.T) {
	h := testConfigHandler(t)
	plantSpeechSummarySession(t, h, "ses_busy", &speechSummaryStubClient{reply: "x"})

	h.mu.Lock()
	as := h.agents[agentKey("ses_busy")]
	h.mu.Unlock()
	as.mu.Lock()
	defer as.mu.Unlock()

	w := postToolDigest(h, "ses_busy", `{"tool":"bash","command":"ls"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", w.Code, w.Body.String())
	}
}

// TestHandleToolCallDigestDoesNotTouchTheTranscript is the isolation contract:
// an explanation is a side call and must leave the conversation untouched.
func TestHandleToolCallDigestDoesNotTouchTheTranscript(t *testing.T) {
	h := testConfigHandler(t)
	plantSpeechSummarySession(t, h, "ses_iso", &speechSummaryStubClient{reply: "Lists files."})

	h.mu.Lock()
	as := h.agents[agentKey("ses_iso")]
	h.mu.Unlock()
	as.messages = []agent.Message{{Role: "user", Content: "hello"}}

	if w := postToolDigest(h, "ses_iso", `{"tool":"bash","command":"ls"}`); w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if len(as.messages) != 1 || as.messages[0].Content != "hello" {
		t.Fatalf("transcript mutated: %+v", as.messages)
	}
}
