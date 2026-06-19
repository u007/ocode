package orchestrator

import (
	"strings"
)

// Verdict is the result of parsing a validator agent response.
type Verdict int

const (
	VerdictPassed    Verdict = iota // response contains "VALIDATION_PASSED"
	VerdictFailed                   // response contains "### Validation Failure Report"
	VerdictMalformed                // neither sentinel found
)

// ExtractVerdict searches s for the validator output sentinels.
// Returns the verdict and, for VerdictFailed, the failure report body
// (from the sentinel to end of string).
func ExtractVerdict(s string) (Verdict, string) {
	if strings.Contains(s, "VALIDATION_PASSED") {
		return VerdictPassed, ""
	}
	const failSentinel = "### Validation Failure Report"
	if i := strings.Index(s, failSentinel); i >= 0 {
		return VerdictFailed, s[i:]
	}
	return VerdictMalformed, s
}

// ExtractDeveloperReport parses the developer agent's completion report.
// If the sentinel is absent, returns a low-confidence stub (pipeline continues).
func ExtractDeveloperReport(s string) *DeveloperReport {
	const sentinel = "### Developer Completion Report"
	dr := &DeveloperReport{Confidence: "low"}
	i := strings.Index(s, sentinel)
	if i < 0 {
		return dr
	}
	body := s[i+len(sentinel):]
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := extractField(line, "**Files Changed:**"); ok {
			dr.FilesChanged = parseFileList(v)
		} else if v, ok := extractField(line, "**What Was Done:**"); ok {
			dr.WhatWasDone = v
		} else if v, ok := extractField(line, "**What Was NOT Done:**"); ok {
			dr.WhatWasNotDone = v
		} else if v, ok := extractField(line, "**Confidence:**"); ok {
			dr.Confidence = strings.TrimSpace(v)
		} else if v, ok := extractField(line, "**Suggested Validator Focus:**"); ok {
			dr.SuggestedValidatorFocus = v
		}
	}
	return dr
}

// ExtractPlanJSON delegates to ParsePlan — convenience wrapper used by the pipeline.
func ExtractPlanJSON(s string) (*Plan, error) {
	return ParsePlan(s)
}

func extractField(line, key string) (string, bool) {
	prefix := "- " + key + " "
	if strings.HasPrefix(line, prefix) {
		return strings.TrimSpace(line[len(prefix):]), true
	}
	prefix2 := "- " + key
	if strings.HasPrefix(line, prefix2) {
		return strings.TrimSpace(line[len(prefix2):]), true
	}
	return "", false
}

func parseFileList(s string) []string {
	s = strings.Trim(s, "[]")
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
