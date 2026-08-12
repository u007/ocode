package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// sendMessageRequest posts a message to /api/sessions/:id/message.
func sendMessageRequest(t *testing.T, h *Handler, id string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	rec := httptest.NewRecorder()
	h.HandleSendMessage(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/message", bytes.NewReader(raw)), id)
	return rec
}

// TestSendMessageRoutesBridgedSessionToTUI covers Part 06 Task 3: a message to
// the bridged TUI session id is forwarded through the bridge — and only that
// session id is. A different (headless) session in the same server reaches its
// own agent; nothing is globally forwarded to the TUI.
func TestSendMessageRoutesBridgedSessionToTUI(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.sessions.Register("sess-rc", proj)
	rcCh := make(chan RCRequest, 4)
	h.rc = &RCBridge{RcCh: rcCh, SessionID: "sess-rc", Model: "rc-model"}

	// Bridged session (sync): the request must reach the TUI channel, and the
	// response must carry the TUI-produced reply.
	go func() {
		req := <-rcCh
		req.ResultCh <- RCResult{Messages: []agent.Message{{Role: "assistant", Content: "from-tui"}}}
	}()
	rec := sendMessageRequest(t, h, "sess-rc", map[string]any{"content": "hey"})
	if rec.Code != http.StatusOK {
		t.Fatalf("bridged send status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var resp ChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Content != "from-tui" || resp.SessionID != "sess-rc" {
		t.Fatalf("bridged response = %+v, want from-tui for sess-rc", resp)
	}

	// Async bridged: 202 immediately, request forwarded.
	rec = sendMessageRequest(t, h, "sess-rc", map[string]any{"content": "fast", "async": true})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("async bridged status %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	select {
	case <-rcCh:
	case <-time.After(2 * time.Second):
		t.Fatal("async bridged request not forwarded to the TUI")
	}

	// A headless session in the same server must NOT be forwarded: its turn
	// runs on the server's own agent.
	webID := session.NewSessionID()
	h.sessions.Register(webID, t.TempDir())
	newTestSession(h, webID, instantClient{})
	rec = sendMessageRequest(t, h, webID, map[string]any{"content": "server-side"})
	if rec.Code != http.StatusOK {
		t.Fatalf("headless send status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Content != "hi" { // instantClient's canned reply
		t.Fatalf("headless response content = %q, want server agent reply 'hi'", resp.Content)
	}
	// The TUI channel must not have received the headless message.
	select {
	case req := <-rcCh:
		t.Fatalf("headless message leaked to the TUI bridge: %+v", req)
	default:
	}

	// A session that exists in no registered project still 404s.
	rec = sendMessageRequest(t, h, "ses_missing", map[string]any{"content": "x"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing session send status %d, want 404", rec.Code)
	}
}

// TestBridgeBroadcastPublishesToBusTaggedAtSource covers Part 06 Task 2: a
// frame broadcast by a bridged session arrives on the unified event bus
// carrying the real session id and owning project, even when the TUI produced
// it without a session tag (the bridge stamps at source).
func TestBridgeBroadcastPublishesToBusTaggedAtSource(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.sessions.Register("sess-rc", proj)

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	// Wire the same publish hook RegisterExternalSession installs.
	b := &RCBridge{SessionID: "sess-rc", publish: func(ev SSEEvent) {
		p := ""
		if e := h.sessions.Lookup(ev.SessionID); e != nil {
			p = e.ProjectRoot
		}
		h.bus.Publish(ev.Event, p, ev.SessionID, ev.Data)
		h.sessions.SetLastSeq(ev.SessionID, h.bus.LastSeq())
	}}

	// Untagged at the TUI layer — the bridge must stamp the real session id.
	b.Broadcast(SSEEvent{Event: "text", Data: TextDelta{Delta: "hi"}})

	select {
	case env := <-sub:
		if env.Event != "text" {
			t.Fatalf("bus event = %q, want text", env.Event)
		}
		if env.SessionID != "sess-rc" {
			t.Fatalf("bus session = %q, want sess-rc", env.SessionID)
		}
		if env.Project != proj {
			t.Fatalf("bus project = %q, want %q", env.Project, proj)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bridge frame never reached the event bus")
	}
	if st, _ := h.sessions.State("sess-rc"); st.LastSeq == 0 {
		t.Fatal("reconcile last_seq not updated by bridge publish")
	}
}

// TestRegisterExternalSessionRegistersTUIProject covers Part 06 Task 1 at the
// Server layer: RegisterExternalSession binds the TUI session into the
// SessionManager with the TUI's project root (the handler workdir), the
// per-session state endpoint resolves it, and the bridge's publish hook puts
// tagged frames on the event bus.
func TestRegisterExternalSessionRegistersTUIProject(t *testing.T) {
	srv := New("127.0.0.1:0", "", "", nil)
	workDir := t.TempDir()
	srv.SetWorkDir(workDir)
	rcCh := make(chan RCRequest, 1)
	resolveCh := make(chan RCResolution, 1)

	bridge := srv.RegisterExternalSession("sess-tui", "model-x", rcCh, resolveCh, "tok")
	if bridge == nil {
		t.Fatal("RegisterExternalSession returned nil bridge")
	}
	if srv.handler.rc != bridge {
		t.Fatal("bridge not installed on handler")
	}
	entry := srv.handler.sessions.Lookup("sess-tui")
	if entry == nil {
		t.Fatal("TUI session not registered in the SessionManager")
	}
	if entry.ProjectRoot != workDir {
		t.Fatalf("TUI session project root = %q, want workdir %q", entry.ProjectRoot, workDir)
	}

	// Per-session state resolves the bridged session.
	rec := httptest.NewRecorder()
	srv.handler.HandleSessionState(rec, httptest.NewRequest("GET", "/api/sessions/sess-tui/state", nil), "sess-tui")
	if rec.Code != http.StatusOK {
		t.Fatalf("bridged state status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// Bridge broadcasts reach the bus tagged at source with session + project.
	sub := srv.handler.bus.Subscribe(nil)
	defer srv.handler.bus.Unsubscribe(sub)
	bridge.Broadcast(SSEEvent{Event: "text", Data: TextDelta{Delta: "x"}})
	select {
	case env := <-sub:
		if env.SessionID != "sess-tui" || env.Project != workDir || env.Event != "text" {
			t.Fatalf("bus envelope = %+v, want text for sess-tui under %s", env, workDir)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bridge frame never reached the event bus")
	}
}
