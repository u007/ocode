package agent

import (
	"strings"
	"testing"
)

func sandboxHeredocDecide(t *testing.T, command string) PermissionDecision {
	t.Helper()
	return decideBash(t, sandboxDecideTestPM(t), command)
}

// A quoted heredoc fed to a non-shell program is inert stdin: markdown
// backticks, `$x` and words like "git reset" in the body are data, so the
// sandbox gate must not ask about them.
func TestSandboxQuotedHeredocBodyIsNotParsedAsCommands(t *testing.T) {
	command := "cd sub && python3 - <<'PY'\n" +
		"done = (\n" +
		"    \"`loadProgramme` (`pm.export.ts`) through a `leftJoin`\"\n" +
		"    \"$HOME is not expanded; git reset --hard is just text\"\n" +
		")\n" +
		"print(done)\n" +
		"PY"
	if dec := sandboxHeredocDecide(t, command); dec.Level != PermissionAllow {
		t.Fatalf("level=%s rule=%v, want allow", dec.Level, dec.Request)
	}
}

// The body stays under the gates whenever the shell would run or expand it,
// and commands after the terminator are always checked.
func TestSandboxHeredocBodyStillGatedWhenItIsShellCode(t *testing.T) {
	cases := map[string]string{
		"unquoted heredoc expands":  "python3 - <<PY\n`git stash`\nPY",
		"bash consumes body":        "bash <<'EOF'\ngit stash\nEOF",
		"piped into sh":             "cat <<'EOF' | sh\ngit reset --hard\nEOF",
		"wrapped shell":             "env X=1 bash -s <<'EOF'\ngit stash\nEOF",
		"command after terminator":  "python3 - <<'PY'\nprint(1)\nPY\ngit stash",
		"operator inside a quote":   "echo \"<<'X'\"\ngit stash\nX",
		"operator inside a comment": "true # <<'X'\ngit stash\nX",
		"unterminated heredoc":      "python3 - <<'PY'\ngit stash",
		"two heredocs on one line":  "cat <<'A' <<'B'\ngit stash\nA\nx\nB",
	}
	for name, command := range cases {
		t.Run(name, func(t *testing.T) {
			if dec := sandboxHeredocDecide(t, command); dec.Level == PermissionAllow {
				t.Fatalf("Decide(%q) = allow, want the body/tail gated", command)
			}
		})
	}
}

func TestSandboxGateParseTargetKeepsLinesAfterTheTerminator(t *testing.T) {
	got := sandboxGateParseTarget("python3 - <<'PY'\nprint(1)\nPY\necho done")
	if strings.Contains(got, "print(1)") || !strings.Contains(got, "echo done") {
		t.Fatalf("target=%q, want body removed and tail kept", got)
	}
}
