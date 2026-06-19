package orchestrator

import (
	"strings"
	"testing"
)

func TestStructuredReport_FormatMarkdown_passed(t *testing.T) {
	r := &StructuredReport{
		Goal:             "add validation",
		TotalIterations:  2,
		AdvisorConsulted: false,
		Passed:           true,
	}
	out := r.FormatMarkdown()
	if !strings.Contains(out, "VALIDATION_PASSED") {
		t.Error("missing VALIDATION_PASSED")
	}
	if !strings.Contains(out, "add validation") {
		t.Error("missing goal")
	}
	if !strings.Contains(out, "2") {
		t.Error("missing iteration count")
	}
}

func TestStructuredReport_FormatMarkdown_halted(t *testing.T) {
	r := &StructuredReport{
		Goal:                 "add validation",
		TotalIterations:      4,
		AdvisorConsulted:     true,
		AdvisorNote:          "Try adding a nil guard at line 12",
		Passed:               false,
		FinalValidatorReport: "### Validation Failure Report\n- **Issue:** nil panic",
		RecommendedNextStep:  "Add nil guard before calling Validate()",
		FilesChanged:         []FileDiff{{Path: "auth.go", Summary: "added validate func"}},
	}
	out := r.FormatMarkdown()
	if !strings.Contains(out, "HALTED") {
		t.Error("missing HALTED")
	}
	if !strings.Contains(out, "nil panic") {
		t.Error("missing validator report")
	}
	if !strings.Contains(out, "Add nil guard") {
		t.Error("missing recommended next step")
	}
	if !strings.Contains(out, "auth.go") {
		t.Error("missing changed file")
	}
	if !strings.Contains(strings.ToLower(out), "advisor") {
		t.Error("missing advisor mention")
	}
}

func TestDeveloperReport_defaults(t *testing.T) {
	dr := &DeveloperReport{Confidence: "low"}
	if dr.Confidence != "low" {
		t.Error("confidence not set")
	}
}
