package server

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
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
