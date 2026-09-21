package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stashFixture builds a repo with one committed baseline, then stashes a
// tracked modification, a deletion, and an untracked file in one entry. It
// returns the repo dir.
func stashFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "tracked.txt"), "one\ntwo\nthree\n")
	writeFile(t, filepath.Join(dir, "deleted.txt"), "gone\n")
	writeFile(t, filepath.Join(dir, "keep.txt"), "keep\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-m", "init")

	writeFile(t, filepath.Join(dir, "tracked.txt"), "one\nCHANGED\nthree\n")
	if err := os.Remove(filepath.Join(dir, "deleted.txt")); err != nil {
		t.Fatalf("remove deleted.txt: %v", err)
	}
	writeFile(t, filepath.Join(dir, "untracked.txt"), "fresh\n")
	run(t, dir, "git", "stash", "push", "-u", "-m", "mystash")
	return dir
}

func gitStashHandler(t *testing.T, dir string) *Handler {
	t.Helper()
	h := NewHandler()
	h.SetWorkDir(dir)
	return h
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return out
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestGitStashListAndShow(t *testing.T) {
	dir := stashFixture(t)
	h := gitStashHandler(t, dir)

	rec := httptest.NewRecorder()
	h.HandleGitStashList(rec, httptest.NewRequest("GET", "/api/git/stash/list", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", rec.Code, rec.Body.String())
	}
	list := decodeBody[[]GitStash](t, rec)
	if len(list) != 1 {
		t.Fatalf("expected 1 stash, got %d (%+v)", len(list), list)
	}
	if list[0].Index != 0 || list[0].Ref != "stash@{0}" {
		t.Errorf("unexpected stash selector: %+v", list[0])
	}
	if !strings.Contains(list[0].Message, "mystash") {
		t.Errorf("stash subject %q does not carry the message", list[0].Message)
	}
	if list[0].Hash == "" || list[0].Date == "" {
		t.Errorf("stash entry missing hash/date: %+v", list[0])
	}

	rec = httptest.NewRecorder()
	h.HandleGitStashShow(rec, httptest.NewRequest("GET", "/api/git/stash/show?index=0", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("show status = %d, body %s", rec.Code, rec.Body.String())
	}
	files := decodeBody[[]GitDiffFile](t, rec)
	paths := map[string]string{}
	for _, f := range files {
		paths[f.Path] = f.Status
	}
	for _, want := range []string{"tracked.txt", "deleted.txt", "untracked.txt"} {
		if _, ok := paths[want]; !ok {
			t.Errorf("stash show missing %s (got %v)", want, paths)
		}
	}
	if paths["untracked.txt"] != "added" {
		t.Errorf("untracked-in-stash file status = %q, want added", paths["untracked.txt"])
	}
	if paths["deleted.txt"] != "deleted" {
		t.Errorf("deleted-in-stash file status = %q, want deleted", paths["deleted.txt"])
	}
}

func TestGitStashShowRejectsBadIndex(t *testing.T) {
	dir := stashFixture(t)
	h := gitStashHandler(t, dir)

	for _, target := range []string{
		"/api/git/stash/show",           // missing
		"/api/git/stash/show?index=-1",  // negative
		"/api/git/stash/show?index=abc", // malformed
		"/api/git/stash/show?index=9",   // out of range
	} {
		rec := httptest.NewRecorder()
		h.HandleGitStashShow(rec, httptest.NewRequest("GET", target, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body %s)", target, rec.Code, rec.Body.String())
		}
	}
}

// TestGitStashRestoreSelectedFiles proves restore is per-file: restoring only
// the untracked entry leaves the tracked modification and the deletion in the
// stash, and the stash entry survives the restore.
func TestGitStashRestoreSelectedFiles(t *testing.T) {
	dir := stashFixture(t)
	h := gitStashHandler(t, dir)

	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.HandleGitStashApply(rec, httptest.NewRequest("POST", "/api/git/stash/apply", strings.NewReader(body)))
		return rec
	}

	// Restore just the untracked file.
	rec := post(`{"index":0,"paths":["untracked.txt"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := readFileString(t, filepath.Join(dir, "untracked.txt")); got != "fresh\n" {
		t.Errorf("untracked.txt content = %q, want %q", got, "fresh\n")
	}
	// The tracked modification must NOT have been restored.
	if got := readFileString(t, filepath.Join(dir, "tracked.txt")); got != "one\ntwo\nthree\n" {
		t.Errorf("tracked.txt = %q, want the stashed-away (HEAD) content", got)
	}
	// The deletion must NOT have been replayed.
	if _, err := os.Stat(filepath.Join(dir, "deleted.txt")); err != nil {
		t.Errorf("deleted.txt should still exist (only untracked.txt was restored): %v", err)
	}
	// The stash entry itself survives an apply.
	rec = httptest.NewRecorder()
	h.HandleGitStashList(rec, httptest.NewRequest("GET", "/api/git/stash/list", nil))
	if list := decodeBody[[]GitStash](t, rec); len(list) != 1 {
		t.Fatalf("stash apply must not drop the entry; list = %+v", list)
	}

	// Restore the tracked modification.
	if rec := post(`{"index":0,"paths":["tracked.txt"]}`); rec.Code != http.StatusOK {
		t.Fatalf("apply tracked status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := readFileString(t, filepath.Join(dir, "tracked.txt")); got != "one\nCHANGED\nthree\n" {
		t.Errorf("tracked.txt = %q, want restored content", got)
	}

	// Restore the deletion (a file absent from the stash tree).
	if rec := post(`{"index":0,"paths":["deleted.txt"]}`); rec.Code != http.StatusOK {
		t.Fatalf("apply deleted status = %d, body %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "deleted.txt")); !os.IsNotExist(err) {
		t.Errorf("deleted.txt should be removed by the restore, stat err = %v", err)
	}
}

func TestGitStashApplyRejectsEmptyAndUnknownPaths(t *testing.T) {
	dir := stashFixture(t)
	h := gitStashHandler(t, dir)

	post := func(body string) int {
		rec := httptest.NewRecorder()
		h.HandleGitStashApply(rec, httptest.NewRequest("POST", "/api/git/stash/apply", strings.NewReader(body)))
		return rec.Code
	}
	if code := post(`{"index":0,"paths":[]}`); code != http.StatusBadRequest {
		t.Errorf("empty paths status = %d, want 400", code)
	}
	if code := post(`{"index":0,"paths":["not-in-stash.txt"]}`); code != http.StatusBadRequest {
		t.Errorf("unknown path status = %d, want 400", code)
	}
	if code := post(`{"index":5,"paths":["tracked.txt"]}`); code != http.StatusBadRequest {
		t.Errorf("unknown stash status = %d, want 400", code)
	}
}

func TestGitStashDropRemovesEntry(t *testing.T) {
	dir := stashFixture(t)
	h := gitStashHandler(t, dir)

	rec := httptest.NewRecorder()
	h.HandleGitStashDrop(rec, httptest.NewRequest("POST", "/api/git/stash/drop", strings.NewReader(`{"index":0}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("drop status = %d, body %s", rec.Code, rec.Body.String())
	}
	if list := decodeBody[[]GitStash](t, rec); len(list) != 0 {
		t.Errorf("drop response should carry the refreshed (empty) list, got %+v", list)
	}

	rec = httptest.NewRecorder()
	h.HandleGitStashDrop(rec, httptest.NewRequest("POST", "/api/git/stash/drop", strings.NewReader(`{"index":0}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("dropping a missing stash = %d, want 400", rec.Code)
	}
}

// TestGitStashPushIncludeUntracked covers the new include_untracked field on
// the existing push endpoint used by the Git tab's "Stash" button.
func TestGitStashPushIncludeUntracked(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "tracked.txt"), "base\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-m", "init")
	writeFile(t, filepath.Join(dir, "tracked.txt"), "base\nchanged\n")
	writeFile(t, filepath.Join(dir, "brand-new.txt"), "new\n")

	h := gitStashHandler(t, dir)
	rec := httptest.NewRecorder()
	body := `{"paths":[],"message":"with untracked","include_untracked":true}`
	h.HandleGitStash(rec, httptest.NewRequest("POST", "/api/git/stash", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("stash status = %d, body %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "brand-new.txt")); !os.IsNotExist(err) {
		t.Errorf("untracked file should have been stashed away, stat err = %v", err)
	}

	rec = httptest.NewRecorder()
	h.HandleGitStashShow(rec, httptest.NewRequest("GET", "/api/git/stash/show?index=0", nil))
	files := decodeBody[[]GitDiffFile](t, rec)
	found := false
	for _, f := range files {
		if f.Path == "brand-new.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("stash show should list the untracked file, got %+v", files)
	}
}

// TestRemoteGitStashOverFakeSSH exercises list/show/apply/drop through the
// remote transport (fake ssh shim runs the commands locally).
func TestRemoteGitStashOverFakeSSH(t *testing.T) {
	installFakeSSH(t)
	dir := stashFixture(t)
	h := newTestHandlerWithRemote(t, "ci.local", dir)
	query := "?project=" + urlQueryEscape(dir) + "&host=ci.local"

	rec := httptest.NewRecorder()
	h.HandleGitStashList(rec, httptest.NewRequest("GET", "/api/git/stash/list"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("remote list status = %d, body %s", rec.Code, rec.Body.String())
	}
	list := decodeBody[[]GitStash](t, rec)
	if len(list) != 1 || !strings.Contains(list[0].Message, "mystash") {
		t.Fatalf("remote stash list = %+v", list)
	}

	rec = httptest.NewRecorder()
	h.HandleGitStashShow(rec, httptest.NewRequest("GET", "/api/git/stash/show?index=0&project="+urlQueryEscape(dir)+"&host=ci.local", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("remote show status = %d, body %s", rec.Code, rec.Body.String())
	}
	show := decodeBody[[]GitDiffFile](t, rec)
	if len(show) == 0 {
		t.Fatalf("remote show returned no files")
	}

	body := `{"index":0,"paths":["untracked.txt"]}`
	rec = httptest.NewRecorder()
	h.HandleGitStashApply(rec, httptest.NewRequest("POST", "/api/git/stash/apply"+query, strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("remote apply status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := readFileString(t, filepath.Join(dir, "untracked.txt")); got != "fresh\n" {
		t.Errorf("remote restore content = %q, want %q", got, "fresh\n")
	}

	rec = httptest.NewRecorder()
	h.HandleGitStashDrop(rec, httptest.NewRequest("POST", "/api/git/stash/drop"+query, strings.NewReader(`{"index":0}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("remote drop status = %d, body %s", rec.Code, rec.Body.String())
	}
	if list := decodeBody[[]GitStash](t, rec); len(list) != 0 {
		t.Errorf("remote drop list = %+v, want empty", list)
	}
}

func urlQueryEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
