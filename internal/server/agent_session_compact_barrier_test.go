package server

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// markerSpyClient records, per Chat call, whether the input already carried a
// compaction summary.
type markerSpyClient struct {
	*scriptedAutoContinueClient
	mu       sync.Mutex
	hasMarks []bool
}

func (c *markerSpyClient) Chat(messages []agent.Message, tools []map[string]interface{}) (*agent.Message, error) {
	has := false
	for _, m := range messages {
		if strings.HasPrefix(m.Content, "[ocode:compaction-summary]") {
			has = true
		}
	}
	c.mu.Lock()
	c.hasMarks = append(c.hasMarks, has)
	c.mu.Unlock()
	return c.scriptedAutoContinueClient.Chat(messages, tools)
}

// TestRunTurnCompactsBeforeAutoContinueResume guards the barrier between two
// Steps of one runTurn chain: when the context is over the compaction
// threshold, the auto-continue resume must wait for compaction and run its next
// LLM round on the compacted transcript, not on the pre-compaction one.
func TestRunTurnCompactsBeforeAutoContinueResume(t *testing.T) {
	h, id, cl := autoContinueTestServer(t, "")
	h.mu.Lock()
	h.cfg.Ocode.Compact = autoCompactConfig().Ocode.Compact
	h.mu.Unlock()
	cl.toolCallsUntil = 1
	cl.handler = func(int) string { return "summary of round" }

	as := h.lookupAgentSession(id)
	spy := &markerSpyClient{scriptedAutoContinueClient: cl}
	as.agent = agent.NewAgent(spy, nil, h.cfg, nil)
	as.agent.SetWorkDir(t.TempDir())
	h.wireCompactCallbacks(id, as.agent)
	seed := seedTranscript()
	as.messages = seed

	if _, err := h.runTurn(id, as, "do the work", turnOptions{}); err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	// Call 1 is the first Step (tool round), call 2 its summarize round. Every
	// later non-summarizer round is the resumed Step, which must already see
	// the compacted transcript.
	spy.mu.Lock()
	marks := append([]bool(nil), spy.hasMarks...)
	spy.mu.Unlock()
	resumedSawSummary := false
	for _, has := range marks[min(2, len(marks)):] {
		resumedSawSummary = resumedSawSummary || has
	}
	if !resumedSawSummary {
		t.Fatalf("resumed Step ran on an uncompacted transcript: per-call summary presence=%v", marks)
	}

	summaries := 0
	resumeIdx := -1
	for i, m := range as.messages {
		if m.Role == "user" && m.Content == agent.AutoContinueStepLimitPrompt && resumeIdx < 0 {
			resumeIdx = i
		}
		if strings.HasPrefix(m.Content, "[ocode:compaction-summary]") {
			summaries++
		}
	}
	if resumeIdx < 0 {
		t.Fatalf("no auto-continue resume in transcript: %+v", as.messages)
	}
	if summaries == 0 {
		t.Fatalf("transcript was never compacted before the resume: len=%d", len(as.messages))
	}
	// The post-turn async pass (the threshold here always trips) may still be
	// in flight; every lifecycle slot must retire.
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, _ := h.sessions.State(id)
		if !state.Compacting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("compaction lifecycle never retired: %+v", state)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
