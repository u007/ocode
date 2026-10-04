package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// TestCompactSessionShrinksStoredTranscript is the regression test for
// "inline compression does nothing" (ses_2026-10-02-205400-f1e06e02).
//
// The manual /compact endpoint computed the summary correctly and spliced it
// into as.messages, but persisted it with saveSession instead of
// replaceSession. An ordinary save can never delete stored rows: sqlitestore
// rejects a shorter snapshot with ErrTranscriptConflict unless replace=true.
// The endpoint discarded that error with `_ =`, so it answered 200 and the UI
// reported a compaction that had in fact saved nothing. The store kept the full
// pre-compaction transcript, so every later save re-diverged, every live
// snapshot was dropped ("stored rows are not a prefix of the snapshot") and the
// session lost turns until the server re-synced memory to disk and released the
// agent.
//
// The load-bearing assertion is on the STORED transcript, not as.messages.
// Every pre-existing compact test asserts on memory only, and seeds the store
// with a single row (saveSessionToDir), so shrinking is never true and the bug
// was invisible to the suite.
func TestCompactSessionShrinksStoredTranscript(t *testing.T) {
	h := NewHandler()
	if h.cfg != nil {
		h.cfg.Model = "gpt-4o-mini"
	}
	proj := t.TempDir()
	id := session.NewSessionID()
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })
	h.sessions.Register(id, proj)

	// The store and the resident agent must agree BEFORE compaction. Seeding
	// the store with only one message (as saveSessionToDir does) would make the
	// compacted list LONGER than the stored one, so the shrink guard never
	// engages and the bug stays hidden.
	seed := seedTranscript()
	if err := session.Save(id, "project session", seed, nil); err != nil {
		t.Fatalf("seed store for %s under %s: %v", id, proj, err)
	}

	as := &agentSession{
		agent:    agent.NewAgent(compactSummaryClient{}, nil, autoCompactConfig(), nil),
		model:    "fake-model",
		messages: seed,
	}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()

	rec := httptest.NewRecorder()
	h.HandleCompactSession(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/compact", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("compact status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// Precondition: compaction really did collapse the in-memory transcript.
	// Without this the disk assertion below could pass vacuously.
	if len(as.messages) >= len(seed) {
		t.Fatalf("compaction did not shrink memory: len=%d, want < %d", len(as.messages), len(seed))
	}

	// The assertion that matters: the shrink must be durable. On the old
	// saveSession path this stayed at len(seed) because the write conflicted.
	stored, err := session.Load(id)
	if err != nil {
		t.Fatalf("reload compacted session %s: %v", id, err)
	}
	if len(stored.Messages) >= len(seed) {
		t.Fatalf("compaction was not persisted: stored %d msgs, seeded %d — the "+
			"compacted transcript was reported as a success but never written",
			len(stored.Messages), len(seed))
	}

	var found bool
	for _, m := range stored.Messages {
		if strings.Contains(m.Content, "## Original Request") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("compacted summary is absent from the stored transcript")
	}
}

// TestCompactSessionThenAppendPersists pins the user-visible consequence: after
// a compaction, ordinary turn-end appends must keep landing on disk. While the
// store held the uncompacted transcript, the next save saw stored rows that were
// not a prefix of the resident agent's list and was dropped every time.
func TestCompactSessionThenAppendPersists(t *testing.T) {
	h := NewHandler()
	if h.cfg != nil {
		h.cfg.Model = "gpt-4o-mini"
	}
	proj := t.TempDir()
	id := session.NewSessionID()
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })
	h.sessions.Register(id, proj)

	seed := seedTranscript()
	if err := session.Save(id, "project session", seed, nil); err != nil {
		t.Fatalf("seed store for %s under %s: %v", id, proj, err)
	}

	as := &agentSession{
		agent:    agent.NewAgent(compactSummaryClient{}, nil, autoCompactConfig(), nil),
		model:    "fake-model",
		messages: seed,
	}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()

	rec := httptest.NewRecorder()
	h.HandleCompactSession(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/compact", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("compact status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// One more turn, persisted the ordinary way.
	as.mu.Lock()
	as.messages = append(as.messages,
		agent.Message{Role: "user", Content: "after compaction"},
		agent.Message{Role: "assistant", Content: "after compaction reply"},
	)
	wantLen := len(as.messages)
	if err := h.saveSession(id, "", as.messages, nil); err != nil {
		as.mu.Unlock()
		t.Fatalf("post-compaction turn-end save: %v", err)
	}
	as.mu.Unlock()

	stored, err := session.Load(id)
	if err != nil {
		t.Fatalf("reload session %s: %v", id, err)
	}
	if len(stored.Messages) != wantLen {
		t.Fatalf("post-compaction append did not reach disk: stored=%d, in-memory=%d "+
			"(the turn was silently discarded)", len(stored.Messages), wantLen)
	}
}
