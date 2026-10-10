package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// resolvedDir returns dir with symlinks resolved, so comparisons against
// `pwd -P` output hold on macOS (where /tmp is a symlink to /private/tmp).
func resolvedDir(t *testing.T, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", dir, err)
	}
	return real
}

// TestBashTool_CwdDoesNotPersistAcrossInvocations pins the invariant that makes
// every `cd` inside a bash tool call one-shot: the bash tool spawns a fresh
// process per invocation with cmd.Dir = the session workDir and keeps no shell
// state between calls.
//
// Why this is load-bearing rather than trivia: the bundled
// using-git-worktrees skill does exactly
//
//	git worktree add "$path" -b "$BRANCH_NAME"
//	cd "$path"
//
// and then assumes it is working inside the worktree. Under ocode every
// subsequent command silently runs in the MAIN checkout instead — and because
// the worktree lives *inside* workDir, no permission check objects, so the
// whole session keeps building the wrong tree with no error anywhere. A
// worktree-aware `cd` therefore has to be a real Agent-level feature (a
// separate mutable activeCwd, not a reuse of Agent.SetWorkDir, which
// re-derives paths.ProjectSlug via projectSnapshotsDir and would relocate the
// snapshot store), never an expectation about the shell.
//
// If this test ever starts failing because a persistent cwd was introduced,
// that is a deliberate architecture change — update the worktree skill and the
// activeCwd design in the same change, because both currently depend on `cd`
// being one-shot.
func TestBashTool_CwdDoesNotPersistAcrossInvocations(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX cd/pwd and bash -c")
	}
	root := resolvedDir(t, t.TempDir())
	sub := filepath.Join(root, "worktree")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("Mkdir(%q): %v", sub, err)
	}

	bt := BashTool{Procs: NewProcessRegistry()}
	run := func(command string) string {
		t.Helper()
		ctx := WithWorkDir(context.Background(), root)
		args, _ := json.Marshal(map[string]interface{}{"command": command})
		out, err := bt.ExecuteCtx(ctx, args)
		if err != nil {
			t.Fatalf("ExecuteCtx(%q) returned error: %v", command, err)
		}
		return strings.TrimSpace(out)
	}

	// Step 1 (control): the workDir alone determines the cwd. This must NOT use
	// `cd`, because `cd <absolute>` succeeds even when cmd.Dir is ignored — a
	// cd-based control would pass against a build that drops the workDir
	// entirely, leaving the real assertion below vacuous.
	if got := run("pwd -P"); got != root {
		t.Fatalf("WithWorkDir was not honored: bare pwd -P = %q, want workDir %q", got, root)
	}

	// Step 2 (control): `cd` works within a single invocation.
	if got := run("cd " + sub + " && pwd -P"); got != sub {
		t.Fatalf("cd within one invocation did not take effect: cd %s && pwd -P = %q, want %q", sub, got, sub)
	}

	// Step 3 (the invariant): a separate invocation starts back at workDir, not
	// at wherever the previous one cd'd to.
	if got := run("pwd -P"); got != root {
		t.Fatalf("bash cwd persisted across invocations: second pwd -P = %q, want workDir %q "+
			"(a persistent cwd is a deliberate architecture change — see the test doc comment)", got, root)
	}
}

// TestBashTool_RelativePathsAnchorOnWorkDir pins the other half of the
// worktree story: a relative path in a later call resolves against the session
// workDir, never against a directory an earlier call cd'd into. This is why an
// agent that "moved itself" into a worktree keeps writing to the main
// checkout's files.
func TestBashTool_RelativePathsAnchorOnWorkDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX cd/pwd and bash -c")
	}
	root := resolvedDir(t, t.TempDir())
	sub := filepath.Join(root, "worktree")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("Mkdir(%q): %v", sub, err)
	}

	bt := BashTool{Procs: NewProcessRegistry()}
	run := func(command string) string {
		t.Helper()
		ctx := WithWorkDir(context.Background(), root)
		args, _ := json.Marshal(map[string]interface{}{"command": command})
		out, err := bt.ExecuteCtx(ctx, args)
		if err != nil {
			t.Fatalf("ExecuteCtx(%q) returned error: %v", command, err)
		}
		return strings.TrimSpace(out)
	}

	// The write calls assert on file contents below, not on the tool's stdout:
	// the bash tool renders a successful no-output command as a success
	// sentinel string, so an "expected empty output" assertion would fail for
	// a reason unrelated to cwd.
	run("cd " + sub + " && printf x > marker.txt")

	// After the previous call cd'd into the worktree and created marker.txt
	// there, a fresh relative write must still land under workDir (the main
	// checkout) — proving the worktree copy is NOT what a later relative write
	// reaches.
	run("printf y > marker.txt")

	atRoot, err := os.ReadFile(filepath.Join(root, "marker.txt"))
	if err != nil {
		t.Fatalf("marker.txt not created under workDir: %v", err)
	}
	if string(atRoot) != "y" {
		t.Fatalf("workDir marker.txt = %q, want %q", atRoot, "y")
	}

	// Sanity: the worktree copy is the one the cd'd call wrote, and it is
	// untouched by the later relative write.
	inSub, err := os.ReadFile(filepath.Join(sub, "marker.txt"))
	if err != nil {
		t.Fatalf("marker.txt not created in the cd'd directory: %v", err)
	}
	if string(inSub) != "x" {
		t.Fatalf("worktree marker.txt = %q, want %q", inSub, "x")
	}
}
