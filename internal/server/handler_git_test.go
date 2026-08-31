package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/projects"
)

func TestGitDiffNoRepo(t *testing.T) {
	// Use a temp dir that's definitely not in a git repo
	dir := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/git/diff", nil)
	h.HandleGitDiff(w, r)

	// Should return 200 with empty list (no repo = no changes)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result []GitDiffFile
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d files", len(result))
	}
}

func TestGitDiffCleanRepo(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/git/diff", nil)
	h.HandleGitDiff(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result []GitDiffFile
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty list for clean repo, got %d files", len(result))
	}
}

func TestGitDiffCleanRepoReturnsEmptyArray(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/git/diff", nil)
	h.HandleGitDiff(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Check raw body: must be JSON array "[]" not "null"
	// Decoding into Go []GitDiffFile hides the difference because
	// json.Decode of null into a slice leaves it nil (len(nil)==0).
	body := w.Body.String()
	if body == "null\n" || body == "null" {
		t.Fatal("clean repo returned JSON null instead of empty array [] — frontend crashes on .find()")
	}
	if !strings.HasPrefix(body, "[") {
		t.Fatalf("expected JSON array starting with '[', got: %s", body)
	}
}

func TestGitDiffWithChanges(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Create and modify a file
	writeFile(t, filepath.Join(dir, "test.txt"), "hello")
	run(t, dir, "git", "add", "test.txt")
	run(t, dir, "git", "commit", "-m", "initial")
	writeFile(t, filepath.Join(dir, "test.txt"), "hello world")

	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/git/diff", nil)
	h.HandleGitDiff(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result []GitDiffFile
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(result))
	}
	if result[0].Path != "test.txt" {
		t.Errorf("expected path test.txt, got %s", result[0].Path)
	}
	if result[0].Status != "modified" {
		t.Errorf("expected status modified, got %s", result[0].Status)
	}
	if result[0].Patch == "" {
		t.Error("expected non-empty patch")
	}
}

func TestGitDiffWithPathFilter(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, filepath.Join(dir, "a.txt"), "aaa")
	writeFile(t, filepath.Join(dir, "b.txt"), "bbb")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "initial")
	writeFile(t, filepath.Join(dir, "a.txt"), "aaa modified")
	writeFile(t, filepath.Join(dir, "b.txt"), "bbb modified")

	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/git/diff?path=a.txt", nil)
	h.HandleGitDiff(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result []GitDiffFile
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 file for path filter, got %d", len(result))
	}
	if result[0].Path != "a.txt" {
		t.Errorf("expected path a.txt, got %s", result[0].Path)
	}
}

func TestGitDiffUntrackedFile(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, filepath.Join(dir, "new.txt"), "new content")

	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/git/diff", nil)
	h.HandleGitDiff(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result []GitDiffFile
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 untracked file, got %d", len(result))
	}
	if result[0].Path != "new.txt" {
		t.Errorf("expected path new.txt, got %s", result[0].Path)
	}
	if result[0].Status != "untracked" {
		t.Errorf("expected status untracked, got %s", result[0].Status)
	}
}

func TestGitDiffUsesRegisteredProject(t *testing.T) {
	serverDir := t.TempDir()
	projectDir := t.TempDir()
	initGitRepo(t, projectDir)
	writeFile(t, filepath.Join(projectDir, "project.txt"), "initial")
	run(t, projectDir, "git", "add", "project.txt")
	run(t, projectDir, "git", "commit", "-m", "initial")
	writeFile(t, filepath.Join(projectDir, "project.txt"), "changed")

	h := NewHandler()
	h.SetWorkDir(serverDir)
	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(projectDir); err != nil {
		t.Fatal(err)
	}
	h.projects = store

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/git/diff?project="+url.QueryEscape(projectDir), nil)
	h.HandleGitDiff(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []GitDiffFile
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 1 || result[0].Path != "project.txt" {
		t.Fatalf("expected project.txt diff, got %+v", result)
	}

	bad := httptest.NewRecorder()
	h.HandleGitDiff(bad, httptest.NewRequest("GET", "/api/git/diff?project="+url.QueryEscape(t.TempDir()), nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown project, got %d", bad.Code)
	}
}

// --- helpers ---

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %v failed: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// --- SourceTree-style panel endpoints (log / show / workspace / hunks) ---

func TestGitLogListsCommits(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "a.txt"), "one")
	run(t, dir, "git", "add", "a.txt")
	run(t, dir, "git", "commit", "-m", "first commit")
	writeFile(t, filepath.Join(dir, "a.txt"), "two")
	run(t, dir, "git", "add", "a.txt")
	run(t, dir, "git", "commit", "-m", "second commit")

	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	h.HandleGitLog(w, httptest.NewRequest("GET", "/api/git/log", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var commits []GitCommit
	if err := json.NewDecoder(w.Body).Decode(&commits); err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].Message != "second commit" || commits[1].Message != "first commit" {
		t.Errorf("wrong order/messages: %+v", commits)
	}
	if commits[0].Hash == "" || commits[0].Short == "" {
		t.Error("expected hash and short hash")
	}
	if commits[0].Author != "Test" || commits[0].Email != "test@test.com" {
		t.Errorf("expected author Test, got %q / %q", commits[0].Author, commits[0].Email)
	}
	if commits[0].Date == "" {
		t.Error("expected date")
	}
}

func TestGitLogNoCommits(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	h.HandleGitLog(w, httptest.NewRequest("GET", "/api/git/log", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.TrimSpace(body) != "[]" {
		t.Fatalf("expected empty array, got %s", body)
	}
}

func TestGitShowCommitDiff(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "a.txt"), "one")
	run(t, dir, "git", "add", "a.txt")
	run(t, dir, "git", "commit", "-m", "first")
	hash := strings.TrimSpace(runOut(t, dir, "git", "rev-parse", "HEAD"))

	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	h.HandleGitShow(w, httptest.NewRequest("GET", "/api/git/show?commit="+hash, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var files []GitDiffFile
	if err := json.NewDecoder(w.Body).Decode(&files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "a.txt" || files[0].Status != "added" {
		t.Fatalf("expected added a.txt, got %+v", files)
	}
}

func TestGitShowBadCommit(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	h := NewHandler()
	h.SetWorkDir(dir)
	for _, rev := range []string{"", "not-a-commit", "HEAD~99"} {
		w := httptest.NewRecorder()
		h.HandleGitShow(w, httptest.NewRequest("GET", "/api/git/show?commit="+rev, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("rev %q: expected 400, got %d", rev, w.Code)
		}
	}
}

func TestGitDiffStagedOnly(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "f.txt"), "base")
	run(t, dir, "git", "add", "f.txt")
	run(t, dir, "git", "commit", "-m", "initial")
	writeFile(t, filepath.Join(dir, "f.txt"), "staged change")
	run(t, dir, "git", "add", "f.txt")
	writeFile(t, filepath.Join(dir, "f.txt"), "working change")

	h := NewHandler()
	h.SetWorkDir(dir)

	w := httptest.NewRecorder()
	h.HandleGitDiff(w, httptest.NewRequest("GET", "/api/git/diff?staged=true", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var staged []GitDiffFile
	if err := json.NewDecoder(w.Body).Decode(&staged); err != nil {
		t.Fatal(err)
	}
	if len(staged) != 1 || !strings.Contains(staged[0].Patch, "+staged change") {
		t.Fatalf("expected staged diff with staged change, got %+v", staged)
	}

	// Default (no staged param) must stay the working-tree diff.
	w2 := httptest.NewRecorder()
	h.HandleGitDiff(w2, httptest.NewRequest("GET", "/api/git/diff", nil))
	var unstaged []GitDiffFile
	if err := json.NewDecoder(w2.Body).Decode(&unstaged); err != nil {
		t.Fatal(err)
	}
	if len(unstaged) != 1 || !strings.Contains(unstaged[0].Patch, "+working change") {
		t.Fatalf("expected unstaged diff with working change, got %+v", unstaged)
	}
}

func TestGitWorkspaceCombinesStagedUnstagedUntracked(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "tracked.txt"), "base")
	writeFile(t, filepath.Join(dir, "second.txt"), "second base")
	run(t, dir, "git", "add", "tracked.txt", "second.txt")
	run(t, dir, "git", "commit", "-m", "initial")

	// staged-only change
	writeFile(t, filepath.Join(dir, "tracked.txt"), "base + staged")
	run(t, dir, "git", "add", "tracked.txt")
	// unstaged change on another tracked file
	writeFile(t, filepath.Join(dir, "second.txt"), "second base + work")
	// untracked file
	writeFile(t, filepath.Join(dir, "new.txt"), "untracked")

	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	h.HandleGitWorkspace(w, httptest.NewRequest("GET", "/api/git/workspace", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var ws GitWorkspace
	if err := json.NewDecoder(w.Body).Decode(&ws); err != nil {
		t.Fatal(err)
	}
	if !ws.Status.HasChanges {
		t.Fatal("expected has_changes")
	}
	if len(ws.Staged) != 1 || ws.Staged[0].Path != "tracked.txt" {
		t.Fatalf("expected staged tracked.txt, got %+v", ws.Staged)
	}
	if len(ws.Unstaged) != 2 {
		t.Fatalf("expected 2 unstaged (second.txt + new.txt), got %+v", ws.Unstaged)
	}
	patches := map[string]string{}
	for _, f := range ws.Unstaged {
		patches[f.Path] = f.Status
	}
	if patches["second.txt"] != "modified" || patches["new.txt"] != "untracked" {
		t.Fatalf("unexpected unstaged map: %+v", patches)
	}
}

// makeTwoHunkFile writes a 20-line file, commits it, then applies the two
// distinct edits used by the hunk tests (line 2 and line 18).
func makeTwoHunkFile(t *testing.T, dir string) {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&b, "line%d\n", i)
	}
	writeFile(t, filepath.Join(dir, "h.txt"), b.String())
	run(t, dir, "git", "add", "h.txt")
	run(t, dir, "git", "commit", "-m", "baseline")

	lines := strings.Split(b.String(), "\n")
	lines[1] = "line2 EDITED"
	lines[17] = "line18 EDITED"
	writeFile(t, filepath.Join(dir, "h.txt"), strings.Join(lines, "\n"))
}

func TestGitHunkStageFromUnstaged(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	makeTwoHunkFile(t, dir)

	h := NewHandler()
	h.SetWorkDir(dir)
	body, _ := json.Marshal(gitHunkRequest{Path: "h.txt", Hunk: 0, Action: "stage", Staged: false})
	w := httptest.NewRecorder()
	h.HandleGitHunk(w, httptest.NewRequest("POST", "/api/git/hunk", strings.NewReader(string(body))))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var ws GitWorkspace
	if err := json.NewDecoder(w.Body).Decode(&ws); err != nil {
		t.Fatal(err)
	}
	if len(ws.Staged) != 1 || ws.Staged[0].Path != "h.txt" {
		t.Fatalf("expected h.txt staged, got %+v", ws.Staged)
	}
	if !strings.Contains(ws.Staged[0].Patch, "+line2 EDITED") {
		t.Errorf("staged patch should contain the staged hunk, got: %s", ws.Staged[0].Patch)
	}
	if strings.Contains(ws.Staged[0].Patch, "+line18 EDITED") {
		t.Error("staged patch should not contain the unstaged hunk")
	}
}

func TestGitHunkDiscardFromUnstaged(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	makeTwoHunkFile(t, dir)

	h := NewHandler()
	h.SetWorkDir(dir)
	body, _ := json.Marshal(gitHunkRequest{Path: "h.txt", Hunk: 0, Action: "discard", Staged: false})
	w := httptest.NewRecorder()
	h.HandleGitHunk(w, httptest.NewRequest("POST", "/api/git/hunk", strings.NewReader(string(body))))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var ws GitWorkspace
	if err := json.NewDecoder(w.Body).Decode(&ws); err != nil {
		t.Fatal(err)
	}
	if len(ws.Unstaged) != 1 || ws.Unstaged[0].Path != "h.txt" {
		t.Fatalf("expected h.txt still unstaged, got %+v", ws.Unstaged)
	}
	if strings.Contains(ws.Unstaged[0].Patch, "+line2 EDITED") {
		t.Error("discarded hunk should be gone from the patch")
	}
	if !strings.Contains(ws.Unstaged[0].Patch, "+line18 EDITED") {
		t.Error("the other hunk should remain")
	}
	// Verify the actual file content on disk.
	content, err := os.ReadFile(filepath.Join(dir, "h.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "line2\n") {
		t.Error("line2 should be reverted on disk")
	}
	if !strings.Contains(string(content), "line18 EDITED") {
		t.Error("line18 edit should survive on disk")
	}
}

func TestGitHunkUnstageFromStaged(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	makeTwoHunkFile(t, dir)
	run(t, dir, "git", "add", "h.txt")

	h := NewHandler()
	h.SetWorkDir(dir)
	body, _ := json.Marshal(gitHunkRequest{Path: "h.txt", Hunk: 1, Action: "unstage", Staged: true})
	w := httptest.NewRecorder()
	h.HandleGitHunk(w, httptest.NewRequest("POST", "/api/git/hunk", strings.NewReader(string(body))))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var ws GitWorkspace
	if err := json.NewDecoder(w.Body).Decode(&ws); err != nil {
		t.Fatal(err)
	}
	if len(ws.Staged) != 1 || ws.Staged[0].Path != "h.txt" {
		t.Fatalf("expected h.txt still staged, got %+v", ws.Staged)
	}
	if strings.Contains(ws.Staged[0].Patch, "+line18 EDITED") {
		t.Error("unstaged hunk should leave the staged patch")
	}
	// The unstaged area must now show hunk 1 (line18) as an unstaged change.
	found := false
	for _, f := range ws.Unstaged {
		if f.Path == "h.txt" && strings.Contains(f.Patch, "+line18 EDITED") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected line18 hunk back in unstaged, got %+v", ws.Unstaged)
	}
}

func TestGitHunkUntrackedStageAndDiscard(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	// stage an untracked file via hunk 0
	writeFile(t, filepath.Join(dir, "brand.txt"), "brand new")
	h := NewHandler()
	h.SetWorkDir(dir)
	body, _ := json.Marshal(gitHunkRequest{Path: "brand.txt", Hunk: 0, Action: "stage", Staged: false})
	w := httptest.NewRecorder()
	h.HandleGitHunk(w, httptest.NewRequest("POST", "/api/git/hunk", strings.NewReader(string(body))))
	if w.Code != http.StatusOK {
		t.Fatalf("stage: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var ws GitWorkspace
	if err := json.NewDecoder(w.Body).Decode(&ws); err != nil {
		t.Fatal(err)
	}
	if len(ws.Staged) != 1 || ws.Staged[0].Path != "brand.txt" {
		t.Fatalf("stage: expected brand.txt staged, got %+v", ws.Staged)
	}

	// discard a different untracked file via hunk 0
	writeFile(t, filepath.Join(dir, "temp.txt"), "temporary")
	body2, _ := json.Marshal(gitHunkRequest{Path: "temp.txt", Hunk: 0, Action: "discard", Staged: false})
	w2 := httptest.NewRecorder()
	h.HandleGitHunk(w2, httptest.NewRequest("POST", "/api/git/hunk", strings.NewReader(string(body2))))
	if w2.Code != http.StatusOK {
		t.Fatalf("discard: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "temp.txt")); !os.IsNotExist(err) {
		t.Fatalf("discard: expected temp.txt deleted, stat err=%v", err)
	}
}

func TestGitHunkRejectsInvalidRequests(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	makeTwoHunkFile(t, dir)

	h := NewHandler()
	h.SetWorkDir(dir)

	cases := []gitHunkRequest{
		{Path: "h.txt", Hunk: 0, Action: "bogus", Staged: false},         // bad action
		{Path: "h.txt", Hunk: 9, Action: "stage", Staged: false},         // out of range
		{Path: "h.txt", Hunk: 0, Action: "stage", Staged: true},          // stage on staged
		{Path: "h.txt", Hunk: 0, Action: "unstage", Staged: false},       // unstage on unstaged
		{Path: "h.txt", Hunk: 0, Action: "discard", Staged: true},        // discard on staged
		{Path: "../escape.txt", Hunk: 0, Action: "stage", Staged: false}, // path traversal
		{Path: "", Hunk: 0, Action: "stage", Staged: false},              // empty path
	}
	for _, tc := range cases {
		body, _ := json.Marshal(tc)
		w := httptest.NewRecorder()
		h.HandleGitHunk(w, httptest.NewRequest("POST", "/api/git/hunk", strings.NewReader(string(body))))
		if w.Code != http.StatusBadRequest {
			t.Errorf("case %+v: expected 400, got %d: %s", tc, w.Code, w.Body.String())
		}
	}
}

func TestSplitDiffHunks(t *testing.T) {
	header, hunks := splitDiffHunks("")
	if header != "" || len(hunks) != 0 {
		t.Fatalf("empty diff: header=%q hunks=%v", header, hunks)
	}
	diff := "diff --git a/f.txt b/f.txt\nindex aaa..bbb 100644\n--- a/f.txt\n+++ b/f.txt\n@@ -1,3 +1,3 @@\n-x\n+y\n@@ -10,2 +10,2 @@\n-a\n+b\n"
	header, hunks = splitDiffHunks(diff)
	if !strings.Contains(header, "diff --git") || !strings.Contains(header, "+++ b/f.txt") {
		t.Errorf("expected preamble in header, got %q", header)
	}
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(hunks))
	}
	if !strings.HasPrefix(hunks[0], "@@ -1,3") || !strings.HasPrefix(hunks[1], "@@ -10,2") {
		t.Errorf("wrong hunk contents: %q / %q", hunks[0], hunks[1])
	}
}

// runOut runs a command and returns trimmed stdout (test helper).
func runOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("command %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestGitHunkDeleteRestore(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "f.txt"), "line1\nline2\nline3\n")
	run(t, dir, "git", "add", "f.txt")
	run(t, dir, "git", "commit", "-m", "init")

	// Deleting a file yields a deletion diff; the single hunk's "discard"
	// (reverse apply) must restore the file in the working tree.
	if err := os.Remove(filepath.Join(dir, "f.txt")); err != nil {
		t.Fatal(err)
	}
	h := NewHandler()
	h.SetWorkDir(dir)
	body, _ := json.Marshal(gitHunkRequest{Path: "f.txt", Hunk: 0, Action: "discard", Staged: false})
	w := httptest.NewRecorder()
	h.HandleGitHunk(w, httptest.NewRequest("POST", "/api/git/hunk", strings.NewReader(string(body))))
	if w.Code != http.StatusOK {
		t.Fatalf("discard deletion: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	content, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	if err != nil || !strings.Contains(string(content), "line2") {
		t.Fatalf("expected file restored after discard, err=%v content=%q", err, content)
	}

	// And "stage" on the deletion hunk must stage the deletion (staged list
	// gains the path as deleted).
	if err := os.Remove(filepath.Join(dir, "f.txt")); err != nil {
		t.Fatal(err)
	}
	body2, _ := json.Marshal(gitHunkRequest{Path: "f.txt", Hunk: 0, Action: "stage", Staged: false})
	w2 := httptest.NewRecorder()
	h.HandleGitHunk(w2, httptest.NewRequest("POST", "/api/git/hunk", strings.NewReader(string(body2))))
	if w2.Code != http.StatusOK {
		t.Fatalf("stage deletion: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var ws GitWorkspace
	if err := json.NewDecoder(w2.Body).Decode(&ws); err != nil {
		t.Fatal(err)
	}
	if len(ws.Staged) != 1 || ws.Staged[0].Path != "f.txt" || ws.Staged[0].Status != "deleted" {
		t.Fatalf("expected staged deleted f.txt, got %+v", ws.Staged)
	}
}
