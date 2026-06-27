package orchestrator

import (
	"strconv"
	"strings"
)

// Plan is the task contract produced by the planner agent.
// Text holds the full natural-language plan; VerifyMode and MaxIterations
// are extracted from keyword lines and fall back to safe defaults.
type Plan struct {
	Text          string // full natural-language plan from the planner
	VerifyMode    string // "llm_only" | "build_llm" | "build_test_llm"
	MaxIterations int
}

var validVerifyModes = map[string]bool{
	"llm_only":       true,
	"build_llm":      true,
	"build_test_llm": true,
}

// ParsePlan accepts any non-empty planner output (natural language).
// It scans for two optional keyword lines:
//
//	verify_mode: build_test_llm
//	max_iterations: 4
//
// Unrecognised or absent values fall back to "build_test_llm" / 4.
// ParsePlan never returns an error — any text is a valid plan.
func ParsePlan(s string) *Plan {
	p := &Plan{
		Text:          strings.TrimSpace(s),
		VerifyMode:    "build_test_llm",
		MaxIterations: 4,
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := extractKeywordValue(line, "verify_mode"); ok {
			if validVerifyModes[v] {
				p.VerifyMode = v
			}
		}
		if v, ok := extractKeywordValue(line, "max_iterations"); ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				p.MaxIterations = n
			}
		}
	}
	return p
}

// extractKeywordValue matches "key: value" and "**key:** value" patterns.
// More specific pattern is tried first to avoid partial matches.
func extractKeywordValue(line, key string) (string, bool) {
	for _, prefix := range []string{"**" + key + ":**", key + ":"} {
		if idx := strings.Index(strings.ToLower(line), prefix); idx >= 0 {
			val := strings.TrimSpace(line[idx+len(prefix):])
			if val != "" {
				return val, true
			}
		}
	}
	return "", false
}
