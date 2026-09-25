package server

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/gitexec"
)

// --- helpers ---

// gitCombined runs a command and returns its combined output plus the error,
// for cases where failure is the expected outcome (e.g. a conflicting merge).
// The caller decides whether a failure is acceptable; the project rule is that
// an unexpected result must fail the test loudly rather than be tolerated.
func gitCombined(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	// LC_ALL=C so callers can match git's English diagnostics ("CONFLICT",
	// "does not have our version"); a translated git would break those.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// mkState builds the state dump a git repository would report: every name in
// files is present as a text file with that value, and every name in dirs is
// present as a directory.
func mkState(files map[string]string, dirs ...string) []gitStateEntry {
	entries := make([]gitStateEntry, 0, len(files)+len(dirs))
	for _, name := range dirs {
		entries = append(entries, gitStateEntry{Name: name, IsDir: true})
	}
	for name, value := range files {
		entries = append(entries, gitStateEntry{Name: name, Value: value})
	}
	return entries
}

// gitConflictedMerge creates a repository whose merge stops on a content
// conflict in the named file. The file name is a parameter so a test can build
// the conflict on an awkward path (spaces, pathspec-magic-shaped) from the
// base commit onward. Renaming a path that is ALREADY conflicted is not
// possible — git refuses with "conflicted, source=..., destination=..." — so
// the name has to be chosen before the branches diverge.
func gitConflictedMerge(t *testing.T) string {
	t.Helper()
	return gitConflictedMergeFile(t, "f.txt")
}

func gitConflictedMergeFile(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	initGitRepo(t, dir)
	path := filepath.Join(dir, name)
	writeFile(t, path, "a\nb\nc\n")
	// GIT_LITERAL_PATHSPECS: a name like ":(top)f.txt" is otherwise read as
	// pathspec magic and resolves to a different path. `--` is not sufficient
	// for pathspec magic; only the literal setting is.
	runEnv(t, dir, []string{"GIT_LITERAL_PATHSPECS=1"}, "git", "add", "--", name)
	run(t, dir, "git", "commit", "-m", "base")
	run(t, dir, "git", "switch", "-c", "other")
	writeFile(t, path, "a\nTHEIRS\nc\n")
	run(t, dir, "git", "commit", "-am", "theirs")
	run(t, dir, "git", "switch", "-")
	writeFile(t, path, "a\nOURS\nc\n")
	run(t, dir, "git", "commit", "-am", "ours")
	out, err := gitCombined(t, dir, "git", "merge", "other")
	if err == nil {
		t.Fatalf("expected the merge to conflict, but it succeeded:\n%s", out)
	}
	if !strings.Contains(out, "CONFLICT") {
		t.Fatalf("expected git to report CONFLICT, got:\n%s", out)
	}
	return dir
}

// --- synthesized state (pure parser) ---

func TestGitOperationStateForDetectsEveryKind(t *testing.T) {
	tests := []struct {
		name  string
		state []gitStateEntry
		want  string
	}{
		{
			name:  "no state is idle",
			state: nil,
			want:  "",
		},
		{
			name:  "unknown files only are idle",
			state: mkState(map[string]string{"COMMIT_EDITMSG": "x", "FETCH_HEAD": "y"}),
			want:  "",
		},
		{
			name: "rebase in progress",
			state: mkState(map[string]string{
				"rebase-merge/msgnum":    "3",
				"rebase-merge/end":       "7",
				"rebase-merge/head-name": "refs/heads/main",
				"rebase-merge/onto":      "1a2b3c4d5e6f70819a2b3c4d5e6f70819a2b3c4",
			}, "rebase-merge"),
			want: "rebase",
		},
		{
			name: "interactive rebase",
			state: mkState(map[string]string{
				"rebase-merge/interactive": "",
				"rebase-merge/msgnum":      "1",
				"rebase-merge/end":         "2",
			}, "rebase-merge"),
			want: "rebase-interactive",
		},
		{
			name: "am in progress",
			state: mkState(map[string]string{
				"rebase-apply/applying": "",
				"rebase-apply/next":     "2",
				"rebase-apply/last":     "5",
			}, "rebase-apply"),
			want: "am",
		},
		{
			name:  "rebase-apply without applying is a rebase",
			state: mkState(map[string]string{"rebase-apply/next": "1", "rebase-apply/last": "4"}, "rebase-apply"),
			want:  "rebase",
		},
		{
			name:  "cherry-pick in progress",
			state: mkState(map[string]string{"CHERRY_PICK_HEAD": "abcdef1234567890abcdef1234567890abcdef12"}),
			want:  "cherry-pick",
		},
		{
			name:  "revert in progress",
			state: mkState(map[string]string{"REVERT_HEAD": "abcdef1234567890abcdef1234567890abcdef12"}),
			want:  "revert",
		},
		{
			name:  "sequencer reverting",
			state: mkState(map[string]string{"sequencer/todo": "revert deadbeef Revert \"thing\""}, "sequencer"),
			want:  "revert",
		},
		{
			name:  "sequencer cherry-picking",
			state: mkState(map[string]string{"sequencer/todo": "pick deadbeef some subject"}, "sequencer"),
			want:  "cherry-pick",
		},
		{
			name: "merge in progress",
			state: mkState(map[string]string{
				"MERGE_HEAD": "1234567890abcdef",
				"MERGE_MSG":  "Merge branch 'other'\n\n# Conflicts:\n#\tf.txt\n",
			}),
			want: "merge",
		},
		{
			name:  "bisect in progress",
			state: mkState(map[string]string{"BISECT_START": "abcdef1234567890abcdef1234567890abcdef12"}),
			want:  "bisect",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gitOperationStateFor(tt.state)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("expected no operation, got kind=%q label=%q", got.Kind, got.Label)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected kind %q, got no operation", tt.want)
			}
			if got.Kind != tt.want {
				t.Errorf("kind = %q, want %q (label %q)", got.Kind, tt.want, got.Label)
			}
		})
	}
}

func TestGitOperationStateForPrecedence(t *testing.T) {
	t.Run("rebase-merge wins over merge head", func(t *testing.T) {
		state := mkState(map[string]string{
			"MERGE_HEAD":          "1234567890abcdef",
			"rebase-merge/msgnum": "1",
			"rebase-merge/end":    "2",
		}, "rebase-merge")
		got := gitOperationStateFor(state)
		if got == nil || got.Kind != "rebase" {
			t.Fatalf("expected rebase to win, got %+v", got)
		}
	})

	t.Run("rebase-apply wins over merge head", func(t *testing.T) {
		state := mkState(map[string]string{
			"MERGE_HEAD":        "1234567890abcdef",
			"rebase-apply/next": "1",
			"rebase-apply/last": "1",
		}, "rebase-apply")
		got := gitOperationStateFor(state)
		if got == nil || got.Kind != "rebase" {
			t.Fatalf("expected rebase-apply to win, got %+v", got)
		}
	})

	t.Run("cherry-pick head wins over a sequencer directory", func(t *testing.T) {
		state := mkState(map[string]string{
			"CHERRY_PICK_HEAD": "abcdef1234567890abcdef1234567890abcdef12",
			"sequencer/todo":   "pick deadbeef subject",
		}, "sequencer")
		got := gitOperationStateFor(state)
		if got == nil || got.Kind != "cherry-pick" {
			t.Fatalf("expected cherry-pick, got %+v", got)
		}
		// The head marker and the sequencer directory both classify as
		// "cherry-pick", so asserting the kind alone would not notice a
		// regression in which branch won. The labels differ: the head
		// branch names the commit, the sequencer branch names a series.
		if got.Label != "Cherry-picking abcdef1" {
			t.Errorf("label = %q, want the single-commit head label %q", got.Label, "Cherry-picking abcdef1")
		}
	})

	t.Run("revert head wins over a sequencer directory", func(t *testing.T) {
		state := mkState(map[string]string{
			"REVERT_HEAD":    "abcdef1234567890abcdef1234567890abcdef12",
			"sequencer/todo": "revert deadbeef subject",
		}, "sequencer")
		got := gitOperationStateFor(state)
		if got == nil || got.Kind != "revert" {
			t.Fatalf("expected revert, got %+v", got)
		}
		if got.Label != "Reverting abcdef1" {
			t.Errorf("label = %q, want the single-commit head label %q", got.Label, "Reverting abcdef1")
		}
	})
}

func TestGitOperationStateForLabelsAndProgress(t *testing.T) {
	t.Run("rebase label carries branch, onto and progress", func(t *testing.T) {
		state := mkState(map[string]string{
			"rebase-merge/msgnum":    "3\n",
			"rebase-merge/end":       "7\n",
			"rebase-merge/head-name": "refs/heads/feature/login\n",
			"rebase-merge/onto":      "1a2b3c4d5e6f70819a2b3c4d5e6f70819a2b3c4\n",
		}, "rebase-merge")
		got := gitOperationStateFor(state)
		if got == nil {
			t.Fatal("expected an operation")
		}
		if got.Step != 3 || got.Total != 7 {
			t.Errorf("progress = %d/%d, want 3/7", got.Step, got.Total)
		}
		want := "Rebasing feature/login onto 1a2b3c4 (3/7)"
		if got.Label != want {
			t.Errorf("label = %q, want %q", got.Label, want)
		}
	})

	t.Run("rebase label degrades when optional state is missing", func(t *testing.T) {
		state := mkState(map[string]string{
			"rebase-merge/msgnum": "1",
			"rebase-merge/end":    "1",
		}, "rebase-merge")
		got := gitOperationStateFor(state)
		if got == nil {
			t.Fatal("expected an operation")
		}
		want := "Rebasing (1/1)"
		if got.Label != want {
			t.Errorf("label = %q, want %q", got.Label, want)
		}
	})

	t.Run("non numeric progress is ignored rather than zeroed", func(t *testing.T) {
		state := mkState(map[string]string{
			"rebase-merge/msgnum": "not-a-number",
			"rebase-merge/end":    "also-bad",
		}, "rebase-merge")
		got := gitOperationStateFor(state)
		if got == nil {
			t.Fatal("expected an operation")
		}
		if got.Step != 0 || got.Total != 0 {
			t.Errorf("progress = %d/%d, want 0/0", got.Step, got.Total)
		}
		if strings.Contains(got.Label, "/0") {
			t.Errorf("label %q must not show a 0/0 progress suffix", got.Label)
		}
	})

	t.Run("merge label uses the first line of MERGE_MSG only", func(t *testing.T) {
		state := mkState(map[string]string{
			"MERGE_HEAD": "1234567890abcdef",
			"MERGE_MSG":  "Merge branch 'topic'\n\n# Conflicts:\n#\tf.txt\n",
		})
		got := gitOperationStateFor(state)
		if got == nil {
			t.Fatal("expected an operation")
		}
		if got.Label != "Merge branch 'topic'" {
			t.Errorf("label = %q, want %q", got.Label, "Merge branch 'topic'")
		}
	})

	t.Run("merge without a usable message still labels", func(t *testing.T) {
		got := gitOperationStateFor(mkState(map[string]string{"MERGE_HEAD": "1234567890abcdef"}))
		if got == nil {
			t.Fatal("expected an operation")
		}
		if got.Label == "" {
			t.Error("label must never be empty")
		}
	})

	t.Run("cherry-pick and revert labels shorten the sha", func(t *testing.T) {
		got := gitOperationStateFor(mkState(map[string]string{"CHERRY_PICK_HEAD": "1a2b3c4d5e6f70819a2b3c4d5e6f70819a2b3c4"}))
		if got == nil || got.Label != "Cherry-picking 1a2b3c4" {
			t.Fatalf("cherry-pick label = %+v, want %q", got, "Cherry-picking 1a2b3c4")
		}
		got = gitOperationStateFor(mkState(map[string]string{"REVERT_HEAD": "1a2b3c4d5e6f70819a2b3c4d5e6f70819a2b3c4"}))
		if got == nil || got.Label != "Reverting 1a2b3c4" {
			t.Fatalf("revert label = %+v, want %q", got, "Reverting 1a2b3c4")
		}
	})

	t.Run("bisect label explains how to advance", func(t *testing.T) {
		got := gitOperationStateFor(mkState(map[string]string{"BISECT_START": "abc"}))
		if got == nil {
			t.Fatal("expected an operation")
		}
		if !strings.Contains(strings.ToLower(got.Label), "bisect") {
			t.Errorf("label = %q, want it to mention bisect", got.Label)
		}
	})
}

// --- real repository ---

func TestGitOperationStateForDirRealMergeConflict(t *testing.T) {
	dir := gitConflictedMerge(t)
	got, err := gitOperationStateForDir(context.Background(), dir)
	if err != nil {
		t.Fatalf("detection failed on a real repository: %v", err)
	}
	if got == nil {
		t.Fatal("expected a halted merge to be detected")
	}
	if got.Kind != "merge" {
		t.Errorf("kind = %q, want %q", got.Kind, "merge")
	}
	if got.Label == "" {
		t.Error("label must not be empty")
	}
}

func TestGitOperationStateForDirCleanRepoIsIdle(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "a.txt"), "one")
	run(t, dir, "git", "add", "a.txt")
	run(t, dir, "git", "commit", "-m", "first")
	got, err := gitOperationStateForDir(context.Background(), dir)
	if err != nil {
		t.Fatalf("detection failed on a clean repository: %v", err)
	}
	if got != nil {
		t.Errorf("a clean repository must report no operation, got %+v", got)
	}
}

func TestGitOperationStateForDirNonRepoIsIdle(t *testing.T) {
	got, err := gitOperationStateForDir(context.Background(), t.TempDir())
	if !errors.Is(err, errGitNotARepository) {
		t.Fatalf("a non-repository must report errGitNotARepository, got err=%v op=%+v", err, got)
	}
	if got != nil {
		t.Errorf("a non-repository must report no operation, got %+v", got)
	}
}

// TestGitOperationStateProbePinsOneCLocale pins the probe's environment
// handling. git localizes its diagnostics and isGitNotARepository matches the
// English "not a git repository" text, so the probe must contribute exactly
// one LC_ALL=C and must not leave an inherited LC_ALL behind for
// duplicate-entry resolution to decide.
//
// This asserts the environment rather than running git under a foreign locale:
// a locale that is not installed makes git fall back to C anyway, so an
// end-to-end variant would pass even if the pinning were removed.
func TestGitOperationStateProbePinsOneCLocale(t *testing.T) {
	t.Setenv("LC_ALL", "fr_FR.UTF-8")
	t.Setenv("LANG", "fr_FR.UTF-8")

	env := append(withoutLCAll(gitexec.Env()), probeCLocale)
	count := 0
	for _, kv := range env {
		if !strings.HasPrefix(kv, "LC_ALL=") {
			continue
		}
		count++
		if kv != probeCLocale {
			t.Errorf("LC_ALL entry = %q, want %q", kv, probeCLocale)
		}
	}
	if count != 1 {
		t.Errorf("LC_ALL entries = %d, want exactly 1", count)
	}
	// The probe must keep the shared GIT_OPTIONAL_LOCKS discipline, or it
	// would contend for the user's index on every status poll.
	if !slices.Contains(env, "GIT_OPTIONAL_LOCKS=0") {
		t.Error("probe dropped GIT_OPTIONAL_LOCKS=0 from the shared git env")
	}
}

// TestGitOperationStateForDirRealFailure pins the distinction that keeps
// errGitNotARepository honest: a failure that is NOT "not a repository" must
// surface as a real error, never as a quiet nil operation. Otherwise a broken
// repository, a missing git, or a permission problem would look exactly like a
// clean repo, and a halted merge would silently disappear from the Git tab.
//
// A directory that does not exist makes git fail before it can say anything
// about repositories, which is exactly the "real failure" shape needed here.
func TestGitOperationStateForDirRealFailureIsNotTheSentinel(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	got, err := gitOperationStateForDir(context.Background(), missing)
	if err == nil {
		t.Fatalf("expected a real failure for a missing directory, got operation %+v", got)
	}
	if errors.Is(err, errGitNotARepository) {
		t.Fatalf("a missing directory must not be reported as a non-repository: %v", err)
	}
	if got != nil {
		t.Errorf("a failed detection must not report an operation, got %+v", got)
	}
}

// TestGitOperationStateForDirLinkedWorktree pins the per-worktree rule: the
// git directory reported for a linked worktree is that worktree's own, so
// state written there is detected there and never leaks to the main worktree.
func TestGitOperationStateForDirLinkedWorktree(t *testing.T) {
	mainDir := t.TempDir()
	initGitRepo(t, mainDir)
	writeFile(t, filepath.Join(mainDir, "a.txt"), "one")
	run(t, mainDir, "git", "add", "a.txt")
	run(t, mainDir, "git", "commit", "-m", "first")

	linked := filepath.Join(t.TempDir(), "linked")
	run(t, mainDir, "git", "worktree", "add", "-b", "side", linked)

	out, err := gitCombined(t, linked, "git", "rev-parse", "--absolute-git-dir")
	if err != nil {
		t.Fatalf("rev-parse in the linked worktree failed: %v\n%s", err, out)
	}
	gitDir := strings.TrimSpace(out)
	if !strings.Contains(gitDir, "worktrees") {
		t.Fatalf("expected a per-worktree git dir, got %q", gitDir)
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "rebase-merge"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"msgnum":    "2",
		"end":       "5",
		"head-name": "refs/heads/side",
		"onto":      "1a2b3c4d5e6f70819a2b3c4d5e6f70819a2b3c4",
	} {
		if err := os.WriteFile(filepath.Join(gitDir, "rebase-merge", name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := gitOperationStateForDir(context.Background(), linked)
	if err != nil {
		t.Fatalf("detection failed in the linked worktree: %v", err)
	}
	if got == nil {
		t.Fatal("expected the linked worktree's own rebase state to be detected")
	}
	if got.Kind != "rebase" {
		t.Errorf("linked kind = %q, want %q", got.Kind, "rebase")
	}
	if got.Step != 2 || got.Total != 5 {
		t.Errorf("linked progress = %d/%d, want 2/5", got.Step, got.Total)
	}
	if !strings.Contains(got.Label, "side") {
		t.Errorf("linked label = %q, want it to name the side branch", got.Label)
	}

	other, err := gitOperationStateForDir(context.Background(), mainDir)
	if err != nil {
		t.Fatalf("detection failed in the main worktree: %v", err)
	}
	if other != nil {
		t.Errorf("main worktree must not see the linked worktree's state, got %+v", other)
	}
}
