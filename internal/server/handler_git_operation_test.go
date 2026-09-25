package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// GitOperationResult mirrors the endpoint's response payload.
type gitOperationResult struct {
	Workspace GitWorkspace `json:"workspace"`
	Output    string       `json:"output"`
}

// callOperation invokes HandleGitOperation and decodes the result on success.
func callOperation(t *testing.T, dir, action, kind string) (*httptest.ResponseRecorder, gitOperationResult) {
	t.Helper()
	h := NewHandler()
	h.SetWorkDir(dir)
	r, w := call("/api/git/operation", map[string]string{"action": action, "kind": kind})
	h.HandleGitOperation(w, r)

	var res gitOperationResult
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
			t.Fatalf("decode result: %v\nbody=%s", err, w.Body.String())
		}
	}
	return w, res
}

// gitMustOutput returns a command's trimmed stdout, failing the test if the
// command errors. Unlike gitOutput it is used only where the command is
// expected to succeed; assertions that a ref must be ABSENT check the file
// system directly rather than relying on a non-zero exit.
func gitMustOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitCombined(t, dir, args...)
	if err != nil {
		t.Fatalf("command %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(out)
}

// --- the pure command mapping ---

// TestGitOperationCommandMapping pins the exact git argv for every
// (kind, action) pair. Rebase and am cannot be driven end to end here
// (`git rebase` is denied by this environment's permission rules), so this
// table is what proves their flags.
func TestGitOperationCommandMapping(t *testing.T) {
	tests := []struct {
		kind    string
		action  string
		want    []string
		support bool
	}{
		{kind: "merge", action: "continue", want: []string{"merge", "--continue"}, support: true},
		{kind: "merge", action: "abort", want: []string{"merge", "--abort"}, support: true},
		{kind: "merge", action: "skip", support: false},

		{kind: "rebase", action: "continue", want: []string{"rebase", "--continue"}, support: true},
		{kind: "rebase", action: "abort", want: []string{"rebase", "--abort"}, support: true},
		{kind: "rebase", action: "skip", want: []string{"rebase", "--skip"}, support: true},

		{kind: "rebase-interactive", action: "continue", want: []string{"rebase", "--continue"}, support: true},
		{kind: "rebase-interactive", action: "abort", want: []string{"rebase", "--abort"}, support: true},
		{kind: "rebase-interactive", action: "skip", want: []string{"rebase", "--skip"}, support: true},

		{kind: "am", action: "continue", want: []string{"am", "--continue"}, support: true},
		{kind: "am", action: "abort", want: []string{"am", "--abort"}, support: true},
		{kind: "am", action: "skip", want: []string{"am", "--skip"}, support: true},

		{kind: "cherry-pick", action: "continue", want: []string{"cherry-pick", "--continue"}, support: true},
		{kind: "cherry-pick", action: "abort", want: []string{"cherry-pick", "--abort"}, support: true},
		{kind: "cherry-pick", action: "skip", want: []string{"cherry-pick", "--skip"}, support: true},

		{kind: "revert", action: "continue", want: []string{"revert", "--continue"}, support: true},
		{kind: "revert", action: "abort", want: []string{"revert", "--abort"}, support: true},
		{kind: "revert", action: "skip", want: []string{"revert", "--skip"}, support: true},

		// A bisect has no "continue": you advance it by marking a commit.
		{kind: "bisect", action: "continue", support: false},
		{kind: "bisect", action: "good", want: []string{"bisect", "good"}, support: true},
		{kind: "bisect", action: "bad", want: []string{"bisect", "bad"}, support: true},
		{kind: "bisect", action: "skip", want: []string{"bisect", "skip"}, support: true},
		{kind: "bisect", action: "abort", want: []string{"bisect", "reset"}, support: true},

		// A merge genuinely has no skip.
		{kind: "merge", action: "good", support: false},
		// And no other operation can be bisected.
		{kind: "rebase", action: "good", support: false},
		{kind: "rebase", action: "reset", support: false},
		// Unreachable kinds and actions are refused too.
		{kind: "", action: "abort", support: false},
		{kind: "nonsense", action: "abort", support: false},
		{kind: "merge", action: "nonsense", support: false},
	}

	for _, tt := range tests {
		t.Run(tt.kind+"/"+tt.action, func(t *testing.T) {
			got, ok := gitOperationCommand(tt.kind, tt.action)
			if ok != tt.support {
				t.Fatalf("supported = %v, want %v (args %v)", ok, tt.support, got)
			}
			if !ok {
				return
			}
			if strings.Join(got, " ") != strings.Join(tt.want, " ") {
				t.Errorf("args = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- live merge: abort and continue ---

func TestGitOperationAbortRestoresTheMerge(t *testing.T) {
	dir := gitConflictedMerge(t)
	file := filepath.Join(dir, "f.txt")

	if got := readFileString(t, file); !strings.Contains(got, "<<<<<<<") {
		t.Fatalf("fixture should have markers, got %q", got)
	}

	w, res := callOperation(t, dir, "abort", "merge")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	content := readFileString(t, file)
	if strings.Contains(content, "<<<<<<<") {
		t.Errorf("abort must clear the conflict markers, got %q", content)
	}
	if !strings.Contains(content, "OURS") {
		t.Errorf("abort must restore our side, got %q", content)
	}
	if res.Workspace.Status.Operation != nil {
		t.Errorf("operation must be gone after abort, got %+v", res.Workspace.Status.Operation)
	}
	if len(res.Workspace.Status.Conflicts) != 0 {
		t.Errorf("conflicts must be empty after abort, got %+v", res.Workspace.Status.Conflicts)
	}
}

func TestGitOperationContinueCompletesTheMerge(t *testing.T) {
	dir := gitConflictedMerge(t)
	file := filepath.Join(dir, "f.txt")

	// Resolve by hand, the way the web UI would.
	writeFile(t, file, "a\nRESOLVED\nc\n")
	run(t, dir, "git", "add", "--", "f.txt")

	w, res := callOperation(t, dir, "continue", "merge")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if res.Workspace.Status.Operation != nil {
		t.Errorf("operation must be gone after continue, got %+v", res.Workspace.Status.Operation)
	}
	// The merge is committed, so the tree is clean.
	if len(res.Workspace.Status.StagedFiles) != 0 || len(res.Workspace.Status.ChangedFiles) != 0 {
		t.Errorf("tree must be clean after a completed merge, got staged=%v changed=%v",
			res.Workspace.Status.StagedFiles, res.Workspace.Status.ChangedFiles)
	}
	// MERGE_HEAD must be gone once the merge is committed. gitConflicted to
	// an empty MERGE_HEAD file directly, since git rev-parse exits non-zero
	// when it is absent and `run` treats a non-zero exit as a test failure.
	if _, err := os.Stat(filepath.Join(dir, ".git", "MERGE_HEAD")); !os.IsNotExist(err) {
		t.Errorf("MERGE_HEAD must be gone after a completed merge, stat err = %v", err)
	}
}

// TestGitOperationContinueSuppressesTheEditor proves the environment reaches
// the child: `merge --continue` commits, which runs hooks, so a hook can record
// what it was handed. Without GIT_EDITOR=true the request would block forever
// on an editor nobody can see — a hang, not a test failure.
func TestGitOperationContinueSuppressesTheEditor(t *testing.T) {
	dir := gitConflictedMerge(t)
	file := filepath.Join(dir, "f.txt")
	writeFile(t, file, "a\nRESOLVED\nc\n")
	run(t, dir, "git", "add", "--", "f.txt")

	log := filepath.Join(t.TempDir(), "hook.log")
	hook := "#!/bin/sh\n" +
		"printf 'GIT_EDITOR=%s\\n' \"$GIT_EDITOR\" >> " + log + "\n" +
		"printf 'GIT_MERGE_AUTOEDIT=%s\\n' \"$GIT_MERGE_AUTOEDIT\" >> " + log + "\n" +
		"printf 'GIT_TERMINAL_PROMPT=%s\\n' \"$GIT_TERMINAL_PROMPT\" >> " + log + "\n" +
		"printf 'GIT_SEQUENCE_EDITOR=%s\\n' \"$GIT_SEQUENCE_EDITOR\" >> " + log + "\n" +
		"printf 'GIT_LITERAL_PATHSPECS=%s\\n' \"$GIT_LITERAL_PATHSPECS\" >> " + log + "\n" +
		"printf 'GIT_OPTIONAL_LOCKS=%s\\n' \"$GIT_OPTIONAL_LOCKS\" >> " + log + "\n"
	hookPath := filepath.Join(dir, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hookPath, []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}

	w, _ := callOperation(t, dir, "continue", "merge")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	recorded, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("pre-commit hook did not run, so the env was never observed: %v", err)
	}
	got := string(recorded)
	for _, want := range []string{
		"GIT_EDITOR=true",
		"GIT_MERGE_AUTOEDIT=no",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SEQUENCE_EDITOR=true",
		"GIT_LITERAL_PATHSPECS=1",
		"GIT_OPTIONAL_LOCKS=0",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("hook env missing %q; recorded:\n%s", want, got)
		}
	}
}

// --- guards ---

func TestGitOperationRequiresAnOperationInProgress(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "a.txt"), "one")
	run(t, dir, "git", "add", "a.txt")
	run(t, dir, "git", "commit", "-m", "first")

	w, _ := callOperation(t, dir, "abort", "merge")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 when no operation is in progress; body=%s", w.Code, w.Body.String())
	}
}

// TestGitOperationRejectsStaleKind is the guard that stops an Abort issued from
// a panel that has been open since before a rebase from aborting a different
// operation that started since.
func TestGitOperationRejectsStaleKind(t *testing.T) {
	dir := gitConflictedMerge(t)

	w, _ := callOperation(t, dir, "abort", "rebase") // the repo is mid-merge
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for a stale kind; body=%s", w.Code, w.Body.String())
	}

	// The rejected request must have changed nothing.
	status := gitStatusForDir(dir)
	if status.Operation == nil || status.Operation.Kind != "merge" {
		t.Errorf("the merge must still be in progress, got %+v", status.Operation)
	}
	if len(status.Conflicts) != 1 {
		t.Errorf("the conflict must survive a rejected abort, got %+v", status.Conflicts)
	}
}

func TestGitOperationRejectsUnknownAction(t *testing.T) {
	dir := gitConflictedMerge(t)
	w, _ := callOperation(t, dir, "obliterate", "merge")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown action; body=%s", w.Code, w.Body.String())
	}
}

func TestGitOperationRejectsUnsupportedCombination(t *testing.T) {
	dir := gitConflictedMerge(t)
	// A merge has no skip.
	w, _ := callOperation(t, dir, "skip", "merge")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for skip on a merge; body=%s", w.Code, w.Body.String())
	}
}

func TestGitOperationRequiresAKind(t *testing.T) {
	dir := gitConflictedMerge(t)
	w, _ := callOperation(t, dir, "abort", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when the client sends no kind; body=%s", w.Code, w.Body.String())
	}
}

// TestGitOperationContinueRefusesWhileConflictsRemain keeps the server
// consistent with the UI, which disables Continue while conflicts exist. git
// would fail anyway, but a 409 naming the problem beats git's prose.
func TestGitOperationContinueRefusesWhileConflictsRemain(t *testing.T) {
	dir := gitConflictedMerge(t)

	w, _ := callOperation(t, dir, "continue", "merge")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 while conflicts remain; body=%s", w.Code, w.Body.String())
	}
	// Abort must still work, proving the conflict was not resolved behind our back.
	if w2, _ := callOperation(t, dir, "abort", "merge"); w2.Code != http.StatusOK {
		t.Fatalf("abort after the refusal = %d, want 200", w2.Code)
	}
}

// --- bisect ---

func gitConflictedBisect(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "a.txt"), "1\n")
	run(t, dir, "git", "add", "a.txt")
	run(t, dir, "git", "commit", "-m", "one")
	first := gitMustOutput(t, dir, "git", "rev-parse", "HEAD")
	writeFile(t, filepath.Join(dir, "a.txt"), "2\n")
	run(t, dir, "git", "commit", "-am", "two")
	writeFile(t, filepath.Join(dir, "a.txt"), "3\n")
	run(t, dir, "git", "commit", "-am", "three")

	// A real bisect, so the state is genuine rather than hand-built.
	run(t, dir, "git", "bisect", "start")
	run(t, dir, "git", "bisect", "bad")
	run(t, dir, "git", "bisect", "good", first)
	return dir
}

func TestGitOperationDetectsARealBisect(t *testing.T) {
	dir := gitConflictedBisect(t)

	status := gitStatusForDir(dir)
	if status.Operation == nil {
		t.Fatal("a real bisect must be reported as an operation")
	}
	if status.Operation.Kind != "bisect" {
		t.Errorf("kind = %q, want %q", status.Operation.Kind, "bisect")
	}
}

func TestGitOperationBisectGoodAndReset(t *testing.T) {
	dir := gitConflictedBisect(t)

	w, res := callOperation(t, dir, "good", "bisect")
	if w.Code != http.StatusOK {
		t.Fatalf("bisect good status = %d, body=%s", w.Code, w.Body.String())
	}
	// Still bisecting (or finished); either way it is no longer a plain idle repo.
	if res.Workspace.Status.Operation == nil {
		t.Error("expected the bisect to still be in progress after marking good")
	}

	w, res = callOperation(t, dir, "abort", "bisect") // abort maps to bisect reset
	if w.Code != http.StatusOK {
		t.Fatalf("bisect reset status = %d, body=%s", w.Code, w.Body.String())
	}
	if res.Workspace.Status.Operation != nil {
		t.Errorf("reset must end the bisect, got %+v", res.Workspace.Status.Operation)
	}
}

func TestGitOperationBisectRejectsContinue(t *testing.T) {
	dir := gitConflictedBisect(t)
	w, _ := callOperation(t, dir, "continue", "bisect")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: a bisect has no continue; body=%s", w.Code, w.Body.String())
	}
}
