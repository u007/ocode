package plugins

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestInstallLocal(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "plugin.json"), []byte(`{"name":"test","description":"Test plugin"}`), 0644); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	p, err := InstallLocal(src, dest)
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	if p.Name != "test" {
		t.Errorf("got name %q, want %q", p.Name, "test")
	}
	if _, err := os.Stat(filepath.Join(dest, "plugin.json")); err != nil {
		t.Errorf("plugin.json not found in dest: %v", err)
	}
}

func TestInstallLocalMissingManifest(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	if _, err := InstallLocal(src, dest); err == nil {
		t.Error("expected error for missing plugin.json")
	}
}

func TestRemovePlugin(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "myplugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := Remove(pluginDir); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(pluginDir); !os.IsNotExist(err) {
		t.Error("plugin directory still exists after remove")
	}
}

func TestNormaliseGitURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"github.com/user/repo", "https://github.com/user/repo"},
		{"https://github.com/user/repo", "https://github.com/user/repo"},
		{"https://github.com/user/repo.git", "https://github.com/user/repo.git"},
	}
	for _, c := range cases {
		got := normaliseGitURL(c.in)
		if got != c.want {
			t.Errorf("normaliseGitURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestInstallDirName(t *testing.T) {
	cases := []struct{ url, want string }{
		{"https://github.com/user/repo", "user-repo"},
		{"https://github.com/user/repo.git", "user-repo"},
	}
	for _, c := range cases {
		got := installDirName(c.url)
		if got != c.want {
			t.Errorf("installDirName(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestRunOnInstallEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := RunOnInstall(dir, Plugin{}); err != nil {
		t.Fatalf("RunOnInstall with empty plugin: %v", err)
	}
}

func TestRunOnInstallValidation(t *testing.T) {
	dir := t.TempDir()
	p := Plugin{OnInstall: []string{"rm; evil"}}
	err := RunOnInstall(dir, p)
	if err == nil {
		t.Error("expected error for command containing shell metacharacter")
	}
}

func TestResolveCommitHashSupportsAbbreviatedRefs(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	file := filepath.Join(dir, "plugin.json")
	if err := os.WriteFile(file, []byte(`{"name":"sample"}`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := wt.Add("plugin.json"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	hash, err := wt.Commit("initial", &gogit.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	got, err := resolveCommitHash(repo, hash.String()[:7])
	if err != nil {
		t.Fatalf("resolveCommitHash: %v", err)
	}
	if got != hash {
		t.Fatalf("resolveCommitHash abbreviated ref = %s, want %s", got, hash)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return string(out)
}

func TestCheckSyncAnnotatedTagUsesCommitHash(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	remote := filepath.Join(root, "remote.git")
	clone := filepath.Join(root, "clone")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "init")
	runGit(t, src, "config", "user.email", "test@example.com")
	runGit(t, src, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(src, "plugin.json"), []byte(`{"name":"sample"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "add", "plugin.json")
	runGit(t, src, "commit", "-m", "initial")
	runGit(t, src, "tag", "-a", "v1.0.0", "-m", "annotated")
	runGit(t, src, "clone", "--bare", ".", remote)
	runGit(t, root, "clone", remote, clone)

	got := CheckSync(clone, "", "v1.0.0")
	if got.State != SyncUpToDate {
		t.Fatalf("CheckSync state = %s, want %s (local=%s remote=%s msg=%s)", got.State, SyncUpToDate, got.LocalHash, got.RemoteHash, got.Message)
	}
}
