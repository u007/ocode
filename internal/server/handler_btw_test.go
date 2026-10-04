package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/session"
)

// btwRequest hits HandleBtw for id with the given content and returns the recorder.
func btwRequest(h *Handler, id, content string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	body := `{"content":` + strconvQuote(content) + `}`
	req := httptest.NewRequest("POST", "/api/sessions/"+id+"/btw", strings.NewReader(body))
	h.HandleBtw(rec, req, id)
	return rec
}

// strconvQuote produces a JSON string literal (minimal, no deps).
func strconvQuote(s string) string {
	var buf bytes.Buffer
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			buf.WriteRune(r)
		}
	}
	buf.WriteByte('"')
	return buf.String()
}

// TestHandleBtwDoesNotConflictWithConcurrentWriter verifies that /btw uses
// the concurrent-safe append path (tail-insert with bounded retry) instead
// of load→append→save. A concurrent writer appending messages while /btw
// runs must NOT cause ErrTranscriptConflict — both messages land.
func TestHandleBtwDoesNotConflictWithConcurrentWriter(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	// Seed the session with an initial message so the sqlite file exists.
	if err := session.AppendUserMessageForDir(proj, id, "initial message"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Run /btw concurrently with another writer that uses the same
	// load→append→save pattern (simulating a second ocode process's sync
	// save). The old /btw load→append→save would conflict here because both
	// writers race: one appends between the other's load and save, and the
	// overlap check finds the stored transcript has diverged from the stale
	// snapshot. The fixed /btw uses tail-insert (AppendUserMessageForDir)
	// which retries and converges.
	var wg sync.WaitGroup
	wg.Add(2)

	var btwRec *httptest.ResponseRecorder

	go func() {
		defer wg.Done()
		btwRec = btwRequest(h, id, "by the way note")
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			// Use the concurrent-safe tail-insert path so this writer always
			// succeeds. The /btw handler's old load→append→save would then
			// see a stale snapshot (stored transcript longer than its in-memory
			// copy) and conflict. The fixed /btw uses the same tail-insert
			// path, so both converge.
			if err := session.AppendUserMessageForDir(proj, id, "concurrent message"); err != nil {
				return
			}
		}
	}()

	wg.Wait()

	// The /btw request must succeed — it uses the concurrent-safe tail-insert
	// path (AppendUserMessageForDir) which retries on conflict. The
	// concurrent writer's load→append→save MAY fail (that's the expected
	// behavior for a non-concurrent-safe path racing another writer); we
	// don't assert on it.
	if btwRec.Code != http.StatusOK {
		t.Fatalf("btw status %d, want 200 (body: %s)", btwRec.Code, btwRec.Body.String())
	}

	// Verify the /btw message landed in the transcript.
	s, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	foundBTW := false
	for _, m := range s.Messages {
		if strings.Contains(m.Content, "by the way note") {
			foundBTW = true
			break
		}
	}
	if !foundBTW {
		t.Error("btw message missing from transcript")
	}
}

// TestHandleBtwCreatesSessionWhenMissing verifies that /btw on a brand-new
// session id (no prior transcript) creates the session and appends the message.
func TestHandleBtwCreatesSessionWhenMissing(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	rec := btwRequest(h, id, "first ever note")
	if rec.Code != http.StatusOK {
		t.Fatalf("btw status %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	s, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(s.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(s.Messages))
	}
	if !strings.Contains(s.Messages[0].Content, "first ever note") {
		t.Errorf("message = %q, want to contain 'first ever note'", s.Messages[0].Content)
	}
}

// A live turn takes the aside via injection, never an on-disk append. Appending
// to the transcript underneath a running turn corrupts that turn's persistence:
// the stored transcript stops being a prefix of the in-memory snapshot, so every
// later live snapshot is dropped (session.liveAppendStart → samePrefix bails when
// the snapshot is shorter) and the turn-end sync save reports
// ErrTranscriptConflict. Both failures are only logged, so the rest of the turn
// silently vanishes on reload.
func TestHandleBtwInjectsIntoLiveTurnInsteadOfAppending(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate session storage from other tests
	h := NewHandler()
	proj := t.TempDir()
	id := "sess-btw-live"
	h.sessions.Register(id, proj)
	as := newTestSession(h, id, instantClient{})
	defer as.agent.Shutdown()

	// Seed a stored turn so the transcript file exists and a mid-turn disk
	// append would be observable.
	if err := session.AppendUserMessageForDir(proj, id, "turn in progress"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("load before: %v", err)
	}

	h.sessions.setTurnActive(id, true)
	defer h.sessions.setTurnActive(id, false)

	rec := btwRequest(h, id, "actually use tabs")
	if rec.Code != http.StatusOK {
		t.Fatalf("btw status %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	// The aside must be spliced into the running turn, not written to disk.
	pending := as.agent.DrainPendingInjections()
	if len(pending) != 1 {
		t.Fatalf("pending injections = %d, want 1 (aside must reach the live turn)", len(pending))
	}
	if want := "By the way: actually use tabs"; pending[0].Content != want {
		t.Errorf("injected content = %q, want %q", pending[0].Content, want)
	}
	if pending[0].Role != "user" {
		t.Errorf("injected role = %q, want %q", pending[0].Role, "user")
	}

	after, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("load after: %v", err)
	}
	if len(after.Messages) != len(before.Messages) {
		t.Fatalf("transcript grew from %d to %d messages during a live turn; a mid-turn "+
			"disk append breaks the turn's live snapshots and turn-end save",
			len(before.Messages), len(after.Messages))
	}
}

// The injection path is only for a LIVE turn. With no turn running the handler
// must still record the aside on disk, exactly as before.
func TestHandleBtwAppendsWhenNoTurnIsRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := NewHandler()
	proj := t.TempDir()
	id := "sess-btw-idle"
	h.sessions.Register(id, proj)
	as := newTestSession(h, id, instantClient{})
	defer as.agent.Shutdown()

	rec := btwRequest(h, id, "no turn in flight")
	if rec.Code != http.StatusOK {
		t.Fatalf("btw status %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if n := len(as.agent.DrainPendingInjections()); n != 0 {
		t.Errorf("pending injections = %d, want 0 (no turn active, so the aside goes to disk)", n)
	}

	s, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(s.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(s.Messages))
	}
	if want := "By the way: no turn in flight"; s.Messages[0].Content != want {
		t.Errorf("stored content = %q, want %q", s.Messages[0].Content, want)
	}
}

// An unknown session is still a 404: tryEnqueueInjection returns false for a
// session with no live agent, so the handler must fall through to Resolve.
func TestHandleBtwUnknownSessionStillNotFound(t *testing.T) {
	h := NewHandler()
	rec := httptest.NewRecorder()
	body := `{"content":"orphan aside"}`
	req := httptest.NewRequest("POST", "/api/sessions/nope/btw", strings.NewReader(body))
	h.HandleBtw(rec, req, "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404 (body: %s)", rec.Code, rec.Body.String())
	}
}

// An injected /btw aside must actually REACH a connected browser, not just the
// model. Handler.OnMessage broadcasts a user_message frame for any user-role
// message (handler.go), which is how the aside shows up in the transcript while
// the turn is still running; the turn's own opening message is broadcast by
// runTurn instead, so nothing else would cover this path. Without the frame the
// aside would be invisible in the web/desktop UI until the next reload.
func TestHandleBtwInjectionEmitsUserMessageFrame(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := NewHandler()
	proj := t.TempDir()
	id := "sess-btw-frame"
	h.sessions.Register(id, proj)
	as := newTestSession(h, id, instantClient{})
	defer as.agent.Shutdown()

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	// Install the PRODUCTION callbacks. The user_message broadcast lives in the
	// OnMessage closure wireHeadlessAgentCallbacks builds, so without this the
	// test would assert against a hand-rolled stand-in and prove nothing.
	h.wireHeadlessAgentCallbacks(id, as.agent)

	h.sessions.setTurnActive(id, true)
	defer h.sessions.setTurnActive(id, false)

	if rec := btwRequest(h, id, "call it out mid-turn"); rec.Code != http.StatusOK {
		t.Fatalf("btw status %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	// Simulate the agent picking the injection up at its next tool-call
	// boundary — Step does this via OnMessage, which is what fans out.
	pending := as.agent.DrainPendingInjections()
	if len(pending) != 1 {
		t.Fatalf("pending injections = %d, want 1", len(pending))
	}
	as.agent.OnMessage(pending[0])

	deadline := time.After(2 * time.Second)
	for {
		select {
		case env := <-sub:
			if env.Event != "user_message" {
				continue
			}
			if env.SessionID != id {
				t.Errorf("frame session id = %q, want %q", env.SessionID, id)
			}
			data, ok := env.Data.(map[string]string)
			if !ok {
				t.Fatalf("user_message payload type = %T, want map[string]string", env.Data)
			}
			if want := "By the way: call it out mid-turn"; data["content"] != want {
				t.Errorf("frame content = %q, want %q", data["content"], want)
			}
			return
		case <-deadline:
			t.Fatal("no user_message frame emitted for the injected aside; " +
				"it would be invisible in a connected browser until reload")
		}
	}
}
