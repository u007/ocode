package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The web file tree sends root-anchored paths, not just root-relative ones:
// the tree view builds every mutation target with treePathForRequest, so on a
// remote project whose saved path is "~/www/app" the request path is
// "~/www/app/Makefile". remoteAbsJoin used to detect "already absolute" with a
// leading "/" test, so a tilde-rooted path was prefixed onto the root a second
// time ("~/www/app/~/www/app/Makefile") and every create/copy/paste on such a
// project failed with `parent directory of "~/www/app/Makefile" does not
// exist`.
func TestRemoteAbsJoinKeepsRootAnchoredPaths(t *testing.T) {
	cases := []struct {
		name string
		root string
		p    string
		want string
	}{
		{"tilde root anchored file", "~/www/app", "~/www/app/Makefile", "~/www/app/Makefile"},
		{"tilde root anchored nested", "~/www/app", "~/www/app/src/main.go", "~/www/app/src/main.go"},
		{"tilde root itself", "~/www/app", "~/www/app", "~/www/app"},
		{"tilde root trailing slash", "~/www/app/", "~/www/app/Makefile", "~/www/app/Makefile"},
		{"tilde root relative", "~/www/app", "src/main.go", "~/www/app/src/main.go"},
		{"tilde home root", "~", "~/app/main.go", "~/app/main.go"},
		{"tilde home root relative", "~", "app/main.go", "~/app/main.go"},
		{"absolute root anchored", "/home/me/app", "/home/me/app/Makefile", "/home/me/app/Makefile"},
		{"absolute root relative", "/home/me/app", "Makefile", "/home/me/app/Makefile"},
		{"absolute root itself", "/home/me/app", "/home/me/app", "/home/me/app"},
		{"empty path is the root", "~/www/app", "", "~/www/app"},
		// A sibling whose name merely starts with the root's name is NOT
		// root-anchored; it stays the pre-existing (contained) join.
		{"sibling prefix is not anchored", "~/www/app", "~/www/app2/x", "~/www/app/~/www/app2/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := remoteAbsJoin(tc.root, tc.p); got != tc.want {
				t.Fatalf("remoteAbsJoin(%q, %q) = %q, want %q", tc.root, tc.p, got, tc.want)
			}
		})
	}
}

// tildeRemoteProject makes a real directory tree at $HOME/www/aimsweb and
// registers the remote project under its tilde spelling (which is what the
// desktop saves for a remote project — AddRemote keeps "~" verbatim). The fake
// ssh shim runs the remote command locally with the redirected $HOME, so
// "$HOME/www/aimsweb" exists exactly where the remote path points.
func tildeRemoteProject(t *testing.T) (*Handler, string) {
	t.Helper()
	return tildeRemoteProjectRoot(t, "~/www/aimsweb")
}

func tildeRemoteProjectRoot(t *testing.T, savedRoot string) (*Handler, string) {
	t.Helper()
	installFakeSSH(t)
	home := t.TempDir()
	setHomeTree(t, home)
	proj := filepath.Join(home, "www", "aimsweb")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	h := newTestHandlerWithRemote(t, "ci.local", savedRoot)
	return h, proj
}

func remoteFSTarget(t *testing.T, path string, query url.Values) string {
	return remoteFSTargetProject("~/www/aimsweb", path, query)
}

func remoteFSTargetProject(project, path string, query url.Values) string {
	q := url.Values{"host": {"ci.local"}, "project": {project}}
	for k, v := range query {
		q[k] = v
	}
	return path + "?" + q.Encode()
}

// TestRemoteNewFileAtTildeRootedProjectRoot reproduces the reported failure:
// "add a new file" from the root menu of a remote project whose saved path is
// "~/www/aimsweb".
func TestRemoteNewFileAtTildeRootedProjectRoot(t *testing.T) {
	h, proj := tildeRemoteProject(t)
	r, w := call(remoteFSTarget(t, "/api/fs/new-file", nil),
		map[string]any{"path": "~/www/aimsweb/Makefile"})
	h.HandleFSNewFile(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("new-file at project root failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(proj, "Makefile")); err != nil {
		t.Fatalf("created file missing: %v", err)
	}
	// The doubled-join bug would have created the file under a literal "~"
	// directory inside the project.
	if entries, err := os.ReadDir(proj); err == nil {
		for _, e := range entries {
			if e.Name() == "~" {
				t.Fatalf("created a bogus literal \"~\" directory inside the project")
			}
		}
	}
}

// A saved remote project path is stored verbatim, so the user's own spelling
// "~/www/aimsweb/" must resolve exactly like "~/www/aimsweb".
func TestRemoteNewFileWithTrailingSlashRoot(t *testing.T) {
	h, proj := tildeRemoteProjectRoot(t, "~/www/aimsweb/")
	r, w := call(remoteFSTargetProject("~/www/aimsweb/", "/api/fs/new-file", nil),
		map[string]any{"path": "~/www/aimsweb/Makefile"})
	h.HandleFSNewFile(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("new-file under a trailing-slash root failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(proj, "Makefile")); err != nil {
		t.Fatalf("created file missing: %v", err)
	}
}

func TestRemoteRelCheckToleratesTrailingSlashRoot(t *testing.T) {
	rel, err := remoteRelCheck("~/www/app/", "~/www/app/src/main.go")
	if err != nil || rel != "src/main.go" {
		t.Fatalf("remoteRelCheck(trailing slash) = %q, %v; want %q, nil", rel, err, "src/main.go")
	}
	if rel, err = remoteRelCheck("~/www/app/", "~/www/app"); err != nil || rel != "." {
		t.Fatalf("remoteRelCheck(root itself) = %q, %v; want %q, nil", rel, err, ".")
	}
	if _, err = remoteRelCheck("~/www/app/", "~/www/app2/x"); err == nil {
		t.Fatal("remoteRelCheck accepted a sibling of the root")
	}
}

func TestRemoteNewFileInTildeRootedSubdirectory(t *testing.T) {
	h, proj := tildeRemoteProject(t)
	sub := filepath.Join(proj, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	r, w := call(remoteFSTarget(t, "/api/fs/new-file", nil),
		map[string]any{"path": "~/www/aimsweb/src/main.go"})
	h.HandleFSNewFile(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("new-file in subdirectory failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(sub, "main.go")); err != nil {
		t.Fatalf("created file missing: %v", err)
	}
}

// The project root is a valid PASTE destination ("paste to the root folder"),
// exactly like the local handler: resolveFSPath("read") accepts the root there.
// remoteTargetOf rejects "." for every operation, so copy/move need the
// destination mode.
func TestRemoteCopyIntoProjectRoot(t *testing.T) {
	h, proj := tildeRemoteProject(t)
	sub := filepath.Join(proj, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "pasted.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, w := call(remoteFSTarget(t, "/api/fs/copy", nil),
		map[string]any{"paths": []string{"sub/pasted.txt"}, "dest_dir": "~/www/aimsweb"})
	h.HandleFSCopy(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("copy into project root failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(proj, "pasted.txt")); err != nil {
		t.Fatalf("copied file missing at project root: %v", err)
	}
}

// The columns (Miller) view pastes with a root-RELATIVE destination node path,
// while the tree view passes an anchored one. Both must reach the same
// directory: remoteAbsJoin joins the relative form against the project root and
// the command still has to expand $HOME.
func TestRemoteCopyIntoRelativeSubdirectory(t *testing.T) {
	h, proj := tildeRemoteProject(t)
	if err := os.WriteFile(filepath.Join(proj, "src.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(proj, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	r, w := call(remoteFSTarget(t, "/api/fs/copy", nil),
		map[string]any{"paths": []string{"src.txt"}, "dest_dir": "sub"})
	h.HandleFSCopy(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("copy into a relative subdirectory failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(sub, "src.txt")); err != nil {
		t.Fatalf("copied file missing in the subdirectory: %v", err)
	}
}

// Every remote fs mutation builds its command from the resolved absolute path,
// which is "~"-rooted on a tilde project. Quoting it with ShellQuote instead of
// ShellQuotePath single-quotes the "~", so the POSIX shell never expands it:
// delete silently no-ops (rm -rf on a literal "~/…" path exits 0), rename and
// new-folder fail, and download/upload cannot open the file.
func TestRemoteDeleteInTildeRootedProject(t *testing.T) {
	h, proj := tildeRemoteProject(t)
	target := filepath.Join(proj, "gone.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, w := call(remoteFSTarget(t, "/api/fs/delete", nil),
		map[string]any{"paths": []string{"gone.txt"}})
	h.HandleFSDelete(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("delete failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("file still present after delete: %v", err)
	}
}

func TestRemoteRenameInTildeRootedProject(t *testing.T) {
	h, proj := tildeRemoteProject(t)
	if err := os.WriteFile(filepath.Join(proj, "old.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, w := call(remoteFSTarget(t, "/api/fs/rename", nil),
		map[string]any{"path": "old.txt", "new_name": "new.txt"})
	h.HandleFSRename(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("rename failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(proj, "new.txt")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
}

func TestRemoteNewFolderInTildeRootedProject(t *testing.T) {
	h, proj := tildeRemoteProject(t)
	r, w := call(remoteFSTarget(t, "/api/fs/new-folder", nil),
		map[string]any{"path": "~/www/aimsweb/docs"})
	h.HandleFSNewFolder(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("new-folder failed: %d %s", w.Code, w.Body.String())
	}
	if info, err := os.Stat(filepath.Join(proj, "docs")); err != nil || !info.IsDir() {
		t.Fatalf("created folder missing: %v", err)
	}
}

func TestRemoteDownloadFromTildeRootedProject(t *testing.T) {
	h, proj := tildeRemoteProject(t)
	if err := os.WriteFile(filepath.Join(proj, "readme.md"), []byte("hello remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := remoteFSTarget(t, "/api/fs/download", url.Values{"path": {"readme.md"}})
	r := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	h.HandleFSDownload(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("download failed: %d %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "hello remote\n" {
		t.Fatalf("downloaded body = %q, want %q", w.Body.String(), "hello remote\n")
	}
}

// remoteStreamFile is the paste-upload writer: it claims the destination with
// `set -C` in one remote script. The destination is "~"-rooted on a tilde
// project, so the script must expand $HOME too.
func TestRemoteStreamFileExpandsTildeRoot(t *testing.T) {
	h, proj := tildeRemoteProject(t)
	rw, err := h.remoteWorkFor("ci.local", "~/www/aimsweb")
	if err != nil {
		t.Fatalf("remoteWorkFor: %v", err)
	}
	created, err := remoteStreamFile(context.Background(), rw, "~/www/aimsweb/pasted.txt", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("remoteStreamFile: %v", err)
	}
	if !created {
		t.Fatalf("remoteStreamFile reported created=false")
	}
	body, err := os.ReadFile(filepath.Join(proj, "pasted.txt"))
	if err != nil {
		t.Fatalf("pasted file missing: %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("pasted content = %q, want %q", body, "hello")
	}
}

// Discarding an untracked file builds an `rm -f -- <root>/<spec>` line from the
// remote root, which is "~"-rooted here: quoting it with ShellQuote left the
// "~" literal, so the rm silently removed nothing (exit 0) while the API
// reported success.
func TestRemoteDiscardUntrackedFileInTildeRootedProject(t *testing.T) {
	h, proj := tildeRemoteProject(t)
	runGit(t, proj, "init")
	runGit(t, proj, "config", "user.email", "test@example.com")
	runGit(t, proj, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(proj, "tracked.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, proj, "add", ".")
	runGit(t, proj, "commit", "-m", "init")
	stray := filepath.Join(proj, "stray.txt")
	if err := os.WriteFile(stray, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, w := call(remoteFSTarget(t, "/api/git/hunk", nil),
		map[string]any{"path": "stray.txt", "hunk_index": 0, "action": "discard"})
	h.HandleGitHunk(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("discard untracked failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatalf("untracked file still present after discard: %v", err)
	}
}

// A ?project= inside a registered project is legal: the file tree browses into
// a subfolder and sends it as the project, and the operation is anchored there.
func TestRemoteFSSubProjectAccepted(t *testing.T) {
	h, proj := tildeRemoteProject(t)
	sub := filepath.Join(proj, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	r, w := call(remoteFSTargetProject("~/www/aimsweb/src", "/api/fs/new-file", nil),
		map[string]any{"path": "~/www/aimsweb/src/main.go"})
	h.HandleFSNewFile(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("new-file with a sub-project failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(sub, "main.go")); err != nil {
		t.Fatalf("created file missing in the sub-project: %v", err)
	}
}

func TestRemoteFSSubProjectOutsideRejected(t *testing.T) {
	h, _ := tildeRemoteProject(t)
	r, w := call(remoteFSTargetProject("~/www/other", "/api/fs/new-file", nil),
		map[string]any{"path": "~/www/other/x.txt"})
	h.HandleFSNewFile(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unregistered project path, got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not a remote project registered") {
		t.Fatalf("error should name the admission rule, got %s", w.Body.String())
	}
}

// A symlinked subdirectory must not become a MUTATION anchor: the read path
// tolerates it lexically so linked folders can be browsed, but a write anchored
// there would land outside the project.
func TestRemoteFSSubProjectSymlinkEscapeRejected(t *testing.T) {
	h, proj := tildeRemoteProject(t)
	outside := filepath.Join(filepath.Dir(proj), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(proj, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	r, w := call(remoteFSTargetProject("~/www/aimsweb/link", "/api/fs/new-file", nil),
		map[string]any{"path": "~/www/aimsweb/link/escaped.txt"})
	h.HandleFSNewFile(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a symlinked sub-project, got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "outside its registered project") {
		t.Fatalf("error should name the containment rule, got %s", w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.txt")); !os.IsNotExist(err) {
		t.Fatalf("a file was created outside the project: %v", err)
	}
}
