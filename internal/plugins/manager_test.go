package plugins

import (
	"os"
	"path/filepath"
	"testing"
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
