package orchestrator

import (
	"fmt"
	"strings"
)

// StructuredReport is the final output of the pipeline — either a success or
// a HALTED failure with full context for human intervention.
type StructuredReport struct {
	Goal                 string
	TotalIterations      int
	AdvisorConsulted     bool
	AdvisorNote          string
	Passed               bool
	FinalValidatorReport string // last rejection (empty if Passed)
	FilesChanged         []FileDiff
	RecommendedNextStep  string // advisor's best guess (empty if Passed)
}

// DeveloperReport is parsed from the developer agent's completion output.
type DeveloperReport struct {
	FilesChanged            []string
	WhatWasDone             string
	WhatWasNotDone          string
	Confidence              string // "high" | "medium" | "low"
	SuggestedValidatorFocus string
}

// FormatMarkdown renders the report as a human-readable markdown string.
func (r *StructuredReport) FormatMarkdown() string {
	var b strings.Builder
	if r.Passed {
		b.WriteString("## VALIDATION_PASSED\n\n")
		b.WriteString(fmt.Sprintf("**Goal:** %s\n\n", r.Goal))
		b.WriteString(fmt.Sprintf("**Iterations:** %d\n", r.TotalIterations))
		if r.AdvisorConsulted {
			b.WriteString("**Advisor consulted:** yes\n")
		}
		return b.String()
	}

	b.WriteString("## HALTED — Validation did not pass\n\n")
	b.WriteString(fmt.Sprintf("**Goal:** %s\n\n", r.Goal))
	b.WriteString(fmt.Sprintf("**Total iterations:** %d\n", r.TotalIterations))
	if r.AdvisorConsulted {
		b.WriteString("**Advisor consulted:** yes\n")
		if r.AdvisorNote != "" {
			b.WriteString(fmt.Sprintf("**Advisor note:** %s\n", r.AdvisorNote))
		}
	}
	b.WriteString("\n")

	if r.FinalValidatorReport != "" {
		b.WriteString("### Final Validator Report\n\n")
		b.WriteString(r.FinalValidatorReport)
		b.WriteString("\n\n")
	}

	if len(r.FilesChanged) > 0 {
		b.WriteString("### Files in working state (unvalidated)\n\n")
		for _, f := range r.FilesChanged {
			b.WriteString(fmt.Sprintf("- `%s` — %s\n", f.Path, f.Summary))
		}
		b.WriteString("\n")
	}

	if r.RecommendedNextStep != "" {
		b.WriteString("### Recommended next step\n\n")
		b.WriteString(r.RecommendedNextStep)
		b.WriteString("\n")
	}

	return b.String()
}
