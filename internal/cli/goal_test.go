// internal/cli/goal_test.go
package cli

import (
	"strings"
	"testing"
)

func TestParseGoalArgs_goal(t *testing.T) {
	opts, goal, err := ParseGoalArgs([]string{"add user validation"})
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

func TestParseGoalArgs_noWorktree(t *testing.T) {
	opts, _, err := ParseGoalArgs([]string{"--no-worktree", "add user validation"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.UseWorktree != false {
		t.Error("--no-worktree should set UseWorktree=false")
	}
}

func TestParseGoalArgs_verifyFlag(t *testing.T) {
	opts, _, err := ParseGoalArgs([]string{"--verify", "build_test_llm", "add feature"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.VerifyMode != "build_test_llm" {
		t.Errorf("VerifyMode = %q", opts.VerifyMode)
	}
}

func TestParseGoalArgs_maxIterations(t *testing.T) {
	opts, _, err := ParseGoalArgs([]string{"--max-iterations", "6", "goal"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.MaxIterations != 6 {
		t.Errorf("MaxIterations = %d, want 6", opts.MaxIterations)
	}
}

func TestParseGoalArgs_emptyGoal(t *testing.T) {
	_, _, err := ParseGoalArgs([]string{})
	if err == nil {
		t.Error("expected error for empty goal")
	}
	if !strings.Contains(err.Error(), "goal") {
		t.Errorf("error should mention goal, got: %v", err)
	}
}
