package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// cancelCompactionTestSession registers a live session with the given agent so
// the cancel handlers can resolve it.
func cancelCompactionTestSession(t *testing.T, ag *agent.Agent, msgs []agent.Message) (*Handler, string) {
	t.Helper()
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)
	as := &agentSession{agent: ag, model: "fake-model", messages: append([]agent.Message(nil), msgs...)}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()
	return h, id
}

// newBlockingCompactHandler wires an agent whose summarizer blocks until
// released, so a compaction pass is observably in flight.
func newBlockingCompactHandler(t *testing.T) (*Handler, string, *blockingCompactClient, []agent.Message) {
	t.Helper()
	client := &blockingCompactClient{started: make(chan struct{}), release: make(chan struct{})}
	before := seedTranscript()
	h, id := cancelCompactionTestSession(t, agent.NewAgent(client, nil, autoCompactConfig(), nil), before)
	t.Cleanup(func() {
		select {
		case <-client.release:
		default:
			close(client.release)
		}
	})
	return h, id, client, before
}

func startManualCompact(t *testing.T, h *Handler, id string) <-chan *httptest.ResponseRecorder {
	t.Helper()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.HandleCompactSession(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/compact", nil), id)
		done <- rec
	}()
	return done
}

func awaitCompactionDone(t *testing.T, sub chan SSEEvent, id string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-sub:
			if ev.SessionID != id || ev.Event != "compaction_done" {
				continue
			}
			raw, err := json.Marshal(ev.Data)
			if err != nil {
				t.Fatalf("marshal compaction_done: %v", err)
			}
			var payload struct {
				OK    bool   `json:"ok"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatalf("decode compaction_done: %v", err)
			}
			// A user cancel is not a failure: the terminal frame must be a
			// clean completion so no client paints an error banner.
			if !payload.OK || payload.Error != "" {
				t.Fatalf("compaction_done after cancel = %+v, want clean ok:true", payload)
			}
			return
		case <-deadline:
			t.Fatal("no compaction_done published after cancel")
		}
	}
}

// The Cancel button on the compaction bar drives this endpoint. It must
// interrupt the pass and tell the initiating client it was cancelled without
// turning the cancel into a failure.
func TestCancelCompactionEndpointCancelsManualPass(t *testing.T) {
	h, id, client, before := newBlockingCompactHandler(t)
	sub := h.subscribeHeadless()
	defer h.unsubscribeHeadless(sub)

	compactDone := startManualCompact(t, h, id)
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("manual compaction never reached the summarizer")
	}

	rec := httptest.NewRecorder()
	h.HandleCancelCompaction(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/compact/cancel", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"cancelled":true`) {
		t.Fatalf("cancel body = %s, want cancelled:true", rec.Body.String())
	}

	select {
	case compactRec := <-compactDone:
		if compactRec.Code != http.StatusOK {
			t.Fatalf("compact after cancel = %d, want 200 (body %s)", compactRec.Code, compactRec.Body.String())
		}
		if !strings.Contains(compactRec.Body.String(), `"cancelled":true`) {
			t.Fatalf("compact after cancel body = %s, want cancelled:true", compactRec.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("compact request did not return after cancel")
	}
	awaitCompactionDone(t, sub, id)

	as := h.lookupAgentSession(id)
	as.mu.Lock()
	got := append([]agent.Message(nil), as.messages...)
	as.mu.Unlock()
	if len(got) != len(before) {
		t.Fatalf("cancel changed transcript length: got %d, want %d", len(got), len(before))
	}
}

// The composer Stop (POST /api/sessions/{id}/cancel) must also stop an
// in-flight compaction, including when no turn is active (the manual-compact
// case) — and without recording pendingCancel, which would poison the next turn.
func TestCancelSessionAlsoCancelsCompaction(t *testing.T) {
	h, id, client, before := newBlockingCompactHandler(t)

	compactDone := startManualCompact(t, h, id)
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("manual compaction never reached the summarizer")
	}

	rec := httptest.NewRecorder()
	h.HandleCancelSession(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/cancel", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("session cancel status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	select {
	case compactRec := <-compactDone:
		if compactRec.Code != http.StatusOK || !strings.Contains(compactRec.Body.String(), `"cancelled":true`) {
			t.Fatalf("compact after stop = %d body=%s, want 200 cancelled:true", compactRec.Code, compactRec.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("compact request did not return after stop")
	}

	if h.isPendingCancel(id) {
		t.Fatal("stopping only a compaction recorded pendingCancel — the next turn would be cancelled")
	}
	as := h.lookupAgentSession(id)
	as.mu.Lock()
	got := len(as.messages)
	as.mu.Unlock()
	if got != len(before) {
		t.Fatalf("stop changed transcript length: got %d, want %d", got, len(before))
	}
}

// The auto-compaction path has no HTTP request, so a cancel there surfaces only
// through applyCompactResult. It must publish a clean terminal frame (this is
// what clears the compaction bar on every connected client), not an error.
func TestAutoCompactCancelPublishesCleanTerminalEvent(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)
	h.beginCompaction(id)

	sub := h.subscribeHeadless()
	defer h.unsubscribeHeadless(sub)

	// Mirrors the real auto-compaction goroutine calling OnCompact.
	go h.applyCompactResult(id, agent.CompactResult{OK: false, Err: agent.ErrCompactionCanceled})

	awaitCompactionDone(t, sub, id)
}

// Cancelling with nothing in flight is an idempotent no-op, not an error.
func TestCancelCompactionEndpointNoPassIsNoop(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)

	rec := httptest.NewRecorder()
	h.HandleCancelCompaction(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/compact/cancel", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"cancelled":false`) {
		t.Fatalf("body = %s, want cancelled:false", rec.Body.String())
	}
}

// The route must exist on the real mux: a handler-only test cannot catch a
// missing route, and the SPA fallback would otherwise answer.
func TestCompactCancelRouteIsRegistered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := New("127.0.0.1:0", "", "", nil)
	mux := srv.serveHandler()

	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	srv.handler.sessions.Register(id, proj)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/sessions/"+id+"/compact/cancel", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST compact/cancel = %d body=%s (404 here means the route is missing)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"cancelled":false`) {
		t.Fatalf("body = %s, want cancelled:false", rec.Body.String())
	}
}
