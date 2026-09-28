package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

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
