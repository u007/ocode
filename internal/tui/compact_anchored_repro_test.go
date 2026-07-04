package tui

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

type fakeCompactSummaryClient struct{}

func (fakeCompactSummaryClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	return &agent.Message{Role: "assistant", Content: "fresh compressed summary"}, nil
}
func (fakeCompactSummaryClient) GetProvider() string { return "" }
func (fakeCompactSummaryClient) GetModel() string    { return "fake-compact-model" }

func compactCfg() *config.Config {
	cfg := &config.Config{}
	cfg.Ocode.Compact.Enabled = true
	cfg.Ocode.Compact.KeepRecentTurns = 3
	cfg.Ocode.Compact.MinMessages = 1
	cfg.Ocode.Compact.SummaryTimeoutSeconds = 5
	cfg.Ocode.Compact.SummaryMaxRetries = 0
	cfg.Ocode.Compact.MaxSummaryInputTokens = 100000
	return cfg
}

// Reproduces the reported bug: a session that already contains a prior
// compaction summary banner. buildAgentMessagesSnapshot -> Compact -> apply
// should reduce the transcript again (anchored re-compaction). If apply
// returns false, compaction silently fails forever after the first pass.
func TestAnchoredReCompactionAppliesToTranscript(t *testing.T) {
	prevSummary := &agent.Message{
		Role:    "system",
		Content: "[ocode:compaction-summary]\nCompacted summary covering 4 messages\n\n## Goal\n- earlier work",
	}
	m := model{
		viewport: fastviewport.New(80, 20),
		styles:   ApplyThemeColors("tokyonight"),
		agent:    agent.NewAgent(fakeCompactSummaryClient{}, nil, compactCfg(), nil),
		messages: []message{
			{role: roleAssistant, text: "──────────────────────────────────────────────────"},
			{role: roleAssistant, text: "▣ Compacted 4 earlier messages", raw: prevSummary},
			{role: roleUser, text: "task two please"},
			{role: roleAssistant, text: "did task two"},
			{role: roleUser, text: "task three please"},
			{role: roleAssistant, text: "did task three"},
			{role: roleUser, text: "task four please"},
			{role: roleAssistant, text: "did task four"},
			{role: roleUser, text: "task five please"},
			{role: roleAssistant, text: "did task five"},
		},
	}

	before := len(m.messages)
	snap, uiIdx := m.buildAgentMessagesSnapshot()
	result, enabled := m.agent.Compact(snap)
	if !enabled {
		t.Fatal("compaction unexpectedly disabled")
	}
	if !result.OK {
		t.Fatalf("compaction did not produce a result: %+v", result)
	}
	t.Logf("result: ReplaceFrom=%d ReplaceTo=%d len(uiIdx)=%d len(m.messages)=%d",
		result.ReplaceFrom, result.ReplaceTo, len(uiIdx), len(m.messages))

	ok, _ := m.applyCompactionResult(result, uiIdx)
	if !ok {
		t.Fatalf("applyCompactionResult returned false — transcript NOT reduced (this is the bug). "+
			"ReplaceFrom=%d ReplaceTo=%d len(uiIdx)=%d len(m.messages)=%d",
			result.ReplaceFrom, result.ReplaceTo, len(uiIdx), before)
	}
	if len(m.messages) >= before {
		t.Fatalf("transcript not reduced: before=%d after=%d", before, len(m.messages))
	}
	joined := ""
	for _, mm := range m.messages {
		joined += mm.text + "\n"
	}
	if !strings.Contains(joined, "▣ Compacted") {
		t.Fatalf("expected a compaction banner after apply, got:\n%s", joined)
	}
}

// Reproduces the root cause of the "log says done, chat shows nothing" bug:
// a background job completing WHILE a compaction is in flight must NOT start a
// new turn/compaction. If it does, it overwrites pendingCompactUIIdx and the
// in-flight compaction's result is applied against the wrong (or nil) mapping,
// silently discarding the compaction. Job completions during compaction must
// be deferred to pendingJobMsgs, exactly like completions during streaming.
func TestBackgroundJobDuringCompactionIsDeferred(t *testing.T) {
	m := model{
		viewport:            fastviewport.New(80, 20),
		styles:              ApplyThemeColors("tokyonight"),
		agent:               agent.NewAgent(fakeCompactSummaryClient{}, nil, compactCfg(), nil),
		compacting:          true,               // a compaction is in flight
		pendingCompactUIIdx: []int{-1, 0, 1, 2}, // its captured mapping — must survive
		messages: []message{
			{role: roleUser, text: "start the migration"},
			{role: roleAssistant, text: "running it in the background"},
		},
	}
	uiIdxBefore := len(m.pendingCompactUIIdx)
	msgsBefore := len(m.messages)

	ev := agent.JobEvent{Kind: "process", ID: "p1", Name: "npm run build", Status: "exited", Result: "build ok"}
	updated, _ := m.Update(jobCompletedMsg{agent: m.agent, ev: ev})
	got := updated.(model)

	if len(got.pendingJobMsgs) == 0 {
		t.Fatalf("job completing during compaction must be deferred to pendingJobMsgs, got 0 deferred")
	}
	if len(got.pendingCompactUIIdx) != uiIdxBefore {
		t.Fatalf("pendingCompactUIIdx must not change while a compaction is pending: before=%d after=%d",
			uiIdxBefore, len(got.pendingCompactUIIdx))
	}
	// The job body must NOT have been injected into the live transcript as a
	// new turn (that path calls askAgent and races the pending compaction).
	if len(got.messages) != msgsBefore {
		t.Fatalf("job completion must not append to transcript during compaction: before=%d after=%d",
			msgsBefore, len(got.messages))
	}
}
