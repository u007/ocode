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

// compactSummaryClient is a fake LLM client whose reply satisfies the agent's
// required summary sections, so a manual /compact actually produces a splice.
// It also stands in for the main chat client, which is fine: HandleCompactSession
// only calls Compact.
type compactSummaryClient struct{}

func (compactSummaryClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	return &agent.Message{Role: "assistant", Content: validCompactSummaryForTest()}, nil
}
func (compactSummaryClient) GetProvider() string { return "mock" }
func (compactSummaryClient) GetModel() string    { return "mock-compact" }

// validCompactSummaryForTest mirrors agent.requiredSummarySections (unexported).
func validCompactSummaryForTest() string {
	sections := []string{
		"## Original Request", "## Current Scope", "## User Directives (verbatim)",
		"## Constraints & Preferences", "## Progress", "## Current Work",
		"## Key Decisions", "## Errors & Fixes", "## Next Steps",
		"## Critical Context", "## Relevant Files",
	}
	var b strings.Builder
	for _, s := range sections {
		b.WriteString(s)
		b.WriteString("\n- x\n\n")
	}
	return b.String()
}

// TestManualCompactPublishesContextStatus is the regression test for "after
// /compact the desktop Context sidebar did not reflect": HandleCompactSession
// broadcast only the "messages" event, so the per-session status snapshot (and
// with it the sidebar's context gauge) kept the stale pre-compaction reading
// until the next turn. It must now broadcast a session-tagged "status" event
// carrying the post-compaction estimate (the provider reading was cleared).
func TestManualCompactPublishesContextStatus(t *testing.T) {
	h := NewHandler()
	if h.cfg != nil {
		h.cfg.Model = "gpt-4o-mini"
	}
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)

	as := &agentSession{
		agent:    agent.NewAgent(compactSummaryClient{}, nil, autoCompactConfig(), nil),
		model:    "fake-model",
		messages: seedTranscript(),
	}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()

	sub := h.subscribeHeadless()
	defer h.unsubscribeHeadless(sub)

	rec := httptest.NewRecorder()
	h.HandleCompactSession(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/compact", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("compact status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// The agent records an estimate for the post-splice transcript so the
	// snapshot can report a real number instead of 0/"unknown".
	if got := as.agent.CompactedContextTokens(); got <= 0 {
		t.Fatalf("CompactedContextTokens = %d, want > 0 after compaction", got)
	}

	// The "messages" broadcast precedes the status push; skip until the status
	// snapshot for this session arrives.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-sub:
			if ev.Event != "status" {
				continue
			}
			snap, ok := ev.Data.(TUIStatus)
			if !ok {
				t.Fatalf("status data type = %T, want TUIStatus", ev.Data)
			}
			if snap.SessionID != id {
				continue
			}
			if snap.ContextCurrentTokens <= 0 {
				t.Fatalf("context_current_tokens = %d, want > 0 (post-compaction estimate)", snap.ContextCurrentTokens)
			}
			if snap.ContextMaxTokens <= 0 || snap.ContextModel == "" {
				t.Fatalf("context = %s/%d, want model + window", snap.ContextModel, snap.ContextMaxTokens)
			}
			return
		case <-deadline:
			t.Fatal("no status event broadcast after manual compaction")
		}
	}
}

// TestSessionContextUsesPostCompactionEstimate pins the AGENTS.md "one
// resolution, two entry points" invariant: HandleSessionContext must fall back
// to the agent's post-compaction estimate (like applySessionContext) instead of
// reporting 0 after a /compact cleared LastInputTokens. Reverting the
// CompactedContextTokens fallback makes current_tokens 0 here.
func TestSessionContextUsesPostCompactionEstimate(t *testing.T) {
	h := NewHandler()
	if h.cfg != nil {
		h.cfg.Model = "gpt-4o-mini"
	}
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)

	as := &agentSession{
		agent:    agent.NewAgent(compactSummaryClient{}, nil, autoCompactConfig(), nil),
		model:    "fake-model",
		messages: seedTranscript(),
	}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()

	// Compact in memory (no HTTP, no persistence) so the on-disk transcript
	// stays the 1-message seed while the agent has a live post-compaction
	// estimate. This isolates the agent-estimate branch: if /context ignores
	// CompactedContextTokens it falls through to the ~0 chars/4 disk estimate.
	res, enabled := as.agent.Compact(as.messages)
	if !enabled || !res.OK {
		t.Fatalf("in-memory Compact: enabled=%v ok=%v err=%v", enabled, res.OK, res.Err)
	}
	want := as.agent.CompactedContextTokens()
	if want <= 0 {
		t.Fatalf("CompactedContextTokens = %d, want > 0 after compaction", want)
	}
	diskEstimate := estimateContextFromMessages([]agent.Message{{Role: "user", Content: "hello"}})
	if want <= diskEstimate {
		t.Fatalf("test setup: agent estimate %d not distinguishable from disk estimate %d", want, diskEstimate)
	}

	rec := httptest.NewRecorder()
	h.HandleSessionContext(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/context", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("/context status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		CurrentTokens int64 `json:"current_tokens"`
		Report        *struct {
			Context struct {
				Source string `json:"source"`
			} `json:"context"`
		} `json:"report"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode /context response: %v", err)
	}
	if resp.CurrentTokens != want {
		t.Fatalf("current_tokens = %d, want the agent's post-compaction estimate %d", resp.CurrentTokens, want)
	}
	// The value came from CompactedContextTokens, not a provider reading — the
	// report must not mislabel it "actual".
	if resp.Report != nil && resp.Report.Context.Source == "actual" {
		t.Errorf("report context source = %q, want an estimate label (not \"actual\")", resp.Report.Context.Source)
	}
}
