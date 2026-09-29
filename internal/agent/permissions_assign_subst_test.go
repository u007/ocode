package agent

import "testing"

// TestParseAssignmentWithCommandSubstitution: `DB="$(grep … .env | cut …)"`
// used to tokenize as an empty assignment `DB=` plus a command whose binary is
// the substitution, surfacing a bogus Ask rule `bash.prefix.$(grep …)`. The
// substitution is the assignment's value; its inner commands are still judged
// as their own fragments.
func TestParseAssignmentWithCommandSubstitution(t *testing.T) {
	cases := []struct {
		command string
		env     string
	}{
		{`DB="$(grep -E "^DATABASE_URL=" .env | cut -d= -f2-)"`, `DB=$(grep -E "^DATABASE_URL=" .env | cut -d= -f2-)`},
		{`DB=$(cat x)`, `DB=$(cat x)`},
		{`DB="$(cat x)/suffix"`, `DB=$(cat x)/suffix`},
		{"V=`cat x`", "V=$(cat x)"},
	}
	for _, tc := range cases {
		parsed, err := parseShellCommandLine(tc.command)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.command, err)
		}
		var assign *parsedShellCommand
		for i := range parsed {
			if len(parsed[i].envVars) > 0 {
				assign = &parsed[i]
			}
		}
		if assign == nil {
			t.Errorf("%q: no assignment fragment in %+v", tc.command, parsed)
			continue
		}
		if len(assign.cmdWords) != 0 || len(assign.envVars) != 1 || assign.envVars[0] != tc.env {
			t.Errorf("%q: assignment = env %q words %q, want env [%q] and no words", tc.command, assign.envVars, assign.cmdWords, tc.env)
		}
	}

	// A space after `=` makes the substitution's output the command to run —
	// that must stay an opaque command head, not be folded into the value.
	parsed, err := parseShellCommandLine(`X= $(echo rm) -rf /tmp/x`)
	if err != nil {
		t.Fatal(err)
	}
	opaque := false
	for _, c := range parsed {
		if len(c.cmdWords) > 0 && isOpaqueCommandHead(c.cmdWords) {
			opaque = true
		}
	}
	if !opaque {
		t.Errorf("`X= $(echo rm) -rf` lost its opaque command head: %+v", parsed)
	}

	// The inner commands of the substitution are still fragments.
	parsed, _ = parseShellCommandLine(`DB="$(grep -E "^DATABASE_URL=" .env | cut -d= -f2-)"; psql "$DB"`)
	var heads []string
	for _, c := range parsed {
		if len(c.cmdWords) > 0 {
			heads = append(heads, c.cmdWords[0])
		}
	}
	want := map[string]bool{"grep": true, "cut": true, "psql": true}
	if len(heads) != 3 || !want[heads[0]] || !want[heads[1]] || !want[heads[2]] {
		t.Errorf("command heads = %q, want grep, cut, psql", heads)
	}
}

// TestSandboxBareDotfileSecretAsks: isLikelyPathArg rejected a leading-dot
// name, so `cat .env` (no ./) yielded no sandbox target and auto-allowed while
// `cat ./.env` asked.
func TestSandboxBareDotfileSecretAsks(t *testing.T) {
	pm := sandboxDecideTestPM(t)
	for _, cmd := range []string{
		`cat .env`,
		`cat .env.local`,
		`grep -E "^DATABASE_URL=" .env`,
		`DB="$(grep -E "^DATABASE_URL=" .env | cut -d= -f2-)"; psql "$DB" -c "select 1"`,
		`psql "$(cat .env)"`,
		`cat .netrc`,
	} {
		if dec := decideBash(t, pm, cmd); dec.Level != PermissionAsk {
			t.Errorf("sandbox %q = %s, want Ask (secret material read)", cmd, dec.Level)
		}
	}
	for _, cmd := range []string{`cat .env.example`, `ls .`, `cat .gitignore`} {
		if dec := decideBash(t, pm, cmd); dec.Level != PermissionAllow {
			t.Errorf("sandbox %q = %s, want Allow", cmd, dec.Level)
		}
	}
}
