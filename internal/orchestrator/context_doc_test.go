package orchestrator

import (
	"fmt"
	"strings"
	"testing"
)

func baseDoc() *ContextDoc {
	return &ContextDoc{
		Plan: Plan{
			Text: "add user validation\n\nSuccess criteria:\n- nil input returns error\n- valid input passes",
		},
		ExploreSnapshot: "## auth.go\nfunc Validate(u *User) error { ... }",
	}
}

func TestRender_noIterations(t *testing.T) {
	doc := baseDoc()
	out := doc.Render("implement validation")
	if !strings.Contains(out, "[PLAN]") {
		t.Error("missing [PLAN]")
	}
	if !strings.Contains(out, "add user validation") {
		t.Error("missing plan text")
	}
	if !strings.Contains(out, "nil input returns error") {
		t.Error("missing criterion in plan text")
	}
	if !strings.Contains(out, "[CODEBASE CONTEXT]") {
		t.Error("missing [CODEBASE CONTEXT]")
	}
	if !strings.Contains(out, "func Validate") {
		t.Error("missing explore snapshot")
	}
	if strings.Contains(out, "[PRIOR ATTEMPTS") {
		t.Error("should not have prior attempts section with 0 iterations")
	}
	if strings.Contains(out, "[RECENT ATTEMPTS]") {
		t.Error("should not have recent attempts with 0 iterations")
	}
	if !strings.Contains(out, "implement validation") {
		t.Error("missing brief")
	}
}

func TestRender_twoIterationsVerbatim(t *testing.T) {
	doc := baseDoc()
	doc.AddIteration(Iteration{
		Number:          1,
		FilesChanged:    []FileDiff{{Path: "auth.go", Summary: "added guard"}},
		CompilerOutput:  "ok",
		ValidatorReport: "### Validation Failure Report\n- **Issue:** missing nil check",
	})
	doc.AddIteration(Iteration{
		Number:          2,
		FilesChanged:    []FileDiff{{Path: "auth.go", Summary: "fixed nil"}},
		CompilerOutput:  "ok",
		ValidatorReport: "### Validation Failure Report\n- **Issue:** empty string not handled",
	})
	out := doc.Render("fix empty string")
	if strings.Contains(out, "[PRIOR ATTEMPTS SUMMARY]") {
		t.Error("should not summarise when ≤2 iterations")
	}
	if !strings.Contains(out, "[RECENT ATTEMPTS]") {
		t.Error("missing [RECENT ATTEMPTS]")
	}
	if !strings.Contains(out, "Attempt 1") {
		t.Error("missing attempt 1")
	}
	if !strings.Contains(out, "missing nil check") {
		t.Error("missing validator report for attempt 1")
	}
}

func TestRender_threeIterationsTruncates(t *testing.T) {
	doc := baseDoc()
	for i := 1; i <= 3; i++ {
		doc.AddIteration(Iteration{
			Number:          i,
			FilesChanged:    []FileDiff{{Path: "auth.go", Summary: "change"}},
			CompilerOutput:  "ok",
			ValidatorReport: fmt.Sprintf("### Validation Failure Report\n- **Issue:** problem %d", i),
		})
	}
	out := doc.Render("fix problem 3")
	if !strings.Contains(out, "[PRIOR ATTEMPTS SUMMARY]") {
		t.Error("missing prior attempts summary for iteration 1")
	}
	if !strings.Contains(out, "Attempt 1:") {
		t.Error("attempt 1 should appear in summary")
	}
	// attempt 1 full diff should NOT appear (it's summarised)
	if strings.Count(out, "problem 1") > 1 {
		t.Error("attempt 1 full report should not appear verbatim")
	}
	// attempts 2 and 3 should appear verbatim
	if !strings.Contains(out, "problem 2") {
		t.Error("attempt 2 full report missing")
	}
	if !strings.Contains(out, "problem 3") {
		t.Error("attempt 3 full report missing")
	}
}
