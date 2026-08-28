package plugins

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/u007/ocode/internal/bundled"
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

// TestRemoveRejectsBundledDir ensures a bundled/embedded plugin directory is
// protected from deletion (deleting it would destroy the embedded copy shipped
// inside the binary).
func TestRemoveRejectsBundledDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	bundledDir := t.TempDir()
	pluginDir := filepath.Join(bundledDir, "bundledplugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"bundledplugin"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := bundled.PluginsDir
	bundled.PluginsDir = bundledDir
	defer func() { bundled.PluginsDir = prev }()

	if !IsBundledPluginDir(pluginDir) {
		t.Fatalf("IsBundledPluginDir(%q) = false, want true", pluginDir)
	}
	if err := Remove(pluginDir); err == nil {
		t.Fatal("Remove on a bundled dir succeeded; want rejection")
	}
	// The directory must remain intact.
	if _, err := os.Stat(pluginDir); err != nil {
		t.Fatalf("bundled plugin dir was deleted: %v", err)
	}
}

// TestIsBundledPluginDirDoesNotMatchSibling verifies the guard uses a real
// path-prefix check, not a naive string suffix, so a directory named like the
// bundled root (e.g. "bundledX") is not mistakenly protected.
func TestIsBundledPluginDirDoesNotMatchSibling(t *testing.T) {
	bundledDir := filepath.Join(t.TempDir(), "bundled")
	sibling := filepath.Join(t.TempDir(), "bundledX", "plugin")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}

	prev := bundled.PluginsDir
	bundled.PluginsDir = bundledDir
	defer func() { bundled.PluginsDir = prev }()

	if IsBundledPluginDir(sibling) {
		t.Fatalf("IsBundledPluginDir(%q) = true for non-bundled sibling; want false", sibling)
	}
	// A real nested path under the bundled root is flagged.
	nested := filepath.Join(bundledDir, "plugin")
	if !IsBundledPluginDir(nested) {
		t.Fatalf("IsBundledPluginDir(%q) = false for nested bundled path; want true", nested)
	}
}

// TestLoadPluginsPopulatesDir verifies the on-disk directory is surfaced on the
// returned Plugin so callers (UI, enable/disable, remove) can act on it.
func TestLoadPluginsPopulatesDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	bundledDir := t.TempDir()
	pluginDir := filepath.Join(bundledDir, "dirdemo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"dirdemo"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := bundled.PluginsDir
	bundled.PluginsDir = bundledDir
	defer func() { bundled.PluginsDir = prev }()

	found := false
	for _, p := range LoadPlugins(nil) {
		if p.Name == "dirdemo" {
			found = true
			if p.Dir != pluginDir {
				t.Fatalf("Plugin.Dir = %q, want %q", p.Dir, pluginDir)
			}
		}
	}
	if !found {
		t.Fatal("dirdemo not returned by LoadPlugins")
	}

	// FindPluginDir should resolve the same directory by name.
	if got := FindPluginDir("dirdemo"); got != pluginDir {
		t.Fatalf("FindPluginDir = %q, want %q", got, pluginDir)
	}
	if got := FindPluginDir("does-not-exist"); got != "" {
		t.Fatalf("FindPluginDir(missing) = %q, want empty", got)
	}
}

// TestLoadPluginsPrecedenceAndDedup verifies that when the same plugin name
// exists in both a global install dir and the bundled tree, the higher-precedence
// (disk) copy wins and is returned exactly once.
func TestLoadPluginsPrecedenceAndDedup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	// Global install dir plugin (higher precedence than bundled).
	globalDir := filepath.Join(home, ".config", "opencode", "plugins", "dup")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "plugin.json"), []byte(`{"name":"dup","description":"from-global"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Bundled copy with a different description.
	bundledDir := t.TempDir()
	bundledDup := filepath.Join(bundledDir, "dup")
	if err := os.MkdirAll(bundledDup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundledDup, "plugin.json"), []byte(`{"name":"dup","description":"from-bundled"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := bundled.PluginsDir
	bundled.PluginsDir = bundledDir
	defer func() { bundled.PluginsDir = prev }()

	loaded := LoadPlugins(nil)
	count := 0
	var got Plugin
	for _, p := range loaded {
		if p.Name == "dup" {
			count++
			got = p
		}
	}
	if count != 1 {
		t.Fatalf("expected dup plugin returned once, got %d", count)
	}
	if got.Description != "from-global" {
		t.Errorf("expected higher-precedence global copy, got description %q", got.Description)
	}
	if got.Dir != globalDir {
		t.Errorf("expected Dir to be the global path, got %q", got.Dir)
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

func TestValidateRemovableDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	// Valid path under the global install root.
	validDir := filepath.Join(home, ".config", "opencode", "plugins", "myplugin")
	if err := os.MkdirAll(validDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRemovableDir(validDir); err != nil {
		t.Errorf("ValidateRemovableDir(%q) = %v, want nil", validDir, err)
	}

	// Absolute path outside any plugin root should be rejected.
	if err := ValidateRemovableDir("/etc/passwd"); err == nil {
		t.Error("ValidateRemovableDir(\"/etc/passwd\") = nil, want error")
	}

	// Path with traversal sequences should be rejected.
	if err := ValidateRemovableDir(filepath.Join(home, ".config", "opencode", "plugins", "..", "..", "etc", "passwd")); err == nil {
		t.Error("ValidateRemovableDir with traversal = nil, want error")
	}

	// Relative path (non-absolute after Abs resolution) that escapes — still
	// rejected because it resolves outside any plugin root.
	if err := ValidateRemovableDir("../../../etc/passwd"); err == nil {
		t.Error("ValidateRemovableDir with relative escape = nil, want error")
	}
}

func TestValidateRemovableDirRejectsRootAndNestedTargets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())
	root := filepath.Join(home, ".config", "opencode", "plugins")
	nested := filepath.Join(root, "plugin", "private")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ValidateRemovableDir(root); err == nil {
		t.Fatal("approved plugin root must not be removable")
	}
	if err := ValidateRemovableDir(nested); err == nil {
		t.Fatal("nested path inside a plugin must not be a removal target")
	}
	if err := ValidateRemovableDir(filepath.Join(root, "plugin")); err != nil {
		t.Fatalf("direct plugin child should be removable: %v", err)
	}
}

func TestValidateRemovableDirBundledRoot(t *testing.T) {
	// A path under the bundled plugins root should be accepted.
	bundledDir := t.TempDir()
	pluginDir := filepath.Join(bundledDir, "testplugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prev := bundled.PluginsDir
	bundled.PluginsDir = bundledDir
	defer func() { bundled.PluginsDir = prev }()

	if err := ValidateRemovableDir(pluginDir); err != nil {
		t.Errorf("ValidateRemovableDir under bundled root = %v, want nil", err)
	}
}
