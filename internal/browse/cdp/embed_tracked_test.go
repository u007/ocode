package cdp

import (
	"os/exec"
	"strings"
	"testing"
)

// TestEmbeddedBundleIsTrackedByGit pins the repo rule that a //go:embed input
// must be committed (or generated behind a build tag, like the htr-assets
// archive). pagedom.go embeds bundle/htrcli-dom.js and bundle/formkit.js; when
// either is untracked the author's tree builds while a fresh clone fails with
// "pattern bundle/htrcli-dom.js: no matching files found", which only shows up
// in CI or in someone else's worktree.
func TestEmbeddedBundleIsTrackedByGit(t *testing.T) {
	if err := exec.Command("git", "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		t.Skip("not a git checkout")
	}
	for _, path := range []string{"bundle/htrcli-dom.js", "bundle/formkit.js"} {
		out, err := exec.Command("git", "ls-files", "--error-unmatch", path).CombinedOutput()
		if err != nil {
			t.Fatalf("%s is not tracked by git (%v): %s\n"+
				"pagedom.go embeds it, so an untracked file builds here but breaks every fresh clone. "+
				"Stage it (git add internal/browse/cdp/bundle) or move it behind a build tag with a stub.",
				path, err, strings.TrimSpace(string(out)))
		}
	}
}
