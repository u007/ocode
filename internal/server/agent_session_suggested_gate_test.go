package server

import (
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// TestReconcileProfileAgentDoesNotRebuildForAnUnrecordedSuggestionsFlag is the
// regression for a 500 on every chat turn.
//
// The suggested-responses rebuild gate compared the BUILT agent's live flag
// against the server's current config:
//
//	suggestedChanged := as.agent.SuggestedResponsesEnabled() != suggestedNow
//
// An agent is not always built from the server's config. A session assembled
// by hand — a test, or a helper that injects its own agent — has the ZERO
// value of the flag (`false`), while the server's default is `true`. Such an
// agent therefore reported "changed" on EVERY turn, and the rebuild it
// triggered failed: `buildAgentSession` cannot create a real LLM client for a
// synthetic model id, so the turn answered 500.
//
// The gate now compares a SNAPSHOT of the value the agent was built with
// (as.suggestedResponses), and an UNSET snapshot means "not recorded" — the
// session was not built by buildAgentSession, so the flag is unknown and
// there is nothing to compare.
func TestReconcileProfileAgentDoesNotRebuildForAnUnrecordedSuggestionsFlag(t *testing.T) {
	h := NewHandler()
	// The server's config has the default (true).
	if h.cfg == nil || !h.cfg.Ocode.SuggestedResponsesEnabled {
		t.Fatalf("precondition: the server config must default suggested responses to ON")
	}

	// A hand-built session: the agent's flag is the zero value (false) and the
	// snapshot was never recorded.
	as := &agentSession{
		agent:    agent.NewAgent(instantClient{}, nil, &config.Config{}, nil),
		model:    "fake-model",
		messages: []agent.Message{{Role: "user", Content: "hi"}},
	}
	if as.agent.SuggestedResponsesEnabled() {
		t.Fatalf("precondition: the hand-built agent must carry the zero value")
	}
	if as.suggestedResponses != nil {
		t.Fatalf("precondition: the hand-built session must not record a snapshot")
	}

	// reconcileProfileAgent must NOT try to rebuild: the flag is unknown, and a
	// rebuild of a synthetic model id fails.
	id := "sess-unrecorded-suggestions"
	h.sessions.Register(id, t.TempDir())

	got, err := h.reconcileProfileAgent(id, as, as.model)
	if err != nil {
		t.Fatalf("reconcileProfileAgent returned an error: %v", err)
	}
	if got != as {
		t.Fatalf("the session was rebuilt for an unrecorded suggestions flag")
	}
}

// TestReconcileProfileAgentRebuildsWhenTheSuggestionsFlagChanged pins the
// FEATURE the gate exists for: a toggle that happened while a turn was active
// (so the direct rebuild was deferred) must still land on the next turn.
func TestReconcileProfileAgentRebuildsWhenTheSuggestionsFlagChanged(t *testing.T) {
	h := NewHandler()
	// The server's config carries the default (the feature ON); flip it to OFF
	// so the recorded snapshot (ON) differs from the config.
	if h.cfg == nil {
		t.Fatalf("precondition: the handler must have a config")
	}
	h.cfg.Ocode.SuggestedResponsesEnabled = false

	// The session was built while the feature was ON.
	built := true
	as := &agentSession{
		agent:              agent.NewAgent(instantClient{}, nil, &config.Config{}, nil),
		model:              "fake-model",
		messages:           []agent.Message{{Role: "user", Content: "hi"}},
		suggestedResponses: &built,
	}
	// The config has it OFF → the snapshot differs → a rebuild is due.

	id := "sess-suggestions-changed"
	h.sessions.Register(id, t.TempDir())

	// The rebuild itself fails for a synthetic model id, but the ATTEMPT is
	// what this test pins: reconcileProfileAgent must try.
	_, err := h.reconcileProfileAgent(id, as, as.model)
	if err == nil {
		t.Fatalf("expected the rebuild of a synthetic model id to fail")
	}
}
