package server

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/scheduler"
)

func TestCapCronTokensKeepsHeadAndRecent(t *testing.T) {
	head := agent.Message{Role: "user", Content: "[Scheduled job context]\nJob: ticker\n---\nhi"}
	// Build 20 assistant turns of ~1000 chars each (≈ 250 tokens per turn).
	msgs := []agent.Message{head}
	for i := 0; i < 20; i++ {
		msgs = append(msgs, agent.Message{
			Role:    "assistant",
			Content: strings.Repeat("x", 1000),
		})
	}
	// 20 turns × 250 tokens = 5000 tokens. Budget 1500 should keep the head
	// and at most 5-6 recent turns.
	capped := capCronTokens(msgs, 1500)
	if len(capped) >= len(msgs) {
		t.Fatalf("cap should have trimmed: had %d, after %d", len(msgs), len(capped))
	}
	if capped[0].Content != head.Content {
		t.Fatalf("head must be preserved")
	}
	// The last element should be the most recent turn.
	if capped[len(capped)-1].Content != msgs[len(msgs)-1].Content {
		t.Fatalf("last message should be the most recent turn")
	}
}

func TestCapCronTokensFitsAlreadyFits(t *testing.T) {
	msgs := []agent.Message{
		{Role: "user", Content: "seed"},
		{Role: "assistant", Content: "short"},
	}
	out := capCronTokens(msgs, 100_000)
	if len(out) != len(msgs) {
		t.Fatalf("no-op when under budget: had %d got %d", len(msgs), len(out))
	}
}

// TestCronBlankPermModeResolvesNormal pins the Decision 2/6 cron invariant: a
// job with a blank (or whitespace) perm_mode must resolve to normal — never
// sandbox — even if a prior TUI session had set the live mode to sandbox. The
// web cron UI offers only normal/yolo/locked, so blank is the authored case;
// this guards against a future persistence/override change leaking a
// session-scoped toggle into scheduled runs.
func TestCronBlankPermModeResolvesNormal(t *testing.T) {
	for _, m := range []scheduler.PermissionMode{"", "   ", "\t"} {
		if got := resolveCronPermissionMode(m); got != agent.PermissionModeNormal {
			t.Errorf("perm_mode=%q resolved to %s, want normal", m, got)
		}
	}
}

// TestCronExplicitPermModePassesThrough locks the explicit-value contract: an
// author who deliberately sets perm_mode (including sandbox, accepted by
// SetMode post Part 01) gets that mode; SetMode silently ignores anything
// invalid, preserving the global "invalid mode leaves mode unchanged" rule.
func TestCronExplicitPermModePassesThrough(t *testing.T) {
	cases := []struct {
		in   scheduler.PermissionMode
		want agent.PermissionMode
	}{
		{"normal", agent.PermissionModeNormal},
		{"yolo", agent.PermissionModeYOLO},
		{"locked", agent.PermissionModeLocked},
		{"sandbox", agent.PermissionModeSandbox},
	}
	for _, tc := range cases {
		if got := resolveCronPermissionMode(tc.in); got != tc.want {
			t.Errorf("perm_mode=%q resolved to %s, want %s", tc.in, got, tc.want)
		}
	}

	// Invalid value: the resolver passes it through and SetMode ignores it, so
	// a fresh agent stays normal (the same behavior the guard relies on).
	pm := agent.NewPermissionManager()
	pm.SetMode(resolveCronPermissionMode("bogus"))
	if pm.Mode() != agent.PermissionModeNormal {
		t.Fatalf("invalid perm_mode left agent in %s, want normal (SetMode silent-ignore)", pm.Mode())
	}
}

// TestCronFreshAgentStartsNormalIsolation is the runner-level wiring proof:
// the runner binds resolveCronPermissionMode onto a FRESH agent per firing
// (NewAgent defaults to normal), so a blank perm_mode cannot inherit any live
// override the handler process may be carrying. This mirrors exactly what
// RunScheduledJob does (fresh Agent + SetMode(resolve...)) without needing an
// LLM client for the full turn.
func TestCronFreshAgentStartsNormalIsolation(t *testing.T) {
	ag := agent.NewAgent(nil, nil, nil, nil)
	ag.Permissions().SetMode(resolveCronPermissionMode(""))
	if ag.Permissions().Mode() != agent.PermissionModeNormal {
		t.Fatalf("fresh cron agent with blank perm_mode = %s, want normal", ag.Permissions().Mode())
	}

	// Explicit sandbox is honored on a fresh agent too (the option a session
	// could theoretically leave behind is never picked up, but an authored
	// payload is).
	ag2 := agent.NewAgent(nil, nil, nil, nil)
	ag2.Permissions().SetMode(resolveCronPermissionMode("sandbox"))
	if ag2.Permissions().Mode() != agent.PermissionModeSandbox {
		t.Fatalf("explicit sandbox perm_mode = %s, want sandbox", ag2.Permissions().Mode())
	}
}
