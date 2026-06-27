package orchestrator

import (
	"testing"
)

func TestParsePlan_naturalLanguage(t *testing.T) {
	raw := `We need to add input validation to the user handler.

Key files: internal/handler/user.go, internal/handler/user_test.go

Success criteria:
- nil input returns an error
- empty username returns an error
- valid input is accepted

verify_mode: build_test_llm
max_iterations: 4`
	p := ParsePlan(raw)
	if p.VerifyMode != "build_test_llm" {
		t.Errorf("verify_mode = %q, want build_test_llm", p.VerifyMode)
	}
	if p.MaxIterations != 4 {
		t.Errorf("max_iterations = %d, want 4", p.MaxIterations)
	}
	if p.Text == "" {
		t.Error("expected non-empty plan text")
	}
}

func TestParsePlan_defaults(t *testing.T) {
	// No keyword lines — fall back to safe defaults
	p := ParsePlan("Just fix the bug in the handler.")
	if p.VerifyMode != "build_test_llm" {
		t.Errorf("default verify_mode = %q, want build_test_llm", p.VerifyMode)
	}
	if p.MaxIterations != 4 {
		t.Errorf("default max_iterations = %d, want 4", p.MaxIterations)
	}
}

func TestParsePlan_boldKeywordStyle(t *testing.T) {
	// Planner may use markdown bold: **verify_mode:** build_llm
	raw := "Fix the crash.\n\n**verify_mode:** build_llm\n**max_iterations:** 3"
	p := ParsePlan(raw)
	if p.VerifyMode != "build_llm" {
		t.Errorf("verify_mode = %q, want build_llm", p.VerifyMode)
	}
	if p.MaxIterations != 3 {
		t.Errorf("max_iterations = %d, want 3", p.MaxIterations)
	}
}

func TestParsePlan_invalidVerifyModeFallsBack(t *testing.T) {
	raw := "Plan text.\nverify_mode: everything\nmax_iterations: 2"
	p := ParsePlan(raw)
	if p.VerifyMode != "build_test_llm" {
		t.Errorf("invalid verify_mode should fall back to build_test_llm, got %q", p.VerifyMode)
	}
}

func TestParsePlan_emptyInputReturnsDefaults(t *testing.T) {
	p := ParsePlan("")
	if p.VerifyMode != "build_test_llm" {
		t.Errorf("empty input: verify_mode = %q", p.VerifyMode)
	}
	if p.MaxIterations != 4 {
		t.Errorf("empty input: max_iterations = %d", p.MaxIterations)
	}
}
