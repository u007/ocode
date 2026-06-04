package commands

import (
	"os"
	"strings"
	"testing"
)

// TestParseGitCommitPushFull is a one-shot check that the 50-line cap removal
// in parseCommandFile didn't truncate /git-commit-push.md (the file grew past
// 50 lines after frontmatter was added, and the cap was a latent bug).
// It walks the same search paths the runtime loader uses, so it would
// silently regress if the cap came back or if the frontmatter stopped being
// parsed.
func TestParseGitCommitPushFull(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	if _, err := os.Stat(home + "/.config/opencode/commands/git-commit-push.md"); err != nil {
		t.Skipf("git-commit-push.md not present at %s: %v", home+"/.config/opencode/commands/git-commit-push.md", err)
	}

	cmd, err := LoadCommand("git-commit-push", nil)
	if err != nil {
		t.Fatalf("LoadCommand: %v", err)
	}
	if cmd == nil {
		t.Fatal("LoadCommand returned nil")
	}

	if cmd.Description == "" {
		t.Error("Description is empty — frontmatter description: not picked up")
	}
	if !strings.Contains(strings.ToLower(cmd.Description), "doc") {
		t.Errorf("Description %q does not mention 'doc' — likely a stale parse", cmd.Description)
	}
	if len(cmd.Prompt) < 1500 {
		t.Errorf("Prompt length = %d; expected the full body (>1500 chars). The 50-line cap may have been reintroduced.", len(cmd.Prompt))
	}
	for _, must := range []string{
		"## Step 4: Stage updated docs",
		"## Step 5: Commit and push",
		"Report the commit hash and the branch pushed.",
	} {
		if !strings.Contains(cmd.Prompt, must) {
			t.Errorf("prompt is missing %q — body was truncated", must)
		}
	}
	t.Logf("description=%q prompt_len=%d", cmd.Description, len(cmd.Prompt))
}
