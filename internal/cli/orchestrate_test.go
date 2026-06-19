// internal/cli/orchestrate_test.go
package cli

import (
	"strings"
	"testing"
)

func TestParseOrchestrateArgs_goal(t *testing.T) {
	opts, goal, err := ParseOrchestrateArgs([]string{"add user validation"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if goal != "add user validation" {
		t.Errorf("goal = %q, want %q", goal, "add user validation")
	}
	if opts.UseWorktree != true {
		t.Error("UseWorktree should default to true")
	}
}

func TestParseOrchestrateArgs_noWorktree(t *testing.T) {
	opts, _, err := ParseOrchestrateArgs([]string{"--no-worktree", "add user validation"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.UseWorktree != false {
		t.Error("--no-worktree should set UseWorktree=false")
	}
}

func TestParseOrchestrateArgs_verifyFlag(t *testing.T) {
	opts, _, err := ParseOrchestrateArgs([]string{"--verify", "build_test_llm", "add feature"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.VerifyMode != "build_test_llm" {
		t.Errorf("VerifyMode = %q", opts.VerifyMode)
	}
}

func TestParseOrchestrateArgs_maxIterations(t *testing.T) {
	opts, _, err := ParseOrchestrateArgs([]string{"--max-iterations", "6", "goal"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.MaxIterations != 6 {
		t.Errorf("MaxIterations = %d, want 6", opts.MaxIterations)
	}
}

func TestParseOrchestrateArgs_emptyGoal(t *testing.T) {
	_, _, err := ParseOrchestrateArgs([]string{})
	if err == nil {
		t.Error("expected error for empty goal")
	}
	if !strings.Contains(err.Error(), "goal") {
		t.Errorf("error should mention goal, got: %v", err)
	}
}
