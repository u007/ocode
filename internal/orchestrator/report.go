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
	FinalValidatorReport string // last validator rejection (empty if no validator ever ran on the last attempt)
	LastCompileOutput    string // last compiler output (populated when the most recent iteration failed at compile, not validation)
	FilesChanged         []FileDiff
	RecommendedNextStep  string // advisor's best guess (empty if Passed)
	// Iterations is the per-iteration record: each entry carries the developer
	// brief, files changed, compiler output, and validator report. Surfaced in
	// the HALT markdown so the user can see why each attempt failed.
	Iterations []Iteration
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

	// Surface *why* the pipeline halted. The validator is the usual reason,
	// but when the last attempt failed at compile, the validator never ran
	// and FinalValidatorReport is empty — leaving the user with no reason
	// at all. Show the compile error in that case so the halt is never silent.
	if r.FinalValidatorReport != "" {
		b.WriteString("### Final Validator Report\n\n")
		b.WriteString(r.FinalValidatorReport)
		b.WriteString("\n\n")
	} else if r.LastCompileOutput != "" {
		b.WriteString("### Final Compile Output (validator did not run)\n\n")
		b.WriteString(r.LastCompileOutput)
		b.WriteString("\n\n")
	}

	if len(r.Iterations) > 0 {
		b.WriteString("### Iteration History\n\n")
		for _, iter := range r.Iterations {
			status, detail := iterationStatus(iter)
			b.WriteString(fmt.Sprintf("- **Attempt %d:** %s", iter.Number, status))
			if detail != "" {
				b.WriteString(" — ")
				b.WriteString(detail)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
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

// iterationStatus returns a short status label and one-line detail for a
// single iteration, used by the HALT markdown's "Iteration History" section.
func iterationStatus(iter Iteration) (status, detail string) {
	switch {
	case iter.ValidatorReport != "":
		// Pull the first "- **Issue:**" line if present for a one-line summary.
		for _, line := range strings.Split(iter.ValidatorReport, "\n") {
			if issue, ok := extractField(strings.TrimSpace(line), "**Issue:**"); ok && issue != "" {
				return "validator-fail", issue
			}
		}
		return "validator-fail", ""
	case iter.CompilerOutput != "":
		// Compile failed — surface the first non-empty error line if any.
		for _, line := range strings.Split(iter.CompilerOutput, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			return "compile-fail", firstLine(trimmed)
		}
		return "compile-fail", ""
	default:
		return "no-output", ""
	}
}

// firstLine returns s truncated at the first newline.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
