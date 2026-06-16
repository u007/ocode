package tui

import (
	"strings"
	"testing"
)

// TestReviewPrompt_DrivesGroupedFanout confirms the /review
// prompt instructs the orchestrator to: (a) compute a shared
// brief (change set + caller map + doc-rule digest) once,
// (b) spawn a grouped fan-out with shared_notes:true
// partitioned by dimension, (c) run reconcile at the end.
//
// This is the prompt-level rewrite that the Part 04 plan
// calls out. The orchestration is done by the LLM (the
// orchestrator) reading the prompt, not by the command code
// spawning things directly. The command code's job is to put
// the right instructions in front of the LLM.
func TestReviewPrompt_DrivesGroupedFanout(t *testing.T) {
	prompt := buildReviewPrompt(reviewTargetWorkingDir, "fake diff", "uncommitted changes")
	if !strings.Contains(prompt, "shared_notes") {
		t.Errorf("/review prompt missing shared_notes instruction:\n%s", prompt)
	}
	if !strings.Contains(prompt, "brief") && !strings.Contains(prompt, "BRIEF") {
		t.Errorf("/review prompt missing brief instruction:\n%s", prompt)
	}
	// The plan says the orchestrator must run reconcile at
	// the end. The prompt must say so explicitly.
	if !strings.Contains(prompt, "reconcile") && !strings.Contains(prompt, "Reconcile") {
		t.Errorf("/review prompt missing reconcile instruction:\n%s", prompt)
	}
}

// TestReviewPrompt_PreservesReportFormat confirms the
// existing SEVERITY/FILE/LINE/MESSAGE/SUGGESTION report
// format is preserved. The plan explicitly says: "Preserve
// the existing report format and interactive-resolution
// section."
func TestReviewPrompt_PreservesReportFormat(t *testing.T) {
	prompt := buildReviewPrompt(reviewTargetWorkingDir, "fake diff", "uncommitted changes")
	for _, marker := range []string{"SEVERITY:", "FILE:", "LINE:", "MESSAGE:", "SUGGESTION:"} {
		if !strings.Contains(prompt, marker) {
			t.Errorf("/review prompt missing report format marker %q:\n%s", marker, prompt)
		}
	}
}

// TestReviewPrompt_PartitionsByDimension confirms the prompt
// tells the orchestrator to partition by dimension (e.g.
// correctness / security / style) — not by file. The plan
// says: "spawn a grouped fan-out with shared_notes:true
// partitioned by dimension (or by file for large diffs)."
// For small diffs (the default working-dir case), dimension
// partitioning is the default.
func TestReviewPrompt_PartitionsByDimension(t *testing.T) {
	prompt := buildReviewPrompt(reviewTargetWorkingDir, "fake diff", "uncommitted changes")
	// Look for the word "dimension" or a list of partition
	// types. The plan lists correctness / security / style
	// as the canonical dimensions, but the test does not
	// pin the exact wording.
	if !strings.Contains(prompt, "dimension") &&
		!strings.Contains(prompt, "Dimension") &&
		!strings.Contains(prompt, "correctness") {
		t.Errorf("/review prompt missing dimension-partition instruction:\n%s", prompt)
	}
}

// TestReviewPrompt_VerifyEscalation: the reconcile
// instructions tell the orchestrator to spawn a focused
// verify agent for contradictions it cannot settle from
// notes alone. The plan calls this out as a non-negotiable
// for the design (a false negative from a weak model must
// not be the final word).
func TestReviewPrompt_VerifyEscalation(t *testing.T) {
	prompt := buildReviewPrompt(reviewTargetWorkingDir, "fake diff", "uncommitted changes")
	if !strings.Contains(prompt, "verify") && !strings.Contains(prompt, "Verify") {
		t.Errorf("/review prompt missing verify-agent escalation:\n%s", prompt)
	}
}

// TestReviewPrompt_NoLongerSingleAgent is the negative
// check: the prompt no longer says "you review this diff
// end-to-end." The plan says: "The skill no longer tells
// one agent to do all phases serially."
func TestReviewPrompt_NoLongerSingleAgent(t *testing.T) {
	prompt := buildReviewPrompt(reviewTargetWorkingDir, "fake diff", "uncommitted changes")
	// Loose check: the prompt must not say "review this
	// yourself" or "do a full review". A grouped-fanout
	// prompt would say "spawn" / "fan out" / "parallel".
	if !strings.Contains(prompt, "spawn") &&
		!strings.Contains(prompt, "fan") &&
		!strings.Contains(prompt, "parallel") {
		t.Errorf("/review prompt no longer drives a grouped fan-out:\n%s", prompt)
	}
}
