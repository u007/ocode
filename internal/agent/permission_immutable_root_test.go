package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestIsImmutableReadRoot_GoModCache(t *testing.T) {
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOPATH", "/Users/james/go")

	cases := []struct {
		path string
		want bool
	}{
		{"/Users/james/go/pkg/mod/charm.land/bubbles/v2@v2.1.0/viewport", true},
		{"/Users/james/go/pkg/mod", true},
		{"/Users/james/go/pkg/modular", false}, // prefix-boundary: not the cache
		{"/Users/james/go/src/foo", false},
		{"/etc/passwd", false},
	}
	for _, c := range cases {
		if got := isImmutableReadRoot(c.path); got != c.want {
			t.Errorf("isImmutableReadRoot(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIsImmutableReadRoot_GOMODCACHE_overrides(t *testing.T) {
	custom := filepath.Join("/tmp", "modcache")
	t.Setenv("GOMODCACHE", custom)
	t.Setenv("GOPATH", "/Users/james/go")

	if !isImmutableReadRoot(filepath.Join(custom, "foo")) {
		t.Error("GOMODCACHE root should be recognized")
	}
	if isImmutableReadRoot("/Users/james/go/pkg/mod/foo") {
		t.Error("GOPATH cache should be ignored when GOMODCACHE is set")
	}
}

// languageDepRoots uses os.UserHomeDir. For tests we override it by setting
// HOME (or USERPROFILE on Windows) so the results are deterministic.
func setHomeForTest(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	if platform := os.Getenv("GOOS"); platform == "windows" || platform == "" {
		t.Setenv("USERPROFILE", home)
	}
}

func TestLanguageDepRoots_GoModCache(t *testing.T) {
	// Clear env vars that affect goModCacheRoots.
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOPATH", "/users/test/go")
	roots := languageDepRoots()
	found := false
	for _, r := range roots {
		if strings.HasSuffix(r, "/users/test/go/pkg/mod") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("languageDepRoots should include Go module cache, got %v", roots)
	}
}

func TestLanguageDepRoots_DefaultHomePaths(t *testing.T) {
	setHomeForTest(t, "/home/testuser")
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOPATH", "")
	t.Setenv("npm_config_cache", "")
	t.Setenv("YARN_CACHE_FOLDER", "")
	t.Setenv("CARGO_HOME", "")
	t.Setenv("PIP_CACHE_DIR", "")
	t.Setenv("GRADLE_USER_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	roots := languageDepRoots()

	expected := []string{
		"/home/testuser/.npm/_cacache",
		"/home/testuser/.local/share/pnpm/store",
		"/home/testuser/.pnpm-store",
		"/home/testuser/.yarn/berry/cache",
		"/home/testuser/.cargo/registry",
		"/home/testuser/.cache/pip",
		"/home/testuser/.m2/repository",
		"/home/testuser/.gradle/caches",
		"/home/testuser/go/pkg/mod",
		"/usr/local/go",
		"/usr/lib/go",
		"/home/testuser/.gem",
		"/home/testuser/.cache/composer",
		"/home/testuser/.composer",
		"/usr/lib/ruby/gems",
		"/usr/local/lib/ruby/gems",
		"/usr/lib/jvm",
		"/Library/Java/JavaVirtualMachines",
	}

	for _, exp := range expected {
		found := false
		for _, r := range roots {
			if r == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("languageDepRoots missing expected path: %s; got %v", exp, roots)
		}
	}
}

func TestLanguageDepRoots_RespectsEnvVars(t *testing.T) {
	setHomeForTest(t, "/home/testuser")
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOPATH", "")
	t.Setenv("npm_config_cache", "/custom/npm/cache")
	t.Setenv("YARN_CACHE_FOLDER", "/custom/yarn/cache")
	t.Setenv("CARGO_HOME", "/custom/cargo")
	t.Setenv("PIP_CACHE_DIR", "/custom/pip/cache")
	t.Setenv("GRADLE_USER_HOME", "/custom/gradle")
	t.Setenv("GEM_HOME", "/custom/gems")
	t.Setenv("COMPOSER_HOME", "/custom/composer")
	t.Setenv("JAVA_HOME", "/custom/java")
	t.Setenv("GOROOT", "/custom/go")

	roots := languageDepRoots()

	expected := []string{
		"/custom/npm/cache",
		"/custom/yarn/cache",
		"/custom/cargo/registry",
		"/custom/pip/cache",
		"/custom/gradle/caches",
		"/home/testuser/.local/share/pnpm/store",
		"/home/testuser/.pnpm-store",
		"/home/testuser/.m2/repository",
		"/custom/gems",
		"/custom/composer/cache",
		"/custom/java",
		"/custom/go",
	}

	for _, exp := range expected {
		found := false
		for _, r := range roots {
			if r == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("languageDepRoots missing %s; got %v", exp, roots)
		}
	}

	// When env vars are set, the fallback home paths should NOT appear.
	// Ruby ~/.gem is always a valid gem path (gempaths are additive) so it
	// stays even when GEM_HOME is set.
	notExpected := []string{
		"/home/testuser/.npm/_cacache",
		"/home/testuser/.yarn/berry/cache",
		"/home/testuser/.cargo/registry",
		"/home/testuser/.cache/pip",
		"/home/testuser/.gradle/caches",
		"/home/testuser/.cache/composer",
		"/home/testuser/.composer",
		"/usr/local/go",
		"/usr/lib/go",
	}
	for _, ne := range notExpected {
		for _, r := range roots {
			if r == ne {
				t.Errorf("languageDepRoots should not contain %s when env var overrides it", ne)
			}
		}
	}
}

func TestLanguageDepRoots_XDGCacheHome(t *testing.T) {
	setHomeForTest(t, "/home/testuser")
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOPATH", "")
	t.Setenv("XDG_CACHE_HOME", "/xdg/cache")
	t.Setenv("npm_config_cache", "")
	t.Setenv("PIP_CACHE_DIR", "")

	roots := languageDepRoots()

	found := false
	for _, r := range roots {
		if r == "/xdg/cache/pip" || r == "/xdg/cache/npm/_cacache" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("languageDepRoots should include XDG_CACHE_HOME paths; got %v", roots)
	}
}

func TestLanguageDepRoots_Sorted(t *testing.T) {
	roots := languageDepRoots()
	if !sort.StringsAreSorted(roots) {
		t.Errorf("languageDepRoots should return roots in sorted order; got %v", roots)
	}
}

func TestIsImmutableReadRoot_LanguageDeps(t *testing.T) {
	setHomeForTest(t, "/home/testuser")
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOPATH", "")
	t.Setenv("npm_config_cache", "")
	t.Setenv("YARN_CACHE_FOLDER", "")
	t.Setenv("CARGO_HOME", "")
	t.Setenv("PIP_CACHE_DIR", "")
	t.Setenv("GRADLE_USER_HOME", "")

	cases := []struct {
		path string
		want bool
	}{
		// Go module cache
		{"/home/testuser/go/pkg/mod/github.com/foo/bar@v1.0.0/file.go", true},
		// npm cache
		{"/home/testuser/.npm/_cacache/content-v2/sha512/abc123", true},
		{"/home/testuser/.npm/_cacache", true},
		// yarn berry cache
		{"/home/testuser/.yarn/berry/cache/lodash-4.17.21.zip", true},
		// pnpm store
		{"/home/testuser/.local/share/pnpm/store/v3/files/abc/def", true},
		{"/home/testuser/.pnpm-store/v3/files/abc/def", true},
		// cargo registry
		{"/home/testuser/.cargo/registry/cache/foo/bar-1.0.0.crate", true},
		// pip cache
		{"/home/testuser/.cache/pip/http/abc123", true},
		// maven
		{"/home/testuser/.m2/repository/com/foo/bar/1.0/bar-1.0.jar", true},
		// gradle
		{"/home/testuser/.gradle/caches/modules-2/files/foo/bar.jar", true},
		// Go GOROOT standard library
		{"/usr/local/go/src/fmt/format.go", true},
		{"/usr/lib/go/src/net/http/server.go", true},
		// Ruby gem paths
		{"/usr/lib/ruby/gems/3.2.0/gems/rails-7.0.0/lib/foo.rb", true},
		{"/usr/local/lib/ruby/gems/3.2.0/gems/bundler/bundler.rb", true},
		{"/home/testuser/.gem/ruby/3.2.0/gems/foo-1.0/lib/foo.rb", true},
		// Java JDK
		{"/usr/lib/jvm/java-17-openjdk-amd64/lib/src.zip", true},
		{"/Library/Java/JavaVirtualMachines/jdk-17.jdk/Contents/Home/lib/src.zip", true},
		// false positives
		{"/home/testuser/.npm/other/file", false},
		{"/home/testuser/.cargo/bin/cargo", false},
		{"/home/testuser/.cache/other/thing", false},
		{"/home/testuser/.gradle/other/file", false},
		{"/home/testuser/go/src/foo", false},
		{"/etc/passwd", false},
		{"/usr/local/bin/curl", false},
		{"/usr/lib/firmware/foo", false},
	}
	for _, c := range cases {
		if got := isImmutableReadRoot(c.path); got != c.want {
			t.Errorf("isImmutableReadRoot(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIsImmutableReadRoot_LanguageDeps_WithEnvOverrides(t *testing.T) {
	setHomeForTest(t, "/home/testuser")
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOPATH", "")
	t.Setenv("npm_config_cache", "/custom/npm")
	t.Setenv("CARGO_HOME", "/custom/rust")

	if !isImmutableReadRoot("/custom/npm/content-v2/sha/abc") {
		t.Error("env-overridden npm cache should be recognized")
	}
	if !isImmutableReadRoot("/custom/rust/registry/cache/foo.crate") {
		t.Error("env-overridden cargo registry should be recognized")
	}
	// Fallback paths should NOT match when env var is set.
	if isImmutableReadRoot("/home/testuser/.npm/_cacache/foo") {
		t.Error("home npm cache should NOT match when npm_config_cache overrides it")
	}
}
