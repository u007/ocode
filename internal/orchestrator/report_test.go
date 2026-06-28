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
		Iterations: []Iteration{
			{Number: 1, ValidatorReport: "### Validation Failure Report\n- **Issue:** nil panic"},
		},
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
	if !strings.Contains(out, "Iteration History") {
		t.Error("missing iteration history section")
	}
	if !strings.Contains(out, "Attempt 1") {
		t.Error("missing attempt 1 in iteration history")
	}
}

func TestStructuredReport_FormatMarkdown_haltedCompileFailure(t *testing.T) {
	// Regression: when the last iteration fails at compile (not validation),
	// the HALT report previously had no failure reason at all. It must now
	// surface the compile output and the iteration history so the user can
	// see why the pipeline halted.
	r := &StructuredReport{
		Goal:              "add lcars theme",
		TotalIterations:   2,
		AdvisorConsulted:  false,
		Passed:            false,
		LastCompileOutput: "./theme.go:42:12: undefined: lcarsPalette",
		Iterations: []Iteration{
			{Number: 1, CompilerOutput: "./theme.go:42:12: undefined: lcarsPalette"},
		},
	}
	out := r.FormatMarkdown()
	if !strings.Contains(out, "HALTED") {
		t.Error("missing HALTED")
	}
	if !strings.Contains(out, "Final Compile Output") {
		t.Error("missing Final Compile Output section (silent halt bug)")
	}
	if !strings.Contains(out, "undefined: lcarsPalette") {
		t.Error("missing compile error text")
	}
	if !strings.Contains(out, "Iteration History") {
		t.Error("missing iteration history section")
	}
	if !strings.Contains(out, "compile-fail") {
		t.Error("missing compile-fail status in iteration history")
	}
	// Validator section must NOT appear when the validator never ran.
	if strings.Contains(out, "Final Validator Report") {
		t.Error("Final Validator Report should be absent when validator never ran")
	}
}

func TestStructuredReport_FormatMarkdown_haltedMixedHistory(t *testing.T) {
	// Three attempts: 1 = compile-fail, 2 = validator-fail, 3 = compile-fail
	// (which is what halts the loop). The history section must reflect that
	// mix, and the final failure must come from the most recent attempt.
	r := &StructuredReport{
		Goal:                 "ship feature",
		TotalIterations:      3,
		Passed:               false,
		FinalValidatorReport: "", // last attempt was a compile-fail
		LastCompileOutput:    "./feature.go:1:1: expected 'package', found 'EOF'",
		Iterations: []Iteration{
			{Number: 1, CompilerOutput: "./feature.go:7:3: missing return"},
			{
				Number:          2,
				ValidatorReport: "### Validation Failure Report\n- **Issue:** wrong signature on Save()",
			},
			{Number: 3, CompilerOutput: "./feature.go:1:1: expected 'package', found 'EOF'"},
		},
	}
	out := r.FormatMarkdown()
	if !strings.Contains(out, "compile-fail") {
		t.Error("expected compile-fail in history")
	}
	if !strings.Contains(out, "validator-fail") {
		t.Error("expected validator-fail in history")
	}
	if !strings.Contains(out, "wrong signature on Save()") {
		t.Error("missing validator issue detail")
	}
	if !strings.Contains(out, "expected 'package'") {
		t.Error("missing last compile error detail")
	}
}

func TestDeveloperReport_defaults(t *testing.T) {
	dr := &DeveloperReport{Confidence: "low"}
	if dr.Confidence != "low" {
		t.Error("confidence not set")
	}
}
