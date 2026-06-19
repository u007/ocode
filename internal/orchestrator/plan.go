package orchestrator

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Plan is the immutable task contract produced by the planner agent.
// It is parsed once at the start of Planning and never mutated.
type Plan struct {
	Intent          string   `json:"intent"` // "feature" | "bugfix"
	Goal            string   `json:"goal"`
	SuccessCriteria []string `json:"success_criteria"`
	VerifyMode      string   `json:"verify_mode"` // "llm_only" | "build_llm" | "build_test_llm"
	MaxIterations   int      `json:"max_iterations"`
}

var validIntents = map[string]bool{"feature": true, "bugfix": true}
var validVerifyModes = map[string]bool{
	"llm_only":       true,
	"build_llm":      true,
	"build_test_llm": true,
}

// ParsePlan extracts the first JSON object from s and validates it as a Plan.
// Planner LLM output may wrap JSON in prose — we tolerate that.
func ParsePlan(s string) (*Plan, error) {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in planner output")
	}
	var p Plan
	if err := json.Unmarshal([]byte(s[start:end+1]), &p); err != nil {
		return nil, fmt.Errorf("plan JSON parse error: %w", err)
	}
	if p.Goal == "" {
		return nil, fmt.Errorf("plan missing required field: goal")
	}
	if p.Intent == "" {
		return nil, fmt.Errorf("plan missing required field: intent")
	}
	if !validIntents[p.Intent] {
		return nil, fmt.Errorf("plan intent %q must be one of: feature, bugfix", p.Intent)
	}
	if p.VerifyMode == "" {
		return nil, fmt.Errorf("plan missing required field: verify_mode")
	}
	if !validVerifyModes[p.VerifyMode] {
		return nil, fmt.Errorf("plan verify_mode %q must be one of: llm_only, build_llm, build_test_llm", p.VerifyMode)
	}
	if len(p.SuccessCriteria) == 0 {
		return nil, fmt.Errorf("plan missing required field: success_criteria (must have at least one)")
	}
	if p.MaxIterations <= 0 {
		p.MaxIterations = 4
	}
	return &p, nil
}
