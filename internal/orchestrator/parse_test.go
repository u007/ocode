package orchestrator

import (
	"testing"
)

func TestExtractVerdict_passed(t *testing.T) {
	v, _ := ExtractVerdict("VALIDATION_PASSED")
	if v != VerdictPassed {
		t.Errorf("got %v, want VerdictPassed", v)
	}
}

func TestExtractVerdict_passedInProse(t *testing.T) {
	v, _ := ExtractVerdict("After careful review:\nVALIDATION_PASSED\nAll checks complete.")
	if v != VerdictPassed {
		t.Errorf("got %v, want VerdictPassed", v)
	}
}

func TestExtractVerdict_failed(t *testing.T) {
	report := "### Validation Failure Report\n- **Issue:** nil panic\n- **Target File:** `auth.go`"
	v, body := ExtractVerdict(report)
	if v != VerdictFailed {
		t.Errorf("got %v, want VerdictFailed", v)
	}
	if body == "" {
		t.Error("body should not be empty for failed verdict")
	}
}

func TestExtractVerdict_malformed(t *testing.T) {
	v, _ := ExtractVerdict("I looked at the code and it seems fine to me.")
	if v != VerdictMalformed {
		t.Errorf("got %v, want VerdictMalformed", v)
	}
}

func TestExtractDeveloperReport_withSentinel(t *testing.T) {
	raw := `I've made the changes.

### Developer Completion Report
- **Files Changed:** [auth.go, auth_test.go]
- **What Was Done:** Added nil guard
- **What Was NOT Done:** Did not add logging
- **Confidence:** high
- **Suggested Validator Focus:** line 42 nil dereference`

	dr := ExtractDeveloperReport(raw)
	if dr.Confidence != "high" {
		t.Errorf("confidence = %q, want high", dr.Confidence)
	}
	if dr.WhatWasDone != "Added nil guard" {
		t.Errorf("WhatWasDone = %q", dr.WhatWasDone)
	}
	if dr.SuggestedValidatorFocus != "line 42 nil dereference" {
		t.Errorf("SuggestedValidatorFocus = %q", dr.SuggestedValidatorFocus)
	}
}

func TestExtractDeveloperReport_noSentinel(t *testing.T) {
	dr := ExtractDeveloperReport("I changed some stuff.")
	if dr.Confidence != "low" {
		t.Errorf("no-sentinel confidence = %q, want low", dr.Confidence)
	}
}

func TestExtractPlanJSON_valid(t *testing.T) {
	raw := `{"intent":"feature","goal":"add auth","success_criteria":["works"],"verify_mode":"build_llm"}`
	p, err := ExtractPlanJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Intent != "feature" {
		t.Errorf("intent = %q", p.Intent)
	}
}
