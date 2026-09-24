package server

import (
	"math"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

func usagePtr(v int64) *int64 { return &v }

// The token metadata keys are a cross-surface contract: the TUI's
// sidebarTelemetry reads and writes exactly these (metadata() /
// telemetryFromSessionMetadata). Renaming them here would silently orphan the
// shared running total between the TUI and web/desktop, so pin them.
func TestSessionUsageMetadataKeysMatchTUI(t *testing.T) {
	if inputTokensMetadataKey != "input_tokens" ||
		outputTokensMetadataKey != "output_tokens" ||
		cachedTokensMetadataKey != "cached_tokens" ||
		billedTokensMetadataKey != "billed_tokens" {
		t.Fatalf("token metadata keys drifted from the TUI contract: %q %q %q %q",
			inputTokensMetadataKey, outputTokensMetadataKey, cachedTokensMetadataKey, billedTokensMetadataKey)
	}
}

// TestTokenCountsFromMessageNormalizesCacheRead pins the anti-double-count
// rule: for a provider that folds cache reads into the prompt count, the
// normalized input excludes them while the cached bucket still counts them
// once, so the web sidebar's In/Cache split matches the TUI's.
func TestTokenCountsFromMessageNormalizesCacheRead(t *testing.T) {
	msg := agent.Message{Usage: &agent.TokenUsage{
		PromptTokens:            usagePtr(1000),
		CompletionTokens:        usagePtr(200),
		CacheReadTokens:         usagePtr(800),
		CacheWriteTokens:        usagePtr(50),
		TotalTokens:             usagePtr(1200),
		PromptIncludesCacheRead: true,
	}}
	in, out, cached, total := tokenCountsFromMessage(msg)
	if in != 200 {
		t.Fatalf("in = %d, want 200 (prompt 1000 minus cache-read 800)", in)
	}
	if out != 200 {
		t.Fatalf("out = %d, want 200", out)
	}
	if cached != 850 {
		t.Fatalf("cached = %d, want 850 (read 800 + write 50)", cached)
	}
	if total != 1200 {
		t.Fatalf("total = %d, want 1200 (provider-reported)", total)
	}
}

// TestTokenCountsFromMessageFallbackTotal covers a provider that reports no
// TotalTokens: the fallback is normalized-in + out, mirroring the TUI.
func TestTokenCountsFromMessageFallbackTotal(t *testing.T) {
	msg := agent.Message{Usage: &agent.TokenUsage{
		PromptTokens:     usagePtr(300),
		CompletionTokens: usagePtr(100),
		CacheReadTokens:  usagePtr(50),
	}}
	in, out, cached, total := tokenCountsFromMessage(msg)
	if in != 300 || out != 100 || cached != 50 || total != 400 {
		t.Fatalf("got in=%d out=%d cached=%d total=%d, want 300/100/50/400", in, out, cached, total)
	}
}

// TestAgentSessionUsageAccumulates pins the per-session token accumulator:
// summing a Step's message usage plus side-path raw usage must produce the
// session's own totals.
func TestAgentSessionUsageAccumulates(t *testing.T) {
	as := &agentSession{}
	as.addUsageFromMessages([]agent.Message{
		{Usage: &agent.TokenUsage{PromptTokens: usagePtr(100), CompletionTokens: usagePtr(20), CacheReadTokens: usagePtr(30)}},
		{Usage: nil},
		{Usage: &agent.TokenUsage{PromptTokens: usagePtr(50), CompletionTokens: usagePtr(10)}},
	})
	as.addRawUsage(5, 7, 2, 3) // side call (advisor/compact)
	as.addUsageFromMessages(nil)

	in, out, cached, total := as.usageSnapshot()
	if in != 155 || out != 37 || cached != 35 || total != 192 {
		t.Fatalf("snapshot = %d/%d/%d/%d, want 155/37/35/192", in, out, cached, total)
	}
	if !as.hasUsage() {
		t.Fatal("hasUsage() = false with non-zero counts")
	}
	if (&agentSession{}).hasUsage() {
		t.Fatal("hasUsage() = true for an empty accumulator")
	}
}

// TestSeedUsageNeverLowers covers the history-restore rule: seeding from a
// stale persisted total must not move any field backwards, and a newer value
// must raise it. Mirrors seedSpend.
func TestSeedUsageNeverLowers(t *testing.T) {
	as := &agentSession{}
	as.seedUsage(100, 50, 20, 150)
	as.seedUsage(90, 60, 10, 140) // stale for in/cached/total, newer for out
	in, out, cached, total := as.usageSnapshot()
	if in != 100 || out != 60 || cached != 20 || total != 150 {
		t.Fatalf("snapshot = %d/%d/%d/%d, want 100/60/20/150", in, out, cached, total)
	}
}

// TestSessionUsageFromMetadata covers the persisted-key read, including the
// legacy key names an older TUI wrote.
func TestSessionUsageFromMetadata(t *testing.T) {
	in, out, cached, total := sessionUsageFromMetadata(map[string]any{
		"input_tokens":  int64(1000),
		"output_tokens": float64(200),
		"cached_tokens": 300,
		"billed_tokens": 1200,
	})
	if in != 1000 || out != 200 || cached != 300 || total != 1200 {
		t.Fatalf("new keys: got %d/%d/%d/%d, want 1000/200/300/1200", in, out, cached, total)
	}

	in, out, cached, total = sessionUsageFromMetadata(map[string]any{
		"prompt_tokens":     7,
		"completion_tokens": 3,
		"total_tokens":      10,
	})
	if in != 7 || out != 3 || cached != 0 || total != 10 {
		t.Fatalf("legacy keys: got %d/%d/%d/%d, want 7/3/0/10", in, out, cached, total)
	}

	if in, out, cached, total = sessionUsageFromMetadata(nil); in != 0 || out != 0 || cached != 0 || total != 0 {
		t.Fatalf("nil metadata = %d/%d/%d/%d, want all zero", in, out, cached, total)
	}
}

// TestApplySessionUsageLiveAgentWins pins the precedence: the live agent's
// atomics win over both metadata and the transcript (no double counting).
func TestApplySessionUsageLiveAgentWins(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate the usage ledger
	h := NewHandler()
	proj := t.TempDir()
	h.SetWorkDir(proj)
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	if err := session.UpdateMetadataForDir(proj, id, func(md map[string]any) {
		md[spentUSDMetadataKey] = 9.0
		md[inputTokensMetadataKey] = 1
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	as := &agentSession{}
	as.addSpendUSD(1.5)
	as.seedUsage(500, 100, 50, 600)
	h.agents[id] = as

	var snap TUIStatus
	h.applySessionUsage(&snap, id)
	if snap.SpendingUSD != 1.5 {
		t.Fatalf("SpendingUSD = %v, want 1.5 (live agent wins)", snap.SpendingUSD)
	}
	if snap.InputTokens != 500 || snap.OutputTokens != 100 || snap.CachedTokens != 50 || snap.TotalTokens != 600 {
		t.Fatalf("tokens = %d/%d/%d/%d, want 500/100/50/600",
			snap.InputTokens, snap.OutputTokens, snap.CachedTokens, snap.TotalTokens)
	}
}

// TestApplySessionUsageRestoredFromMetadata covers the restored/evicted path:
// with no live agent the persisted totals are read back so a session created
// (or last used) in the TUI shows its real history in the web sidebar.
func TestApplySessionUsageRestoredFromMetadata(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.SetWorkDir(proj)
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	if err := session.UpdateMetadataForDir(proj, id, func(md map[string]any) {
		md[spentUSDMetadataKey] = 7.5
		md[inputTokensMetadataKey] = int64(1000)
		md[outputTokensMetadataKey] = int64(200)
		md[cachedTokensMetadataKey] = int64(300)
		md[billedTokensMetadataKey] = int64(1200)
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	var snap TUIStatus
	h.applySessionUsage(&snap, id)
	if snap.SpendingUSD != 7.5 {
		t.Fatalf("SpendingUSD = %v, want 7.5 (persisted)", snap.SpendingUSD)
	}
	if snap.InputTokens != 1000 || snap.OutputTokens != 200 || snap.CachedTokens != 300 || snap.TotalTokens != 1200 {
		t.Fatalf("tokens = %d/%d/%d/%d, want 1000/200/300/1200",
			snap.InputTokens, snap.OutputTokens, snap.CachedTokens, snap.TotalTokens)
	}
}

// TestApplySessionUsageWithoutMetadataLeavesTokensUnknown pins the documented
// limit: Usage/Spend are json:"-" on agent.Message, so the transcript does NOT
// carry per-message usage. A restored session with no token metadata therefore
// reports no token breakdown (the same "n/a" the TUI shows), rather than a
// fabricated zero. Spend still recovers from the ledger when present.
func TestApplySessionUsageWithoutMetadataLeavesTokensUnknown(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate the usage ledger
	h := NewHandler()
	proj := t.TempDir()
	h.SetWorkDir(proj)
	id := session.NewSessionID()
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })
	msgs := []agent.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Usage: &agent.TokenUsage{
			PromptTokens:     usagePtr(1000),
			CompletionTokens: usagePtr(200),
			CacheReadTokens:  usagePtr(300),
			TotalTokens:      usagePtr(1500),
		}},
	}
	if err := session.Save(id, "tokens", msgs, nil); err != nil {
		t.Fatalf("save session: %v", err)
	}

	var snap TUIStatus
	h.applySessionUsage(&snap, id)
	if snap.InputTokens != 0 || snap.OutputTokens != 0 || snap.CachedTokens != 0 || snap.TotalTokens != 0 {
		t.Fatalf("tokens = %d/%d/%d/%d, want all zero (transcript does not persist usage)",
			snap.InputTokens, snap.OutputTokens, snap.CachedTokens, snap.TotalTokens)
	}
}

// TestPersistThenRestoreSessionUsageRoundTrip exercises the real persistence
// function against the keys the TUI reads: turn-end persist → cold snapshot
// read (no live agent) must return the same totals.
func TestPersistThenRestoreSessionUsageRoundTrip(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.SetWorkDir(proj)
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)

	as := &agentSession{}
	as.addSpendUSD(1.25)
	as.addUsageFromMessages([]agent.Message{{Usage: &agent.TokenUsage{
		PromptTokens:     usagePtr(1000),
		CompletionTokens: usagePtr(200),
		CacheReadTokens:  usagePtr(300),
		TotalTokens:      usagePtr(1500),
	}}})
	h.persistSessionTelemetry(id, as)

	var snap TUIStatus
	h.applySessionUsage(&snap, id)
	if snap.SpendingUSD < 1.2499 || snap.SpendingUSD > 1.2501 {
		t.Fatalf("restored SpendingUSD = %v, want 1.25", snap.SpendingUSD)
	}
	if snap.InputTokens != 1000 || snap.OutputTokens != 200 || snap.CachedTokens != 300 || snap.TotalTokens != 1500 {
		t.Fatalf("restored tokens = %d/%d/%d/%d, want 1000/200/300/1500",
			snap.InputTokens, snap.OutputTokens, snap.CachedTokens, snap.TotalTokens)
	}
	s, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if in, out, cached, total := sessionUsageFromMetadata(s.Metadata); in != 1000 || out != 200 || cached != 300 || total != 1500 {
		t.Fatalf("metadata tokens = %d/%d/%d/%d, want 1000/200/300/1500", in, out, cached, total)
	}
}

// TestBuildAgentSessionSeedsUsageFromMetadata pins the history-restore seed at
// agent build: without it a resumed session's token totals would restart at 0.
func TestBuildAgentSessionSeedsUsageFromMetadata(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.SetWorkDir(proj)
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	if err := session.UpdateMetadataForDir(proj, id, func(md map[string]any) {
		md[inputTokensMetadataKey] = int64(1111)
		md[outputTokensMetadataKey] = int64(222)
		md[cachedTokensMetadataKey] = int64(333)
		md[billedTokensMetadataKey] = int64(1444)
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("buildAgentSession: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	in, out, cached, total := as.usageSnapshot()
	if in != 1111 || out != 222 || cached != 333 || total != 1444 {
		t.Fatalf("seeded tokens = %d/%d/%d/%d, want 1111/222/333/1444", in, out, cached, total)
	}
}

// TestBuildAgentSessionOnSideUsageCapturesTokens pins the OnSideUsage wiring:
// side-path token usage (advisor/compact/recap/…) must land in the session
// totals, not just its spend.
func TestBuildAgentSessionOnSideUsageCapturesTokens(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.SetWorkDir(proj)
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("buildAgentSession: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()

	spend := 0.25
	as.agent.OnSideUsage(100, 40, 25, 5, &spend)
	in, out, cached, total := as.usageSnapshot()
	if in != 100 || out != 40 || cached != 30 || total != 140 {
		t.Fatalf("side-path tokens = %d/%d/%d/%d, want 100/40/30/140", in, out, cached, total)
	}
	if got := as.spendUSD(); math.Abs(got-0.25) > 1e-9 {
		t.Fatalf("side-path spend = %v, want 0.25", got)
	}
}

// usageClient returns a message carrying provider usage + spend so the headless
// turn path (runTurn → addUsageFromMessages/addSpendFromMessages) is exercised
// end to end.
type usageClient struct{}

func (usageClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	spend := 0.5
	return &agent.Message{
		Role:    "assistant",
		Content: "hi",
		Usage: &agent.TokenUsage{
			PromptTokens:     usagePtr(1000),
			CompletionTokens: usagePtr(200),
			CacheReadTokens:  usagePtr(300),
			TotalTokens:      usagePtr(1500),
		},
		Spend: &spend,
	}, nil
}
func (usageClient) GetProvider() string { return "fake" }
func (usageClient) GetModel() string    { return "fake-model" }

// TestRunTurnAccumulatesSessionTokens is the integration regression for the
// web/desktop "missing token counts": a headless turn's Step usage must reach
// the session accumulator, the status snapshot, and (via the turn-end status
// publish) the persisted metadata a restart restores from.
func TestRunTurnAccumulatesSessionTokens(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	newTestSession(h, id, usageClient{})

	if _, err := h.runTurn(id, h.lookupAgentSession(id), "hi", turnOptions{}); err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	as := h.lookupAgentSession(id)
	in, out, cached, total := as.usageSnapshot()
	if in != 1000 || out != 200 || cached != 300 || total != 1500 {
		t.Fatalf("accumulated tokens = %d/%d/%d/%d, want 1000/200/300/1500", in, out, cached, total)
	}
	if got := as.spendUSD(); math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("accumulated spend = %v, want 0.5", got)
	}

	// The per-session status snapshot must carry the tokens for the web UI.
	var snap TUIStatus
	h.applySessionUsage(&snap, id)
	if snap.InputTokens != 1000 || snap.OutputTokens != 200 || snap.CachedTokens != 300 || snap.TotalTokens != 1500 {
		t.Fatalf("status tokens = %d/%d/%d/%d, want 1000/200/300/1500",
			snap.InputTokens, snap.OutputTokens, snap.CachedTokens, snap.TotalTokens)
	}

	// Turn end persists the totals into metadata, so a cold read (no live agent)
	// still shows them after a restart.
	h.mu.Lock()
	delete(h.agents, id)
	h.mu.Unlock()
	var cold TUIStatus
	h.applySessionUsage(&cold, id)
	if cold.InputTokens != 1000 || cold.OutputTokens != 200 || cold.CachedTokens != 300 || cold.TotalTokens != 1500 {
		t.Fatalf("restored tokens = %d/%d/%d/%d, want 1000/200/300/1500 (persisted at turn end)",
			cold.InputTokens, cold.OutputTokens, cold.CachedTokens, cold.TotalTokens)
	}
}
