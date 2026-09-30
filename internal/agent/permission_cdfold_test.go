package agent

import (
	"strings"
	"testing"
)

// scopeOf builds an inScope predicate over a fixed set of root prefixes.
func scopeOf(roots ...string) func(string) bool {
	return func(p string) bool {
		for _, r := range roots {
			if p == r || strings.HasPrefix(p, r+"/") {
				return true
			}
		}
		return false
	}
}

func TestFoldTopLevelCds_FoldsSimpleLeadingCd(t *testing.T) {
	in := scopeOf("/Users/james/www")
	folded, cwd, ok := foldTopLevelCds("cd /Users/james/www/kakiit && ls drizzle", "/Users/james/www/ocode", in)
	if !ok {
		t.Fatal("expected fold to succeed for a simple literal in-scope cd")
	}
	if cwd != "/Users/james/www/kakiit" {
		t.Errorf("cwd = %q, want /Users/james/www/kakiit", cwd)
	}
	if strings.Contains(folded, "cd ") {
		t.Errorf("folded command still contains a cd: %q", folded)
	}
	if !strings.Contains(folded, "ls drizzle") {
		t.Errorf("folded command lost its payload: %q", folded)
	}
}

func TestFoldTopLevelCds_ChainedCdsFoldSequentially(t *testing.T) {
	in := scopeOf("/Users/james/www")
	folded, cwd, ok := foldTopLevelCds(
		"cd /Users/james/www/kakiit && cd /Users/james/www/kakiit/drizzle && ls *.sql",
		"/Users/james/www/ocode", in)
	if !ok {
		t.Fatal("expected chained in-scope cds to fold")
	}
	if cwd != "/Users/james/www/kakiit/drizzle" {
		t.Errorf("cwd = %q, want the SECOND cd target", cwd)
	}
	if strings.Contains(folded, "cd ") {
		t.Errorf("folded command still contains a cd: %q", folded)
	}
}

// TestFoldTopLevelCds_RefusesAmbiguous is the safety net: every shape that could
// change the meaning of a cd must be left exactly as it was, so the judge keeps
// seeing it and the call defers to a human instead of being auto-granted.
func TestFoldTopLevelCds_RefusesAmbiguous(t *testing.T) {
	in := scopeOf("/Users/james/www", "/tmp")
	cases := []struct{ name, cmd string }{
		{"cd -", "cd - && ls"},
		{"cd with no arg", "cd && ls"},
		{"cd $VAR", "cd $DIR && ls"},
		{"cd with tilde", "cd ~/www && ls"},
		{"cd with glob", "cd /Users/james/www/* && ls"},
		{"cd with command substitution", "cd $(pwd) && ls"},
		{"cd with backticks", "cd `pwd` && ls"},
		{"out of scope target", "cd /etc && ls"},
		{"escaping relative target", "cd ../../../../etc && ls"},
		{"inside subshell", "(cd /Users/james/www/kakiit && ls)"},
		{"after a pipe", "echo hi | cd /Users/james/www/kakiit"},
		{"after ||", "false || cd /Users/james/www/kakiit"},
		{"cd whose own terminator is ||", "cd /Users/james/www/kakiit || echo failed"},
		{"cd after a loop", "for d in a b; do echo $d; done && cd /Users/james/www/kakiit && ls"},
		{"inside for loop", "for d in a b; do cd $d; done"},
		{"inside if", "if true; then cd /Users/james/www/kakiit; fi"},
		{"multi-line", "cd /Users/james/www/kakiit\nls"},
		{"quoted target", `cd "/Users/james/www/kakiit" && ls`},
		{"cd only, nothing to keep", "cd /Users/james/www/kakiit"},
		{"no cd at all", "ls drizzle | wc -l"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			folded, cwd, ok := foldTopLevelCds(tc.cmd, "/Users/james/www/ocode", in)
			if ok {
				t.Errorf("fold unexpectedly succeeded: %q -> %q (cwd %q)", tc.cmd, folded, cwd)
			}
			if folded != tc.cmd {
				t.Errorf("command was modified on a refused fold:\n got %q\nwant %q", folded, tc.cmd)
			}
			if cwd != "/Users/james/www/ocode" {
				t.Errorf("cwd = %q, want the original workdir on a refused fold", cwd)
			}
		})
	}
}

// TestFoldTopLevelCds_NeverWidens asserts the direction that matters. Folding
// turns an ask into an allow, so it must only ever remove a cd that Go itself
// proved in-scope. Anything dynamic, out-of-scope or structurally nested must be
// refused, leaving the judge's view — and therefore the human's decision — intact.
func TestFoldTopLevelCds_NeverWidens(t *testing.T) {
	in := scopeOf("/Users/james/www")
	for _, cmd := range []string{
		"cd /etc && rm -rf /tmp/x",
		"cd $HOME && rm -rf /tmp/x",
		"cd - && rm -rf /tmp/x",
		"false || cd /Users/james/www/kakiit && rm -rf /tmp/x",
		"echo hi | cd /Users/james/www/kakiit && rm -rf /tmp/x",
		"(cd /Users/james/www/kakiit && rm -rf /tmp/x)",
	} {
		if _, _, ok := foldTopLevelCds(cmd, "/Users/james/www/ocode", in); ok {
			t.Errorf("fold accepted a command it must refuse: %q", cmd)
		}
	}
}

// TestFoldTopLevelCds_RealReportedCommand pins the exact shape that produced
// allow@0.06 in production, so a regression in statement splitting is caught
// against a known-bad input rather than a synthetic one. It must FOLD: the
// trailing for-loop and the $(…) substitutions must not defeat the fold, because
// the cd is an unconditional top-level statement.
func TestFoldTopLevelCds_RealReportedCommand(t *testing.T) {
	const cmd = `cd /Users/james/www/kakiit && total=$(ls drizzle/*.sql | wc -l | tr -d ' '); ` +
		`withmarker=$(grep -l "statement-breakpoint" drizzle/*.sql | wc -l | tr -d ' '); ` +
		`echo "total migrations: $total | with --> statement-breakpoint: $withmarker"; ` +
		`echo "--- WITHOUT the marker: ---"; for f in drizzle/*.sql; do ` +
		`grep -q "statement-breakpoint" "$f" || echo " $(basename $f)"; done`

	folded, cwd, ok := foldTopLevelCds(cmd, "/Users/james/www/ocode", scopeOf("/Users/james/www"))
	if !ok {
		t.Fatalf("the real reported command must fold; got %q", folded)
	}
	if cwd != "/Users/james/www/kakiit" {
		t.Errorf("cwd = %q, want /Users/james/www/kakiit", cwd)
	}
	if strings.Contains(folded, "cd ") {
		t.Errorf("folded command still contains the cd: %q", folded)
	}
	for _, want := range []string{"total=$(ls drizzle/*.sql", "withmarker=$(grep -l",
		"for f in drizzle/*.sql", "done"} {
		if !strings.Contains(folded, want) {
			t.Errorf("folded command lost %q:\n%q", want, folded)
		}
	}
}
