package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func postGitIgnore(t *testing.T, h *Handler, query, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/git/ignore"+query, strings.NewReader(body))
	h.HandleGitIgnore(w, r)
	return w
}

func readGitignore(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	return string(b)
}

func TestGitIgnoreAppendsUntrackedPath(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "build.log"), "x\n")

	h := NewHandler()
	h.SetWorkDir(dir)
	w := postGitIgnore(t, h, "", `{"paths":["build.log"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if got := readGitignore(t, dir); got != "/build.log\n" {
		t.Errorf(".gitignore = %q, want %q", got, "/build.log\n")
	}
	// The response is the refreshed status (same contract as stage/unstage).
	var st GitStatus
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !st.IsRepo {
		t.Errorf("IsRepo = false, want true")
	}
}

func TestGitIgnoreDedupesExistingEntry(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, ".gitignore"), "/build.log\n")

	h := NewHandler()
	h.SetWorkDir(dir)
	w := postGitIgnore(t, h, "", `{"paths":["build.log"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if got := readGitignore(t, dir); got != "/build.log\n" {
		t.Errorf(".gitignore rewritten: %q, want unchanged", got)
	}
}

func TestGitIgnoreAddsNewlineWhenMissing(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, ".gitignore"), "/first\n/second")
	writeFile(t, filepath.Join(dir, "third.txt"), "x\n")

	h := NewHandler()
	h.SetWorkDir(dir)
	w := postGitIgnore(t, h, "", `{"paths":["third.txt"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	want := "/first\n/second\n/third.txt\n"
	if got := readGitignore(t, dir); got != want {
		t.Errorf(".gitignore = %q, want %q", got, want)
	}
}

func TestGitIgnoreEscapesSpecials(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	h := NewHandler()
	h.SetWorkDir(dir)
	body := `{"paths":["sp ace.txt","a*b?.txt","[x].txt","newdir/"]}`
	w := postGitIgnore(t, h, "", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	want := "/sp\\ ace.txt\n/a\\*b\\?.txt\n/\\[x\\].txt\n/newdir/\n"
	if got := readGitignore(t, dir); got != want {
		t.Errorf(".gitignore = %q, want %q", got, want)
	}
}

// A tracked file's path is still added when explicitly requested (the API is
// the mechanism); the frontend is what restricts the menu to untracked rows.
func TestGitIgnoreAcceptsTrackedPath(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "tracked.txt"), "x\n")

	h := NewHandler()
	h.SetWorkDir(dir)
	w := postGitIgnore(t, h, "", `{"paths":["tracked.txt"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if got := readGitignore(t, dir); got != "/tracked.txt\n" {
		t.Errorf(".gitignore = %q, want %q", got, "/tracked.txt\n")
	}
}

// git status C-quotes names with spaces/non-ASCII; the handler must decode the
// quoted form before building the literal pattern.
func TestGitIgnoreUnquotesCPath(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	h := NewHandler()
	h.SetWorkDir(dir)
	// JSON string carries the literal characters: "sp ace.txt" (with quotes).
	body := `{"paths":["\"sp ace.txt\""]}`
	w := postGitIgnore(t, h, "", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if got := readGitignore(t, dir); got != "/sp\\ ace.txt\n" {
		t.Errorf(".gitignore = %q, want %q", got, "/sp\\ ace.txt\n")
	}
}

func TestGitIgnoreRejectsPathOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	h := NewHandler()
	h.SetWorkDir(dir)
	w := postGitIgnore(t, h, "", `{"paths":["../evil.txt"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "..", ".gitignore")); err == nil {
		t.Fatal("wrote .gitignore outside the repo")
	}
}

func TestGitIgnoreRejectsInjectionAndAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	h := NewHandler()
	h.SetWorkDir(dir)

	// A C-quoted name carrying an encoded newline decodes to a real newline;
	// it must be refused, or it would inject a second, attacker-chosen
	// .gitignore line. The JSON value is the C-quoted string `"bad\nname"`.
	cases := []struct {
		name string
		body string
	}{
		{name: "encoded newline", body: `{"paths":["\"bad\\nname\""]}`},
		{name: "encoded carriage return", body: `{"paths":["\"bad\\rname\""]}`},
		{name: "literal newline", body: "{\"paths\":[\"bad\nname\"]}"},
		{name: "absolute path", body: `{"paths":["/etc/passwd"]}`},
		{name: "empty path", body: `{"paths":[""]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postGitIgnore(t, h, "", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
			}
			if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err == nil {
				t.Fatal("wrote .gitignore for a rejected path")
			}
		})
	}
}

func TestGitIgnoreNoPaths(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	h := NewHandler()
	h.SetWorkDir(dir)
	if w := postGitIgnore(t, h, "", `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}

// A nested project path (?project=<subdir>, or workDir set to a subdir) must
// resolve the .gitignore at the repo TOPLEVEL and write toplevel-relative
// entries — never subdir-prefixed ones.
func TestGitIgnoreNestedProjectUsesToplevel(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	writeFile(t, filepath.Join(root, "top.txt"), "x\n")
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	h := NewHandler()
	h.SetWorkDir(sub)
	w := postGitIgnore(t, h, "", `{"paths":["top.txt"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if got := readGitignore(t, root); got != "/top.txt\n" {
		t.Errorf("toplevel .gitignore = %q, want %q", got, "/top.txt\n")
	}
	if _, err := os.Stat(filepath.Join(sub, ".gitignore")); err == nil {
		t.Error("wrote a nested .gitignore; entries must target the toplevel")
	}
}

func TestRemoteGitIgnoreOverFakeSSH(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "new.txt"), "x\n")

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	query := "?host=ci.local&project=" + url.QueryEscape(repo)
	w := postGitIgnore(t, h, query, `{"paths":["new.txt"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if got := readGitignore(t, repo); got != "/new.txt\n" {
		t.Errorf("remote .gitignore = %q, want %q", got, "/new.txt\n")
	}
}

func TestGitignoreLineForPath(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "build.log", want: "/build.log"},
		{in: "sub/dir/file.txt", want: "/sub/dir/file.txt"},
		{in: "newdir/", want: "/newdir/"},
		{in: "sp ace.txt", want: "/sp\\ ace.txt"},
		{in: "a*b?.txt", want: "/a\\*b\\?.txt"},
		{in: "[x].txt", want: "/\\[x\\].txt"},
		{in: "back\\slash", want: "/back\\\\slash"},
		{in: "./rel.txt", want: "/rel.txt"},
		{in: "/abs-looking.txt", want: "/abs-looking.txt"},
		{in: "#hashtag.txt", want: "/#hashtag.txt"},
		{in: "!bang.txt", want: "/!bang.txt"},
		{in: "", wantErr: true},
		{in: "bad\nline", wantErr: true},
		{in: ".", wantErr: true},
	}
	for _, tc := range cases {
		got, err := gitignoreLineForPath(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("gitignoreLineForPath(%q) = %q, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("gitignoreLineForPath(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("gitignoreLineForPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildGitignoreAppend(t *testing.T) {
	cases := []struct {
		name     string
		existing string
		entries  []string
		want     string
		added    int
	}{
		{name: "empty", existing: "", entries: []string{"/a"}, want: "/a\n", added: 1},
		{name: "trailing newline", existing: "/a\n", entries: []string{"/b"}, want: "/b\n", added: 1},
		{name: "no trailing newline", existing: "/a", entries: []string{"/b"}, want: "\n/b\n", added: 1},
		{name: "dedupe all", existing: "/a\n", entries: []string{"/a"}, want: "", added: 0},
		{name: "dedupe partial", existing: "/a\n", entries: []string{"/a", "/b"}, want: "/b\n", added: 1},
		{name: "crlf preserved", existing: "/a\r\n", entries: []string{"/b"}, want: "/b\r\n", added: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, added := buildGitignoreAppend([]byte(tc.existing), tc.entries)
			if string(payload) != tc.want {
				t.Errorf("payload = %q, want %q", payload, tc.want)
			}
			if len(added) != tc.added {
				t.Errorf("added = %v, want %d entries", added, tc.added)
			}
		})
	}
}

func TestGitignoreUnquotePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{in: "plain.txt", want: "plain.txt"},
		{in: `"sp ace.txt"`, want: "sp ace.txt"},
		{in: `"caf\303\251.txt"`, want: "café.txt"},
		{in: `"quote\"inside"`, want: `quote"inside`},
		{in: `"tab\there"`, want: "tab\there"},
	}
	for _, tc := range cases {
		if got := gitignoreUnquotePath(tc.in); got != tc.want {
			t.Errorf("gitignoreUnquotePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The route must be reachable through the real mux (a handler-only test cannot
// catch a missing or shadowed route, which presents as an SPA 404).
func TestGitIgnoreRouteIsRegistered(t *testing.T) {
	setHomeTree(t, t.TempDir())
	dir := t.TempDir()
	initGitRepo(t, dir)

	srv := New("127.0.0.1:0", "", "", nil)
	srv.handler.SetWorkDir(dir)
	mux := srv.serveHandler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/git/ignore", strings.NewReader(`{"paths":["build.log"]}`))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/git/ignore = %d body=%s (404 means the route is missing)", rec.Code, rec.Body.String())
	}
	if got := readGitignore(t, dir); got != "/build.log\n" {
		t.Errorf(".gitignore = %q, want %q", got, "/build.log\n")
	}
}

// Pins remoteReadFile's documented not-found contract: a missing remote path
// must return found=false (callers map it to 404 / start-fresh), not an
// "unexpected output" error — the remote command emits a MISSING sentinel.
func TestRemoteReadFileMissingReturnsNotFound(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	h := newTestHandlerWithRemote(t, "ci.local", repo)
	rw, err := h.remoteWorkFor("ci.local", repo)
	if err != nil {
		t.Fatalf("remoteWorkFor: %v", err)
	}
	data, found, err := remoteReadFile(context.Background(), rw, filepath.Join(repo, "nope.txt"))
	if err != nil {
		t.Fatalf("err = %v, want nil for a missing file", err)
	}
	if found {
		t.Error("found = true, want false for a missing file")
	}
	if data != nil {
		t.Errorf("data = %q, want nil", data)
	}
}
