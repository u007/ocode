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

// Reading a file inside .git on a remote SSH project must work, matching the
// local read path: resolveProjectFilePathScoped checks project-root
// containment only and reading .git/config is allowed. The remote read path
// used to run the MUTATION containment check (remoteRelCheck), whose .git
// clause refused reads with "path is inside .git and cannot be modified" —
// the Files tab listed .git/config (once hidden files were shown) but the
// click 400'd and opened nothing.
func TestRemoteFileContentReadsDotGitConfig(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, ".git", "config"), "[core]\n\trepositoryformatversion = 0\n")
	h := newTestHandlerWithRemote(t, "ci.local", repo)

	q := url.Values{
		"host":         {"ci.local"},
		"path":         {repo + "/.git/config"},
		"project_root": {repo},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/files/content?"+q.Encode(), nil)
	h.HandleFileContent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("remote read of .git/config: status %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	content, _ := body["content"].(string)
	if !strings.Contains(content, "repositoryformatversion") {
		t.Fatalf("remote read of .git/config returned %q, want the config body", content)
	}
}

// The remote ctrl/cmd+P picker stats the typed path; it must report the
// .git/config entry as existing, like it would locally.
func TestRemoteFileStatReportsDotGitConfig(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, ".git", "config"), "[core]\n")
	h := newTestHandlerWithRemote(t, "ci.local", repo)

	q := url.Values{
		"host":         {"ci.local"},
		"path":         {repo + "/.git/config"},
		"project_root": {repo},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/files/stat?"+q.Encode(), nil)
	h.HandleFileStat(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("remote stat of .git/config: status %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["exists"] != true {
		t.Fatalf("remote stat of .git/config: exists=%v, want true", body["exists"])
	}
}

// The MUTATION side keeps its .git boundary: saving into .git on a remote
// project is refused, matching the local fs and git-action guards.
func TestRemoteFileSaveRefusedInsideGit(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, ".git", "config"), "[core]\n")
	h := newTestHandlerWithRemote(t, "ci.local", repo)

	body := `{"path":` + jsonQuote(repo+"/.git/config") + `,"project_root":` + jsonQuote(repo) + `,"content":"x\n"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content?host=ci.local", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.HandleSaveFileContent(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("remote save into .git: status %d, want 400; body %s", w.Code, w.Body.String())
	}
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
