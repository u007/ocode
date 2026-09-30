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

// The `secrets` carve-out covers credential FILES but said nothing about
// enumerating the environment, so `env | grep -i TOKEN | sed 's/=.*/=<set>/'`
// had no rule pointing at the redaction. Observed in the wild: Jev leaned allow
// at 0.72 against the 0.85 floor on a `cd … && grep -ri … ; env | grep -i
// typesafe | sed 's/=.*/=<set>/'` call, and the deferral carried no concern at
// all because the judge also answered "none". The clause below is what makes
// the shape decidable; pin it so an edit cannot silently drop it.
//
// Each assertion pins a DISTINCTIVE fragment. Asserting only the first sentence
// would still pass if someone trimmed the allowed-forms list back out, which is
// the half of the rule that actually did the work in the reported case.
func TestTypesafeJudgeInstructionsCarveOutEnvironmentNameListing(t *testing.T) {
	for _, frag := range []string{
		// The headline rule: values decide, not the existence of a variable.
		"never the existence of a variable",
		// The reported shape, spelled out.
		"env | sed 's/=.*/=<set>/'",
		"env | grep -i TOKEN | sed 's/=.*/=/'",
		// WHY it is safe — without this the judge has to re-derive pipeline order.
		"sed rewrites every line before anything is displayed",
		// The counter-case, so the rule cannot be read as "env is always fine".
		"A bare env, printenv or set with no filter",
	} {
		if !strings.Contains(typesafeJudgeInstructions, frag) {
			t.Errorf("rubric lost environment-enumeration clause fragment %q:\n%s", frag, typesafeJudgeInstructions)
		}
	}
}

// The carve-out must live in the shared rubric, not only the concern question:
// the verdict question is the one whose answer is gated by the floor, so a
// clause added solely to typesafeConcernInstructions would not move a verdict.
func TestTypesafeEnvironmentCarveOutIsInVerdictRubric(t *testing.T) {
	if !strings.Contains(typesafeJudgeInstructions, "Enumerating the environment") {
		t.Fatal("environment carve-out is missing from the verdict rubric (typesafeJudgeInstructions)")
	}
	if strings.Contains(typesafeJudgeInstructions, "`") {
		t.Fatal("verdict rubric contains a backtick; it is a Go raw string literal and an unescaped ` would terminate it")
	}
}
