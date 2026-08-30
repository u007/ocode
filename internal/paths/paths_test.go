package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGlobalDataDir(t *testing.T) {
	dir, err := GlobalDataDir()
	if err != nil {
		t.Fatalf("GlobalDataDir() error: %v", err)
	}
	if dir == "" {
		t.Fatal("GlobalDataDir() returned empty string")
	}

	// Verify directory was created
	if info, err := os.Stat(dir); err != nil {
		t.Fatalf("GlobalDataDir() dir does not exist: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("GlobalDataDir() path is not a directory: %s", dir)
	}

	// Verify platform-specific expectations
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		expected := filepath.Join(home, ".local", "share", AppName)
		if dir != expected {
			t.Errorf("macOS: got %s, want %s", dir, expected)
		}
	case "linux":
		xdg := os.Getenv("XDG_DATA_HOME")
		var expected string
		if xdg != "" {
			expected = filepath.Join(xdg, AppName)
		} else {
			expected = filepath.Join(home, ".local", "share", AppName)
		}
		if dir != expected {
			t.Errorf("linux: got %s, want %s", dir, expected)
		}
	}
}

func TestProjectSessionsDir(t *testing.T) {
	dir, err := ProjectSessionsDir("abc123")
	if err != nil {
		t.Fatalf("ProjectSessionsDir() error: %v", err)
	}
	base, _ := GlobalDataDir()
	expected := filepath.Join(base, "project", "abc123", "sessions")
	if dir != expected {
		t.Errorf("got %s, want %s", dir, expected)
	}
}

func TestUsageDir(t *testing.T) {
	dir, err := UsageDir()
	if err != nil {
		t.Fatalf("ProjectUsageDir() error: %v", err)
	}
	base, _ := GlobalDataDir()
	expected := filepath.Join(base, "usage")
	if dir != expected {
		t.Errorf("got %s, want %s", dir, expected)
	}
}

func TestProjectSlug(t *testing.T) {
	// Deterministic for the same input.
	a := ProjectSlug("/some/project")
	b := ProjectSlug("/some/project")
	if a != b {
		t.Fatalf("slug not deterministic: %q != %q", a, b)
	}
	// 12-char hex.
	if len(a) != 12 {
		t.Fatalf("slug length = %d, want 12", len(a))
	}
	for _, c := range a {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("slug %q contains non-hex char %q", a, c)
		}
	}
	// Different paths yield different slugs (they are not in the same git repo
	// within this test, so gitToplevel falls back to the path itself).
	c := ProjectSlug("/other/project")
	if c == a {
		t.Fatalf("different paths produced identical slug %q", a)
	}
	// Empty input falls back to the current working directory and still works.
	if got := ProjectSlug(""); got == "" {
		t.Fatalf("empty input produced empty slug")
	}
	// On Windows the slug is case-insensitive.
	if runtime.GOOS == "windows" {
		if ProjectSlug("C:\\Proj") != ProjectSlug("c:\\proj") {
			t.Fatalf("windows slug is not case-insensitive")
		}
	}
}

func TestProjectSlugFollowsSymlinkAlias(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	alias := filepath.Join(root, "alias")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("symlink test unavailable: %v", err)
	}
	wantRoot, err := filepath.EvalSymlinks(alias)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", alias, err)
	}
	if got := ProjectRoot(alias); got != wantRoot {
		t.Fatalf("ProjectRoot(%q) = %q, want %q", alias, got, wantRoot)
	}
	if gotAlias, gotReal := ProjectSlug(alias), ProjectSlug(real); gotAlias != gotReal {
		t.Fatalf("ProjectSlug should ignore symlink aliases: alias=%q real=%q", gotAlias, gotReal)
	}
}

func TestProjectSlugFollowsSymlink(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link-to-target")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if gotTarget, gotLink := ProjectSlug(target), ProjectSlug(link); gotTarget != gotLink {
		t.Fatalf("symlink slug mismatch: target=%q link=%q", gotTarget, gotLink)
	}
}

func TestGlobalConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	var want string
	switch runtime.GOOS {
	case "windows":
		appdata := filepath.Join(home, "AppData", "Roaming")
		t.Setenv("APPDATA", appdata)
		want = filepath.Join(appdata, AppName)
	default:
		// macOS ignores XDG_CONFIG_HOME (mirrors the darwin branch of
		// GlobalDataDir). Linux XDG handling is covered below.
		t.Setenv("XDG_CONFIG_HOME", "")
		want = filepath.Join(home, ".config", AppName)
	}

	dir, err := GlobalConfigDir()
	if err != nil {
		t.Fatalf("GlobalConfigDir() error: %v", err)
	}
	if dir != want {
		t.Fatalf("GlobalConfigDir() = %q, want %q", dir, want)
	}
	// Must be side-effect free: the permission/confinement layers probe this on
	// every decision, and a probe must never create directories.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("GlobalConfigDir() must not create %q (stat err = %v)", dir, err)
	}

	if runtime.GOOS == "windows" {
		// Unset APPDATA is a hard error — no silent join onto an empty base
		// (the old ad-hoc resolvers produced a relative "opencode/..." path).
		t.Setenv("APPDATA", "")
		if _, err := GlobalConfigDir(); err == nil {
			t.Fatal("GlobalConfigDir() should error when APPDATA is unset")
		}
		return
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "freebsd" {
		return
	}
	xdg := filepath.Join(home, "xdg-config")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	dir, err = GlobalConfigDir()
	if err != nil {
		t.Fatalf("GlobalConfigDir(XDG) error: %v", err)
	}
	if want := filepath.Join(xdg, AppName); dir != want {
		t.Fatalf("GlobalConfigDir(XDG) = %q, want %q", dir, want)
	}
}
