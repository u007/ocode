package tui

import (
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// rebuildAgentClient replaces m.agent wholesale, so it must re-apply the same
// live wiring installAgent performs — most critically wireCompactCallbacks
// (whose doc comment requires re-invocation whenever m.agent is replaced).
// Without it the rebuilt agent's OnCompact is nil: the next auto-compaction's
// result is silently dropped, pendingCompactUIIdx never clears, and both
// compaction and input submission stay gated off for the rest of the session.
func TestRebuildAgentClientRewiresCompactCallbacks(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	cfg := &config.Config{Model: "deepseek/deepseek-chat"}
	prev := agent.NewAgent(fakeCompactSummaryClient{}, nil, cfg, nil)

	m := model{
		config:         cfg,
		agent:          prev,
		compactCh:      make(chan agent.CompactResult, 4),
		compactStartCh: make(chan struct{}, 4),
		recapCh:        make(chan recapFinishedMsg, 1),
		usageCh:        make(chan usageEvent, 16),
	}

	m.rebuildAgentClient()

	if m.agent == nil {
		t.Fatal("rebuildAgentClient left m.agent nil")
	}
	if m.agent == prev {
		t.Fatal("rebuildAgentClient did not replace the agent")
	}
	if m.agent.OnCompact == nil {
		t.Error("rebuilt agent has nil OnCompact — compaction results would be silently dropped")
	}
	if m.agent.OnCompactStart == nil {
		t.Error("rebuilt agent has nil OnCompactStart — compacting indicator would never show")
	}
	if m.agent.OnRecap == nil {
		t.Error("rebuilt agent has nil OnRecap")
	}
	if m.agent.OnUsage == nil {
		t.Error("rebuilt agent has nil OnUsage")
	}
}

func TestRebuildAgentClientClearsStalePendingSubmit(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	cfg := &config.Config{Model: "deepseek/deepseek-chat"}
	prev := agent.NewAgent(fakeCompactSummaryClient{}, nil, cfg, nil)

	m := model{
		config:             cfg,
		agent:              prev,
		pendingSubmit:      "queued message",
		pendingSubmitAgent: prev,
		compactCh:          make(chan agent.CompactResult, 4),
		compactStartCh:     make(chan struct{}, 4),
		recapCh:            make(chan recapFinishedMsg, 1),
		usageCh:            make(chan usageEvent, 16),
	}

	m.rebuildAgentClient()

	if m.pendingSubmit != "" {
		t.Fatalf("pendingSubmit not cleared: %q", m.pendingSubmit)
	}
	if m.pendingSubmitAgent != nil {
		t.Fatalf("pendingSubmitAgent not cleared: %#v", m.pendingSubmitAgent)
	}
}

func TestRebuildAgentClientPreservesOpenCodeSessionID(t *testing.T) {
	cfg := &config.Config{Model: "opencode-go/mimo-v2.5"}
	prev := agent.NewAgent(&agent.GenericClient{Provider: "opencode-go"}, nil, cfg, nil)
	prev.SetOpenCodeSessionID("tui-session")

	m := model{
		config:         cfg,
		sessionID:      "tui-session",
		agent:          prev,
		compactCh:      make(chan agent.CompactResult, 4),
		compactStartCh: make(chan struct{}, 4),
		recapCh:        make(chan recapFinishedMsg, 1),
		usageCh:        make(chan usageEvent, 16),
	}
	m.rebuildAgentClient()

	if m.agent == nil {
		t.Fatal("rebuildAgentClient left m.agent nil")
	}
	if got := m.agent.OpenCodeSessionID(); got != "tui-session" {
		t.Fatalf("rebuilt agent OpenCode session ID = %q, want %q", got, "tui-session")
	}
}
