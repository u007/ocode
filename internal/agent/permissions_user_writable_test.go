package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUserWritableRoots_ContainsCacheAndBins(t *testing.T) {
	// Use a real temp home so EvalSymlinks / resolveForScopeCheck canonicalization is consistent.
	// /home is a symlink to /System/Volumes/Data/home on macOS, which breaks pathUnderRoot when HOME is /home/testuser.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("CARGO_HOME", "")
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	roots := userWritableRoots()
	mustContain := []string{
		filepath.Join(home, ".cache"),
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "bin"),
		filepath.Join(home, ".cargo", "bin"),
		filepath.Join(home, "go", "bin"),
		filepath.Join(home, ".cache", "go-build"),
		filepath.Join(home, ".cache", "uv"),
	}
	for _, exp := range mustContain {
		found := false
		for _, r := range roots {
			if r == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("userWritableRoots missing %q; got %v", exp, roots)
		}
	}
	// Language dep roots must NOT have been broadened to whole ~/.cache.
	for _, r := range languageDepRoots() {
		if r == filepath.Join(home, ".cache") {
			t.Errorf("languageDepRoots should not contain whole ~/.cache (separate concerns); got %v", languageDepRoots())
		}
	}
	if isImmutableReadRoot(filepath.Join(home, ".cache", "foo")) {
		t.Error("isImmutableReadRoot should not allow whole ~/.cache; only targeted subdirs")
	}
	// But the writable set should make home/.cache in-scope for permission decisions (sandbox writable).
	pm := NewPermissionManager()
	pm.SetWorkDir(filepath.Join(home, "project"))
	if err := os.MkdirAll(filepath.Join(home, "project"), 0755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if !pm.IsPathWithinAllowedRoots(filepath.Join(home, ".cache", "uv", "foo")) {
		t.Error("IsPathWithinAllowedRoots should allow home/.cache/uv/foo via userWritableRoots")
	}
	if !pm.IsPathWithinAllowedRoots(filepath.Join(home, ".cache", "foo")) {
		t.Error("IsPathWithinAllowedRoots should allow home/.cache/foo via whole cache root")
	}
	// Also verify the explicit /Users/james/.cache requirement is covered when HOME is /Users/james.
	// Simulate that home to ensure the exact requested path is present.
	setHomeForTest(t, "/Users/james")
	t.Setenv("XDG_CACHE_HOME", "")
	roots = userWritableRoots()
	found := false
	for _, r := range roots {
		if r == "/Users/james/.cache" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("userWritableRoots must contain /Users/james/.cache per explicit requirement; got %v", roots)
	}
}

func TestUserWritableRoots_XDGCacheHome(t *testing.T) {
	setHomeForTest(t, "/home/testuser")
	t.Setenv("XDG_CACHE_HOME", "/xdg/cache")
	roots := userWritableRoots()
	found := false
	for _, r := range roots {
		if r == "/xdg/cache" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("userWritableRoots should include XDG_CACHE_HOME /xdg/cache; got %v", roots)
	}
	// When XDG set, the fallback ~/.cache should not be added as separate root (dedup covers it if UserCacheDir already)
	// but we at least ensure XDG is present.
}

func TestUserWritableRoots_ValidatesEnvPaths(t *testing.T) {
	setHomeForTest(t, "/home/testuser")
	// CARGO_HOME outside $HOME must be rejected.
	t.Setenv("CARGO_HOME", "/tmp/custom-cargo")
	roots := userWritableRoots()
	for _, r := range roots {
		if r == "/tmp/custom-cargo/bin" {
			t.Errorf("userWritableRoots should reject CARGO_HOME outside $HOME: got %v", roots)
		}
	}
	// CARGO_HOME under $HOME should be allowed.
	t.Setenv("CARGO_HOME", "/home/testuser/customcargo")
	roots = userWritableRoots()
	found := false
	for _, r := range roots {
		if r == "/home/testuser/customcargo/bin" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("userWritableRoots should allow CARGO_HOME under $HOME; got %v", roots)
	}
	// System CARGO_HOME must be rejected.
	t.Setenv("CARGO_HOME", "/usr/local/cargo")
	roots = userWritableRoots()
	for _, r := range roots {
		if r == "/usr/local/cargo/bin" {
			t.Errorf("should reject system CARGO_HOME /usr/local/cargo")
		}
	}
	// GOBIN valid under home
	t.Setenv("CARGO_HOME", "")
	t.Setenv("GOBIN", "/home/testuser/mygobin")
	roots = userWritableRoots()
	found = false
	for _, r := range roots {
		if r == "/home/testuser/mygobin" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("GOBIN under $HOME should be allowed; got %v", roots)
	}
	// GOBIN system path rejected
	t.Setenv("GOBIN", "/usr/local/bin")
	roots = userWritableRoots()
	for _, r := range roots {
		if r == "/usr/local/bin" {
			t.Errorf("GOBIN system path /usr/local/bin must be rejected")
		}
	}
	// GOBIN "/" rejected
	t.Setenv("GOBIN", "/")
	roots = userWritableRoots()
	for _, r := range roots {
		if r == "/" {
			t.Errorf("GOBIN / must be rejected")
		}
	}
	// GOBIN outside home rejected
	t.Setenv("GOBIN", "/tmp/gobin")
	roots = userWritableRoots()
	for _, r := range roots {
		if r == "/tmp/gobin" {
			t.Errorf("GOBIN outside home /tmp/gobin should be rejected")
		}
	}
	// GOPATH handling: valid under home
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "/home/testuser/gopath1:/home/testuser/gopath2")
	roots = userWritableRoots()
	for _, exp := range []string{"/home/testuser/gopath1/bin", "/home/testuser/gopath2/bin", "/home/testuser/go/bin"} {
		found := false
		for _, r := range roots {
			if r == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("GOPATH bin %q should be present; got %v", exp, roots)
		}
	}
	// GOPATH system path rejected
	t.Setenv("GOPATH", "/usr/local/go:/home/testuser/gopath")
	roots = userWritableRoots()
	for _, r := range roots {
		if r == "/usr/local/go/bin" {
			t.Errorf("system GOPATH /usr/local/go/bin must be rejected")
		}
	}
	found = false
	for _, r := range roots {
		if r == "/home/testuser/gopath/bin" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("valid GOPATH under home should be allowed; got %v", roots)
	}
	// XDG_CACHE_HOME "/" rejected
	t.Setenv("GOPATH", "")
	t.Setenv("XDG_CACHE_HOME", "/")
	roots = userWritableRoots()
	for _, r := range roots {
		if r == "/" {
			t.Errorf("XDG_CACHE_HOME / must be rejected")
		}
	}
}

func TestUserWritableRoots_DeduplicationAndSorted(t *testing.T) {
	setHomeForTest(t, "/home/testuser")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("CARGO_HOME", "")
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	roots := userWritableRoots()
	// Sorted
	for i := 1; i < len(roots); i++ {
		if roots[i-1] > roots[i] {
			t.Errorf("userWritableRoots not sorted: %v", roots)
		}
	}
	seen := map[string]struct{}{}
	for _, r := range roots {
		if _, ok := seen[r]; ok {
			t.Errorf("duplicate root %q", r)
		}
		seen[r] = struct{}{}
	}
}

func TestAllowedRootsClassified_WritableSeparation(t *testing.T) {
	setHomeForTest(t, "/home/testuser")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("CARGO_HOME", "")
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	pm := NewPermissionManager()
	pm.SetWorkDir("/home/testuser/project")
	specs := pm.AllowedRootsClassified()
	// Find writable entries for our new roots
	mustWritable := []string{
		"/home/testuser/.cache",
		"/home/testuser/.local/bin",
		"/home/testuser/.cargo/bin",
	}
	for _, want := range mustWritable {
		found := false
		for _, s := range specs {
			if s.Path == want {
				found = true
				if !s.Writable {
					t.Errorf("AllowedRootsClassified %q should be writable", want)
				}
				break
			}
		}
		if !found {
			t.Errorf("AllowedRootsClassified missing %q; got %v", want, specs)
		}
	}
	// Ensure system dep roots remain in classified set but check at least one is present
	found := false
	for _, s := range specs {
		if s.Path == "/usr/local/go" {
			found = true
			if !s.Writable {
				t.Errorf("/usr/local/go should be classified writable (languageDepRoots contract)")
			}
		}
	}
	if !found {
		t.Logf("warning: /usr/local/go not in classified set on this platform")
	}
	// Ensure / is never writable
	for _, s := range specs {
		if s.Path == "/" && s.Writable {
			t.Errorf("writable filesystem root must be rejected")
		}
	}
	// Check UserCacheDir is writable if present
	if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" && cacheDir != "/" {
		found := false
		for _, s := range specs {
			if s.Path == filepath.Clean(cacheDir) {
				found = true
				if !s.Writable {
					t.Errorf("UserCacheDir %q should be writable", cacheDir)
				}
			}
		}
		if !found {
			t.Errorf("UserCacheDir %q missing from classified set; specs=%v", cacheDir, specs)
		}
	}
	// Darwin-specific cache
	if runtime.GOOS == "darwin" {
		found := false
		for _, s := range specs {
			if s.Path == "/home/testuser/Library/Caches" {
				found = true
				if !s.Writable {
					t.Errorf("Library/Caches should be writable on darwin")
				}
			}
		}
		if !found {
			t.Errorf("Library/Caches missing on darwin")
		}
	}
}

func TestAllowedRoots_IncludesUserWritable(t *testing.T) {
	setHomeForTest(t, "/home/testuser")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("CARGO_HOME", "")
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	pm := NewPermissionManager()
	pm.SetWorkDir("/home/testuser/project")
	roots := pm.AllowedRoots()
	for _, want := range []string{"/home/testuser/.cache", "/home/testuser/.local/bin"} {
		found := false
		for _, r := range roots {
			if r == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AllowedRoots missing %q; got %v", want, roots)
		}
	}
}
