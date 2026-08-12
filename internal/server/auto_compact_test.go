package server

import (
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// autoCompactConfig returns a config whose compaction threshold is low enough
// that any non-trivial transcript is over it, so a single turn must trigger
// auto-compaction.
func autoCompactConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Ocode.Compact.Enabled = true
	cfg.Ocode.Compact.TokenThreshold = 0.0001
	cfg.Ocode.Compact.MinMessages = 2
	cfg.Ocode.Compact.KeepRecentTurns = 1
	cfg.Ocode.Compact.KeepRecentTokens = 1
	return cfg
}

// seedTranscript builds a transcript big enough to be over any tiny threshold.
func seedTranscript() []agent.Message {
	filler := strings.Repeat("lorem ipsum dolor sit amet ", 40)
	msgs := make([]agent.Message, 0, 10)
	for i := 0; i < 5; i++ {
		msgs = append(msgs,
			agent.Message{Role: "user", Content: "question " + filler},
			agent.Message{Role: "assistant", Content: "answer " + filler},
		)
	}
	return msgs
}

// TestTurnTriggersAutoCompactHeadless is the regression test for "auto-compact
// never fires on web/desktop": the server turn loop (runTurn) must kick off
// MaybeCompactAsync after a turn, and the resulting summary must be spliced
// into as.messages. Before the fix, only the TUI ever called
// MaybeCompactAsync, so headless sessions grew without bound.
func TestTurnTriggersAutoCompactHeadless(t *testing.T) {
	h := NewHandler()

	seed := seedTranscript()
	as := &agentSession{
		agent:    agent.NewAgent(instantClient{}, nil, autoCompactConfig(), nil),
		model:    "fake-model",
		messages: seed,
	}
	h.wireCompactCallbacks("sess-autocompact", as.agent)
	h.mu.Lock()
	h.agents["sess-autocompact"] = as
	h.mu.Unlock()

	rec := chatRequest(t, h, map[string]any{"content": "hello", "sessionId": "sess-autocompact"})
	if rec.Code != 200 {
		t.Fatalf("chat turn: expected 200, got %d", rec.Code)
	}

	// The turn appended user+assistant (len = seed+2). Auto-compaction runs in
	// a goroutine after the turn; once it lands, the middle of the transcript
	// collapses into a single summary message, so the length must shrink.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		as.mu.Lock()
		n := len(as.messages)
		as.mu.Unlock()
		if n < len(seed)+2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	as.mu.Lock()
	defer as.mu.Unlock()
	t.Fatalf("auto-compaction never spliced the transcript: len=%d (want < %d)", len(as.messages), len(seed)+2)
}

// TestApplyCompactResultSkipsStaleSplice guards the async splice against a
// transcript that shrank between snapshot and result (e.g. a manual /compact
// racing the auto pass): out-of-range indices must be dropped, not panic.
func TestApplyCompactResultSkipsStaleSplice(t *testing.T) {
	h := NewHandler()
	as := &agentSession{
		agent:    agent.NewAgent(instantClient{}, nil, autoCompactConfig(), nil),
		model:    "fake-model",
		messages: []agent.Message{{Role: "user", Content: "only one"}},
	}
	h.mu.Lock()
	h.agents["sess-stale"] = as
	h.mu.Unlock()

	h.applyCompactResult("sess-stale", agent.CompactResult{
		OK:          true,
		ReplaceFrom: 2,
		ReplaceTo:   8,
		Summary:     agent.Message{Role: "assistant", Content: "summary"},
		OriginalLen: 10,
	})

	as.mu.Lock()
	defer as.mu.Unlock()
	if len(as.messages) != 1 || as.messages[0].Content != "only one" {
		t.Fatalf("stale compact result must be dropped, got %d messages", len(as.messages))
	}
}
