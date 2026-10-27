package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// stubCommitMessage replaces the agent-side generator for one test.
func stubCommitMessage(t *testing.T, fn func(ctx context.Context, cfg *config.Config, profile, diff string) (*agent.CommitMessageResult, error)) {
	t.Helper()
	prev := generateCommitMessage
	generateCommitMessage = fn
	t.Cleanup(func() { generateCommitMessage = prev })
}

func TestCommitMsgGeneratorWritesGeneratedText(t *testing.T) {
	var gotDiff string
	stubCommitMessage(t, func(_ context.Context, _ *config.Config, profile, diff string) (*agent.CommitMessageResult, error) {
		if profile != "" {
			t.Errorf("TUI generation must not force a profile, got %q", profile)
		}
		gotDiff = diff
		return &agent.CommitMessageResult{Text: "fix: trimmed"}, nil
	})

	m := &model{}
	result, ok := m.makeCommitMsgGenerator(&config.Config{})("DIFF-TEXT")().(gitCommitMsgMsg)
	if !ok {
		t.Fatal("generator did not return gitCommitMsgMsg")
	}
	if result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	if result.text != "fix: trimmed" {
		t.Fatalf("text = %q, want the agent-trimmed message", result.text)
	}
	if gotDiff != "DIFF-TEXT" {
		t.Fatalf("diff forwarded = %q, want the staged diff", gotDiff)
	}
}

// The provider call runs under a deadline, so a stalled provider cannot leave
// the Git tab's generating state stuck forever.
func TestCommitMsgGeneratorBoundsGenerationWithDeadline(t *testing.T) {
	var deadline time.Time
	var hasDeadline bool
	stubCommitMessage(t, func(ctx context.Context, _ *config.Config, _, _ string) (*agent.CommitMessageResult, error) {
		deadline, hasDeadline = ctx.Deadline()
		return &agent.CommitMessageResult{Text: "fix: bounded"}, nil
	})

	m := &model{}
	if _, ok := m.makeCommitMsgGenerator(&config.Config{})("DIFF")().(gitCommitMsgMsg); !ok {
		t.Fatal("generator did not return gitCommitMsgMsg")
	}
	if !hasDeadline {
		t.Fatal("generator context has no deadline")
	}
	if left := time.Until(deadline); left <= 0 || left > commitMessageTimeout {
		t.Fatalf("deadline in %v, want within %v", left, commitMessageTimeout)
	}
}

func TestCommitMsgGeneratorSurfacesGeneratorError(t *testing.T) {
	stubCommitMessage(t, func(context.Context, *config.Config, string, string) (*agent.CommitMessageResult, error) {
		return nil, errors.New("small/model: rate limited")
	})

	m := &model{}
	result, ok := m.makeCommitMsgGenerator(&config.Config{})("DIFF")().(gitCommitMsgMsg)
	if !ok {
		t.Fatal("generator did not return gitCommitMsgMsg")
	}
	if result.err == nil || result.err.Error() != "small/model: rate limited" {
		t.Fatalf("err = %v, want the generator error surfaced", result.err)
	}
}

func TestCommitMsgGeneratorNilResultIsAnError(t *testing.T) {
	stubCommitMessage(t, func(context.Context, *config.Config, string, string) (*agent.CommitMessageResult, error) {
		return nil, nil
	})

	m := &model{}
	result, ok := m.makeCommitMsgGenerator(&config.Config{})("DIFF")().(gitCommitMsgMsg)
	if !ok {
		t.Fatal("generator did not return gitCommitMsgMsg")
	}
	if result.err == nil {
		t.Fatal("a nil result without an error must not become an empty commit message")
	}
}

// Usage must be booked from the result the agent returned (which model actually
// produced the message is decided there).
func TestCommitMsgGeneratorEmitsSideUsage(t *testing.T) {
	spend := 0.25
	stubCommitMessage(t, func(context.Context, *config.Config, string, string) (*agent.CommitMessageResult, error) {
		return &agent.CommitMessageResult{
			Text:  "fix: usage",
			Model: "main/model",
			Usage: &agent.TokenUsage{
				PromptTokens:     int64Ptr(11),
				CompletionTokens: int64Ptr(4),
			},
			Spend: &spend,
		}, nil
	})

	m := &model{sideUsageCh: make(chan sideUsageData, 1)}
	result, ok := m.makeCommitMsgGenerator(&config.Config{})("DIFF")().(gitCommitMsgMsg)
	if !ok {
		t.Fatal("generator did not return gitCommitMsgMsg")
	}
	if result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	select {
	case got := <-m.sideUsageCh:
		if got.promptTokens != 11 || got.completionTokens != 4 {
			t.Fatalf("side usage = %+v, want 11 prompt / 4 completion tokens", got)
		}
		if got.spend == nil || *got.spend != spend {
			t.Fatalf("side usage spend = %v, want %v", got.spend, spend)
		}
	default:
		t.Fatal("expected side usage from the generated message")
	}
}
