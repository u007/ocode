package orchestrator

import (
	"testing"
)

func TestParsePlan_valid(t *testing.T) {
	raw := `{
		"intent": "feature",
		"goal": "add user validation",
		"success_criteria": ["nil input returns error", "valid input passes"],
		"verify_mode": "build_test_llm",
		"max_iterations": 4
	}`
	p, err := ParsePlan(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Intent != "feature" {
		t.Errorf("intent = %q, want feature", p.Intent)
	}
	if p.Goal != "add user validation" {
		t.Errorf("goal = %q", p.Goal)
	}
	if len(p.SuccessCriteria) != 2 {
		t.Errorf("success_criteria len = %d, want 2", len(p.SuccessCriteria))
	}
	if p.VerifyMode != "build_test_llm" {
		t.Errorf("verify_mode = %q", p.VerifyMode)
	}
	if p.MaxIterations != 4 {
		t.Errorf("max_iterations = %d, want 4", p.MaxIterations)
	}
}

func TestParsePlan_defaultMaxIterations(t *testing.T) {
	raw := `{"intent":"bugfix","goal":"fix crash","success_criteria":["no panic"],"verify_mode":"build_llm"}`
	p, err := ParsePlan(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.MaxIterations != 4 {
		t.Errorf("default max_iterations = %d, want 4", p.MaxIterations)
	}
}

func TestParsePlan_missingGoal(t *testing.T) {
	raw := `{"intent":"feature","success_criteria":["x"],"verify_mode":"llm_only"}`
	_, err := ParsePlan(raw)
	if err == nil {
		t.Fatal("expected error for missing goal")
	}
}

func TestParsePlan_unknownIntent(t *testing.T) {
	raw := `{"intent":"refactor","goal":"g","success_criteria":["x"],"verify_mode":"llm_only"}`
	_, err := ParsePlan(raw)
	if err == nil {
		t.Fatal("expected error for unknown intent")
	}
}

func TestParsePlan_unknownVerifyMode(t *testing.T) {
	raw := `{"intent":"feature","goal":"g","success_criteria":["x"],"verify_mode":"everything"}`
	_, err := ParsePlan(raw)
	if err == nil {
		t.Fatal("expected error for unknown verify_mode")
	}
}

func TestParsePlan_embeddedInProse(t *testing.T) {
	// Planner may wrap JSON in prose — we extract the first JSON object
	raw := `Here is the plan:\n{"intent":"bugfix","goal":"fix nil","success_criteria":["no crash"],"verify_mode":"build_llm"}`
	p, err := ParsePlan(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Intent != "bugfix" {
		t.Errorf("intent = %q", p.Intent)
	}
}
