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

// resolveBody builds the request body for the conflict-resolve endpoint.
func conflictResolveBody(path, resolution string) map[string]string {
	return map[string]string{"path": path, "resolution": resolution}
}

// callResolve invokes HandleGitResolveConflict for the repo at dir.
func callResolve(t *testing.T, dir, path, resolution string) (*httptest.ResponseRecorder, GitWorkspace) {
	t.Helper()
	h := NewHandler()
	h.SetWorkDir(dir)
	r, w := call("/api/git/conflict/resolve", conflictResolveBody(path, resolution))
	h.HandleGitResolveConflict(w, r)

	var ws GitWorkspace
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&ws); err != nil {
			t.Fatalf("decode workspace: %v\nbody=%s", err, w.Body.String())
		}
	}
	return w, ws
}

// conflictState reports whether path is still listed as conflicted, and
// whether it appears in either of the two plain file lists.
func conflictState(ws GitWorkspace, path string) (conflicted bool, listed bool) {
	for _, c := range ws.Status.Conflicts {
		if c.Path == path {
			conflicted = true
		}
	}
	for _, f := range ws.Status.StagedFiles {
		if f == path {
			listed = true
		}
	}
	for _, f := range ws.Status.ChangedFiles {
		if f == path {
			listed = true
		}
	}
	return conflicted, listed
}

// --- ours / theirs ---

func TestResolveConflictOursThenTheirs(t *testing.T) {
	// Content conflict: f.txt is "OURS" on our side and "THEIRS" on theirs.
	for _, tc := range []struct {
		resolution string
		want       string
		absent     string
		// wantStaged: after resolving to one side and staging it, the index
		// matches HEAD for "ours" (our commit is HEAD) so the path is in
		// NEITHER list, but differs from HEAD for "theirs" so it MUST appear
		// in staged_files. Verified directly against git.
		wantStaged bool
	}{
		{resolution: "ours", want: "OURS", absent: "THEIRS", wantStaged: false},
		{resolution: "theirs", want: "THEIRS", absent: "OURS", wantStaged: true},
	} {
		t.Run(tc.resolution, func(t *testing.T) {
			dir := gitConflictedMerge(t)
			file := filepath.Join(dir, "f.txt")

			w, ws := callResolve(t, dir, "f.txt", tc.resolution)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
			}

			content := readFileString(t, file)
			if !strings.Contains(content, tc.want) {
				t.Errorf("working tree = %q, want it to contain %q", content, tc.want)
			}
			if strings.Contains(content, tc.absent) {
				t.Errorf("working tree = %q, must not contain the rejected side %q", content, tc.absent)
			}
			// Markers must be gone: the chosen side is written wholesale.
			if strings.Contains(content, "<<<<<<<") {
				t.Errorf("working tree still has conflict markers: %q", content)
			}

			conflicted, listed := conflictState(ws, "f.txt")
			if conflicted {
				t.Error("f.txt is still reported as conflicted after resolving")
			}
			if listed != tc.wantStaged {
				t.Errorf("listed in staged_files = %v, want %v (staged=%v changed=%v)",
					listed, tc.wantStaged, ws.Status.StagedFiles, ws.Status.ChangedFiles)
			}
			if ws.Status.Operation == nil {
				t.Error("the merge is still in progress, so the operation must remain reported")
			}
		})
	}
}

// TestResolveConflictDeletedSideUsesGitRm is the case that fails if the
// resolver naively runs `checkout --ours/--theirs`: one side is a deletion, so
// that stage does not exist and checkout cannot materialize it.
func TestResolveConflictDeletedSideUsesGitRm(t *testing.T) {
	t.Run("accept the deletion", func(t *testing.T) {
		dir := gitModifyDeleteConflict(t)
		file := filepath.Join(dir, "f.txt")

		w, ws := callResolve(t, dir, "f.txt", "theirs") // theirs deleted it
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Errorf("accepting the deleting side must remove the file, stat err = %v", err)
		}
		if conflicted, _ := conflictState(ws, "f.txt"); conflicted {
			t.Error("the conflict must be cleared once the deletion is accepted")
		}
	})

	t.Run("keep the surviving side", func(t *testing.T) {
		dir := gitModifyDeleteConflict(t)
		file := filepath.Join(dir, "f.txt")

		w, ws := callResolve(t, dir, "f.txt", "ours") // ours modified it
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}
		content := readFileString(t, file)
		if !strings.Contains(content, "MODIFIED") {
			t.Errorf("working tree = %q, want our modified content", content)
		}
		if conflicted, _ := conflictState(ws, "f.txt"); conflicted {
			t.Error("the conflict must be cleared once the surviving side is kept")
		}
	})
}

// --- mark resolved ---

func TestResolveConflictMarkRefusesLeftoverMarkers(t *testing.T) {
	dir := gitConflictedMerge(t)
	file := filepath.Join(dir, "f.txt")

	// The file still contains the conflict markers.
	if !strings.Contains(readFileString(t, file), "<<<<<<<") {
		t.Fatalf("fixture should leave markers in the file")
	}

	w, _ := callResolve(t, dir, "f.txt", "mark")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 while markers remain; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "f.txt") {
		t.Errorf("the refusal must name the file, got %s", w.Body.String())
	}

	// It must not have staged anything on the way out.
	status := gitStatusForDir(dir)
	if len(status.Conflicts) != 1 {
		t.Errorf("the file must still be conflicted after a refused mark, got %+v", status.Conflicts)
	}
	if contains(status.StagedFiles, "f.txt") {
		t.Error("a refused mark must not stage the file")
	}
}

func TestResolveConflictMarkSucceedsAfterEditing(t *testing.T) {
	dir := gitConflictedMerge(t)
	file := filepath.Join(dir, "f.txt")

	writeFile(t, file, "a\nRESOLVED BY HAND\nc\n")

	w, ws := callResolve(t, dir, "f.txt", "mark")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if conflicted, listed := conflictState(ws, "f.txt"); conflicted || !listed {
		t.Errorf("after marking, conflicted=%v listed=%v; want false/true", conflicted, listed)
	}
}

// TestResolveConflictMarkAcceptsMarkdownSetextUnderline guards the false
// positive: a run of '=' characters is an ordinary Markdown setext heading
// underline and an ASCII rule, not a conflict separator. Refusing it would
// block resolving a perfectly normal file.
func TestResolveConflictMarkAcceptsMarkdownSetextUnderline(t *testing.T) {
	dir := gitConflictedMerge(t)
	file := filepath.Join(dir, "f.txt")

	writeFile(t, file, "Heading\n=======\n\nbody text\n")

	w, ws := callResolve(t, dir, "f.txt", "mark")
	if w.Code != http.StatusOK {
		t.Fatalf("a setext underline must not be mistaken for a conflict separator; status = %d, body=%s",
			w.Code, w.Body.String())
	}
	if conflicted, _ := conflictState(ws, "f.txt"); conflicted {
		t.Error("the file should be resolved")
	}
}

// TestResolveConflictMarkAcceptsSeparatorInsideCodeFence covers the same false
// positive in its most likely real form: a line of '=' inside a fenced code
// block, which is exactly how a document ABOUT merge conflicts documents the
// separator. It must still be markable.
func TestResolveConflictMarkAcceptsSeparatorInsideCodeFence(t *testing.T) {
	dir := gitConflictedMerge(t)
	file := filepath.Join(dir, "f.txt")

	writeFile(t, file, "Here is what a conflict looks like:\n\n```\n=======\n```\n\ndone\n")

	w, _ := callResolve(t, dir, "f.txt", "mark")
	if w.Code != http.StatusOK {
		t.Fatalf("a separator line inside a code fence must not be refused; status = %d, body=%s",
			w.Code, w.Body.String())
	}
}

// TestResolveConflictMarkRefusesRealMarkers is the other half: the actual
// opening/closing markers MUST be refused, whichever form they take. Without
// this, hasConflictMarkers could be gutted and every accept-test would still
// pass.
func TestResolveConflictMarkRefusesRealMarkers(t *testing.T) {
	for _, content := range []string{
		"<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> other\n",
		// The markers git emits are followed by a label; a bare run of '<' is
		// not what git writes, but the closing marker with a label is.
		"left\n>>>>>>> branch-name\n",
	} {
		dir := gitConflictedMerge(t)
		writeFile(t, filepath.Join(dir, "f.txt"), content)
		if w, _ := callResolve(t, dir, "f.txt", "mark"); w.Code != http.StatusBadRequest {
			t.Errorf("content %q must be refused, got status %d", content, w.Code)
		}
	}
}

// --- path handling ---

func TestResolveConflictPathWithSpaces(t *testing.T) {
	// The conflict is built on the space-containing name from the base commit
	// onward: a path that is already conflicted cannot be renamed.
	const name = "my file.txt"
	dir := gitConflictedMergeFile(t, name)

	w, ws := callResolve(t, dir, name, "ours")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if conflicted, _ := conflictState(ws, name); conflicted {
		t.Error("the conflict on the space-containing path was not cleared")
	}
}

// TestResolveConflictLiteralPathspec proves GIT_LITERAL_PATHSPECS is in force:
// a name shaped like pathspec magic must be treated as a filename, not as
// magic. Without the setting, `:(top)f.txt` would resolve to `<root>/f.txt`
// and the literal `:(top)f.txt` file would never be touched.
func TestResolveConflictLiteralPathspec(t *testing.T) {
	const magic = ":(top)f.txt"
	dir := gitConflictedMergeFile(t, magic)

	// Confirm the repo really holds a file with that literal name.
	if _, err := os.Stat(filepath.Join(dir, magic)); err != nil {
		t.Fatalf("fixture did not create the literal name: %v", err)
	}

	w, ws := callResolve(t, dir, magic, "ours")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if conflicted, _ := conflictState(ws, magic); conflicted {
		t.Errorf("the literal name %q must be resolved as a path, not interpreted as pathspec magic", magic)
	}
	if _, err := os.Stat(filepath.Join(dir, magic)); err != nil {
		t.Errorf("the literal file must still exist (it was resolved, not renamed or ignored): %v", err)
	}
}

// --- validation ---

func TestResolveConflictRejectsNonConflictedPath(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "a.txt"), "one")
	run(t, dir, "git", "add", "a.txt")
	run(t, dir, "git", "commit", "-m", "first")

	w, _ := callResolve(t, dir, "a.txt", "ours")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for a path that is not conflicted; body=%s", w.Code, w.Body.String())
	}
}

func TestResolveConflictRejectsUnknownResolution(t *testing.T) {
	dir := gitConflictedMerge(t)
	w, _ := callResolve(t, dir, "f.txt", "merge-it-myself")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown resolution; body=%s", w.Code, w.Body.String())
	}
}

func TestResolveConflictRejectsMissingPath(t *testing.T) {
	dir := gitConflictedMerge(t)
	w, _ := callResolve(t, dir, "", "ours")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a missing path; body=%s", w.Code, w.Body.String())
	}
}

// TestResolveConflictRefusesPathOutsideRepo pins that the existing path
// validation is reused and cannot be bypassed by this endpoint.
func TestResolveConflictRefusesPathOutsideRepo(t *testing.T) {
	dir := gitConflictedMerge(t)
	w, _ := callResolve(t, dir, "../../etc/passwd", "ours")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a path escaping the repo; body=%s", w.Code, w.Body.String())
	}
}
