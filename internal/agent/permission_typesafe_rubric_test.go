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

// A module import is not an effect. The five facts describe what a script
// DOES, but the network keyword list (socket, urllib, requests, http, fetch,
// bind, listen) read as a token match, so a bare `import playwright` scored
// allow at 0.34-0.40 with concern "none" — below the 0.80 floor, deferred —
// while `import socket` and `import requests` were named `network` outright.
// Logged 2026-10-28 11:46: confidence 0.58 on
// `python3 -c "import playwright, sys; print(playwright.__version__ …)"`.
// The carve-out below is what makes the shape decidable; pin it so an edit
// cannot silently drop it.
//
// Each assertion pins a DISTINCTIVE fragment: pinning only "import" would still
// pass with either half of the rule removed.
func TestTypesafeJudgeInstructionsCarveOutBareImports(t *testing.T) {
	for _, frag := range []string{
		// The rule itself.
		"An import statement is NEVER itself one of the five facts",
		// Both effects it settles must be tied to a CALL, not a module name.
		"The network answer is yes only when the script CALLS",
		"subprocess answer is yes only on a call",
		// The reported shape, spelled out.
		"print(playwright.__version__)",
		// The counter-instruction that removed the residual hesitation.
		"Do not name a concern for a module name alone",
		// A bare dependency probe (`python3 -c "import x"`) does nothing at all.
		"A script whose ENTIRE body is import statements performs no effect at all",
	} {
		if !strings.Contains(typesafeJudgeInstructions, frag) {
			t.Errorf("rubric lost bare-import carve-out fragment %q:\n%s", frag, typesafeJudgeInstructions)
		}
	}
}

// The concern is where a CALL goes, never which library a script imports — and
// the project-local case is covered by shipping the module's own source rather
// than by treating the import as suspicious. Both halves matter: without the
// first the import name decides the verdict again; without the second a local
// module becomes a way to hide a script behind `import mymod`.
func TestTypesafeJudgeInstructionsJudgeImportDestinationsNotNames(t *testing.T) {
	for _, frag := range []string{
		// The principle, stated so a module name cannot drive the verdict.
		"A network-flavoured module name is not a network effect",
		"importing a module that provides subprocesses is not a subprocess",
		"Judge where a CALL goes, never which library is loaded",
		// The mechanism that keeps the project-local case safe: ship the source.
		"that file's source is in executed_scripts",
		"a project-local module that reads a credential or posts it off-host is still denied",
	} {
		if !strings.Contains(typesafeJudgeInstructions, frag) {
			t.Errorf("rubric lost import-destination fragment %q:\n%s", frag, typesafeJudgeInstructions)
		}
	}
}

// The carve-out must live in the verdict rubric, not only the concern question:
// the verdict answer is the one gated by the confidence floor, so a clause added
// solely to typesafeConcernInstructions would not move a verdict.
func TestTypesafeImportCarveOutIsInVerdictRubric(t *testing.T) {
	if !strings.Contains(typesafeJudgeInstructions, "An import statement is NEVER itself one of the five facts") {
		t.Fatal("bare-import carve-out is missing from the verdict rubric (typesafeJudgeInstructions)")
	}
	if strings.Contains(typesafeJudgeInstructions, "`") {
		t.Fatal("verdict rubric contains a backtick; it is a Go raw string literal and an unescaped ` would terminate it")
	}
}

// Loopback is local development traffic, and the destination must be PROVEN:
// a literal host, or a URL built only from literals in the same source. The
// measured failure this pins (2026-10-28): the five-facts bullet listed
// `network (socket, urllib, requests, …)` with no local exception while a later
// bullet allowed loopback, and Jev answered allow at 0.02-0.34 confidence on
// `requests.get("http://127.0.0.1:8080/health")` — a contradiction it could not
// resolve. Folding the exception INTO the fact lifted the same call to 0.88-0.92,
// and the workspace-variable form to 0.92-0.94, while an env-var destination
// stayed deny 3/3.
//
// Each fragment is unique in the rubric (grep -c -F => 1), so dropping the rule
// fails this test rather than passing on a partial match.
func TestTypesafeJudgeInstructionsProveLoopbackBeforeAllowing(t *testing.T) {
	for _, frag := range []string{
		// The loopback rule itself.
		"A call to LOOPBACK is local development traffic, not egress",
		// The exception must live inside the five-facts definition too, or the
		// judge sees a contradiction and splits its confidence.
		"network reaching a destination OFF THIS HOST",
		"stays on this host, so it is not this fact",
		// Leaving the machine is not loopback, even on a private range.
		"A private or LAN address is NOT loopback",
		// The proof requirement: visible literal, never an opaque variable.
		"A destination is proven local only when you can see it as a literal",
		"A host read from the environment, argv, a file, another file's data or a computed/interpolated string is NOT proven",
		// The authority-smuggling case: userinfo does not make a URL local.
		"userinfo before @ does not change the host",
		// A secret sent to loopback is still `secrets`, but the user's toggle alone
		// decides it. That exception lives INSIDE the fact (and inside the
		// switched-off note), because a separate line contradicting the base rule is
		// what produced a 0.70-0.76 split in the first place.
		"A credential value in the query, header or body is the secrets concern even when the destination is loopback",
		"unless the secrets category is switched off (then it is ordinary data)",
		"this one is also NOT a fact when the secrets category is switched off",
	} {
		if !strings.Contains(typesafeJudgeInstructions, frag) {
			t.Errorf("rubric lost loopback fragment %q:\n%s", frag, typesafeJudgeInstructions)
		}
	}
}
