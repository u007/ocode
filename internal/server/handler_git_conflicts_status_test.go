package server

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// zeroOID is a git object id of all zeros, which is how the porcelain v2
// unmerged record reports an index stage that does not exist (i.e. a deletion
// on that side).
const zeroOID = "0000000000000000000000000000000000000000"

// uRecord builds one `git status --porcelain=v2 -z` unmerged record. The
// field order is: "u", XY, sub, m1, m2, m3, mW, h1, h2, h3, path.
func uRecord(code, h1, h2, h3, path string) string {
	return strings.Join([]string{"u", code, "N...", "100644", "100644", "100644", "100644", h1, h2, h3, path}, " ")
}

// gitModifyDeleteConflict makes a repository whose merge stops on a
// modify/delete conflict, leaving the file deleted on the "theirs" side.
func gitModifyDeleteConflict(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "f.txt"), "a\nb\n")
	run(t, dir, "git", "add", "f.txt")
	run(t, dir, "git", "commit", "-m", "base")
	run(t, dir, "git", "switch", "-c", "deleter")
	run(t, dir, "git", "rm", "f.txt")
	run(t, dir, "git", "commit", "-m", "delete it")
	run(t, dir, "git", "switch", "-")
	writeFile(t, filepath.Join(dir, "f.txt"), "a\nMODIFIED\n")
	run(t, dir, "git", "commit", "-am", "modify it")
	out, err := gitCombined(t, dir, "git", "merge", "deleter")
	if err == nil {
		t.Fatalf("expected a modify/delete conflict, but the merge succeeded:\n%s", out)
	}
	if !strings.Contains(out, "CONFLICT") {
		t.Fatalf("expected git to report CONFLICT, got:\n%s", out)
	}
	return dir
}

// --- the porcelain v2 unmerged parser ---

func TestParseUnmergedPorcelain(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []GitConflict
	}{
		{
			name: "no records",
			in:   "",
			want: []GitConflict{},
		},
		{
			name: "ordinary records are ignored",
			in:   "1 M. N... 100644 100644 100644 abcdef1 abcdef1 staged.txt\x001 .M N... 100644 100644 100644 abcdef1 abcdef1 worktree.txt\x00",
			want: []GitConflict{},
		},
		{
			name: "both modified",
			in:   uRecord("UU", "aaa", "bbb", "ccc", "f.txt") + "\x00",
			want: []GitConflict{{Path: "f.txt", Code: "UU", Ours: true, Theirs: true}},
		},
		{
			name: "both added",
			in:   uRecord("AA", "aaa", "bbb", "ccc", "f.txt") + "\x00",
			want: []GitConflict{{Path: "f.txt", Code: "AA", Ours: true, Theirs: true}},
		},
		{
			name: "modified by us, deleted by them",
			in:   uRecord("UD", "aaa", "bbb", zeroOID, "f.txt") + "\x00",
			want: []GitConflict{{Path: "f.txt", Code: "UD", Ours: true, Theirs: false}},
		},
		{
			name: "deleted by us, modified by them",
			in:   uRecord("DU", "aaa", zeroOID, "ccc", "f.txt") + "\x00",
			want: []GitConflict{{Path: "f.txt", Code: "DU", Ours: false, Theirs: true}},
		},
		{
			name: "added by us, deleted by them",
			in:   uRecord("AU", "aaa", "bbb", zeroOID, "f.txt") + "\x00",
			want: []GitConflict{{Path: "f.txt", Code: "AU", Ours: true, Theirs: false}},
		},
		{
			name: "added by them, deleted by us",
			in:   uRecord("UA", "aaa", zeroOID, "ccc", "f.txt") + "\x00",
			want: []GitConflict{{Path: "f.txt", Code: "UA", Ours: false, Theirs: true}},
		},
		{
			name: "both deleted",
			in:   uRecord("DD", "aaa", zeroOID, zeroOID, "f.txt") + "\x00",
			want: []GitConflict{{Path: "f.txt", Code: "DD", Ours: false, Theirs: false}},
		},
		{
			name: "a path containing spaces survives",
			in:   uRecord("UU", "aaa", "bbb", "ccc", "dir/my conflict file.txt") + "\x00",
			want: []GitConflict{{Path: "dir/my conflict file.txt", Code: "UU", Ours: true, Theirs: true}},
		},
		{
			name: "several records",
			in: uRecord("UU", "a", "b", "c", "one.txt") + "\x00" +
				uRecord("DU", "a", zeroOID, "c", "two.txt") + "\x00",
			want: []GitConflict{
				{Path: "one.txt", Code: "UU", Ours: true, Theirs: true},
				{Path: "two.txt", Code: "DU", Ours: false, Theirs: true},
			},
		},
		{
			name: "a rename record's extra path field does not swallow the next record",
			// A `2` (rename) record is followed by its original path as a
			// SECOND NUL-separated field. That stray field must not be
			// mistaken for a record, and must not stop the following unmerged
			// record from being parsed.
			in: "2 R. N... 100644 100644 100644 aaa bbb R100 new.txt\x00old.txt\x00" +
				uRecord("UU", "aaa", "bbb", "ccc", "conflicted.txt") + "\x00",
			want: []GitConflict{{Path: "conflicted.txt", Code: "UU", Ours: true, Theirs: true}},
		},
		{
			name: "a truncated record is dropped, not guessed at",
			in:   "u UU N... 100644\x00",
			want: []GitConflict{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseUnmergedPorcelain(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d conflicts (%+v), want %d", len(got), got, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("conflict %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// --- status integration ---

// TestGitStatusReportsConflictOnce pins the counting fix. Before this work a
// conflicted path appeared in `git diff --name-only` twice and in
// `git diff --name-only --cached` once, so it was reported three times across
// the two lists and inflated every badge. It must now appear exactly once, in
// the conflicts list.
func TestGitStatusReportsConflictOnce(t *testing.T) {
	dir := gitConflictedMerge(t)
	status := gitStatusForDir(dir)

	if len(status.Conflicts) != 1 {
		t.Fatalf("conflicts = %+v, want exactly one", status.Conflicts)
	}
	conflict := status.Conflicts[0]
	if conflict.Path != "f.txt" {
		t.Errorf("conflict path = %q, want %q", conflict.Path, "f.txt")
	}
	if conflict.Code != "UU" {
		t.Errorf("conflict code = %q, want %q", conflict.Code, "UU")
	}
	if !conflict.Ours || !conflict.Theirs {
		t.Errorf("both sides exist for a UU conflict, got %+v", conflict)
	}

	total := 0
	for _, f := range status.StagedFiles {
		if f == "f.txt" {
			total++
		}
	}
	for _, f := range status.ChangedFiles {
		if f == "f.txt" {
			total++
		}
	}
	if total != 0 {
		t.Errorf("f.txt must not also appear in staged/changed, found it %d more time(s); staged=%v changed=%v",
			total, status.StagedFiles, status.ChangedFiles)
	}
	if len(status.StagedFiles) != 0 || len(status.ChangedFiles) != 0 {
		t.Errorf("the only conflicted path must leave both lists empty, got staged=%v changed=%v",
			status.StagedFiles, status.ChangedFiles)
	}

	if !status.HasChanges {
		t.Error("a repository stopped on a conflict has changes; HasChanges must be true")
	}
	if status.Operation == nil {
		t.Fatal("a halted merge must be reported as an operation")
	}
	if status.Operation.Kind != "merge" {
		t.Errorf("operation kind = %q, want %q", status.Operation.Kind, "merge")
	}
}

// TestGitStatusModifyDeleteConflict pins the per-side flag for a conflict
// where one side is a deletion, which is what the resolver branches on.
func TestGitStatusModifyDeleteConflict(t *testing.T) {
	dir := gitModifyDeleteConflict(t)
	status := gitStatusForDir(dir)

	if len(status.Conflicts) != 1 {
		t.Fatalf("conflicts = %+v, want exactly one", status.Conflicts)
	}
	conflict := status.Conflicts[0]
	if conflict.Code != "UD" {
		t.Errorf("code = %q, want %q", conflict.Code, "UD")
	}
	if !conflict.Ours {
		t.Error("ours was modified, so the ours stage must exist")
	}
	if conflict.Theirs {
		t.Error("theirs deleted the file, so the theirs stage must be absent")
	}
}

// TestGitStatusCleanRepoHasNoConflicts pins the idle shape: non-nil empty
// lists so the web can read .length, and an ABSENT operation so an idle
// repository marshals identically on every poll and the emitter's change
// dedup does not fire spuriously.
func TestGitStatusCleanRepoHasNoConflicts(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "a.txt"), "one")
	run(t, dir, "git", "add", "a.txt")
	run(t, dir, "git", "commit", "-m", "first")

	status := gitStatusForDir(dir)
	if status.Conflicts == nil {
		t.Error("conflicts must be an empty slice, not nil (the web reads .length)")
	}
	if len(status.Conflicts) != 0 {
		t.Errorf("a clean repository has no conflicts, got %+v", status.Conflicts)
	}
	if status.Operation != nil {
		t.Errorf("a clean repository has no operation, got %+v", status.Operation)
	}

	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["operation"]; ok {
		t.Errorf("an idle status must omit the operation field entirely, got %s", raw)
	}
	conflictsJSON, ok := decoded["conflicts"]
	if !ok {
		t.Fatalf("conflicts must be present even when empty, got %s", raw)
	}
	if string(conflictsJSON) != "[]" {
		t.Errorf("conflicts must serialize as [] not null, got %s", conflictsJSON)
	}

	// Two polls of an unchanged repository must marshal identically, or the
	// emitter would publish a git_status event every interval.
	again, err := json.Marshal(gitStatusForDir(dir))
	if err != nil {
		t.Fatalf("marshal again: %v", err)
	}
	if string(raw) != string(again) {
		t.Errorf("idle status is not stable across polls:\n first=%s\nsecond=%s", raw, again)
	}
}

// TestGitStatusNonRepoHasNoConflictsOrOperation pins that a directory git does
// not manage reports the empty shape rather than an error or a phantom
// operation.
func TestGitStatusNonRepoHasNoConflictsOrOperation(t *testing.T) {
	dir := t.TempDir()
	status := gitStatusForDir(dir)
	if len(status.Conflicts) != 0 {
		t.Errorf("a non-repository has no conflicts, got %+v", status.Conflicts)
	}
	if status.Operation != nil {
		t.Errorf("a non-repository has no operation, got %+v", status.Operation)
	}
	if status.IsRepo {
		t.Error("a plain temp dir must not be reported as a repository")
	}
}

// TestGitStatusKeepsUnconflictedChanges asserts the filtering is narrow: an
// ordinary staged and unstaged file still appears, and only the conflicted
// path is diverted to the conflicts list.
func TestGitStatusKeepsUnconflictedChanges(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "committed.txt"), "one\n")
	run(t, dir, "git", "add", "committed.txt")
	run(t, dir, "git", "commit", "-m", "base")

	// One staged file and one unstaged file, no conflict anywhere.
	writeFile(t, filepath.Join(dir, "staged.txt"), "s\n")
	run(t, dir, "git", "add", "staged.txt")
	writeFile(t, filepath.Join(dir, "committed.txt"), "two\n")

	status := gitStatusForDir(dir)
	if len(status.Conflicts) != 0 {
		t.Errorf("no conflict expected, got %+v", status.Conflicts)
	}
	if status.Operation != nil {
		t.Errorf("no operation expected, got %+v", status.Operation)
	}
	if !contains(status.StagedFiles, "staged.txt") {
		t.Errorf("staged.txt missing from staged list %v", status.StagedFiles)
	}
	if !contains(status.ChangedFiles, "committed.txt") {
		t.Errorf("committed.txt missing from changed list %v", status.ChangedFiles)
	}
}
