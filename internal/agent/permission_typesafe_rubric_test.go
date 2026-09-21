package agent

import (
	"strings"
	"testing"
)

// The TypeSafe/Jev rubric must not treat reading a credential file as a deny
// reason by itself. Jev's confidence is distribution-shaped, so a rubric that
// conflates "reads .env" with "exfiltrates secrets" makes it hesitate below the
// confidence floor on ordinary tooling (e.g. psql driven from $(grep … .env …)),
// producing a spurious "Auto-denied by LLM permission model" banner. The
// carve-out below is the rule that removes that hesitation; pin it so an edit
// cannot silently drop it.
func TestTypesafeJudgeInstructionsCarveOutLocalSecretReads(t *testing.T) {
	if !strings.Contains(typesafeJudgeInstructions, "is NOT by itself a reason to deny or to hesitate") {
		t.Fatalf("rubric lost the local-secret-read carve-out:\n%s", typesafeJudgeInstructions)
	}
	if !strings.Contains(typesafeJudgeInstructions, "printed to the command's output") {
		t.Fatalf("rubric lost the exposure criterion (what makes a secret read a concern)")
	}
	// The carve-out must stay an ALLOW rule for the literal local-use shape.
	if !strings.Contains(typesafeJudgeInstructions, "psql") {
		t.Fatalf("rubric lost the worked local-consumer example")
	}
}

// The `secrets` concern category must describe exposure, not mere reading —
// its label is shown verbatim in the auto-deny reason.
func TestTypesafeSecretsConcernIsAboutExposure(t *testing.T) {
	var label string
	for _, c := range typesafeConcerns {
		if c.Key == "secrets" {
			label = c.Label
		}
	}
	if label == "" {
		t.Fatal("secrets concern missing from typesafeConcerns")
	}
	if strings.Contains(label, "reads or exfiltrates") {
		t.Fatalf("secrets concern still conflates reading with exfiltration: %q", label)
	}
	if !strings.Contains(label, "reading one locally without exposing the value is not this concern") {
		t.Fatalf("secrets concern should exempt non-exposing local reads: %q", label)
	}
}
