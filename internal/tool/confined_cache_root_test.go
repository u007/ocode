package tool

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/pathscope"
)

// setHomeTree points HOME and every XDG/Windows equivalent at one tree so path
// resolution is identical on darwin and Linux. internal/config and
// internal/agent have the same helper; this package had none, and a bare
// t.Setenv("HOME", …) shares the process-wide XDG variables on Linux.
func setHomeTree(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
}

// cacheRootTestCtx returns a context whose working directory is NOT the fake
// home, together with a home tree that is NOT under a temp root.
//
// Both properties are load-bearing:
//
//   - The home must sit outside os.TempDir()/ /tmp /var/tmp (and on darwin
//     /var/folders/*/*), because confinedPath returns early for any temp path.
//     A t.TempDir() home would therefore make the assertions below pass even
//     with the bug present. The package directory is the portable non-temp base.
//   - The working directory must not contain the home, otherwise the home is
//     inside the workdir and every path under it is allowed for the wrong
//     reason — the workdir check runs before the managed-cache roots.
func cacheRootTestCtx(t *testing.T) (context.Context, string) {
	t.Helper()
	home, err := os.MkdirTemp(".", ".ocode-lazy-cache-home-")
	if err != nil {
		t.Fatalf("create fake home: %v", err)
	}
	home, err = filepath.Abs(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	if pathscope.IsTempDir(home) {
		t.Fatalf("fake home %s is under a temp root; the test would be vacuous", home)
	}
	setHomeTree(t, home)
	return WithWorkDir(context.Background(), t.TempDir()), home
}

// TestConfinedPathAllowsLazilyCreatedCacheRoot is the regression test for the
// darwin-only pass on a fresh machine. toolResultCacheDir and repoCacheDir are
// created lazily on first use, so confinedPath must accept them even when
// neither the directory nor its parent exists yet: the permission layer's
// AllowedRoots already auto-authorizes them (CacheRoots normalizes lazily), so
// an eager lookup here made an auto-grant hard-error with "path … is outside
// the working directory" on any fresh install.
func TestConfinedPathAllowsLazilyCreatedCacheRoot(t *testing.T) {
	ctx, _ := cacheRootTestCtx(t)

	cacheDir := toolResultCacheDir()
	if pathscope.IsTempDir(cacheDir) {
		// A temp-rooted cache dir short-circuits confinedPath before the
		// managed-cache check, so the assertion below would be vacuous.
		t.Fatalf("cache dir %s resolved under a temp root; test would be vacuous", cacheDir)
	}
	if _, err := os.Stat(cacheDir); err == nil {
		t.Fatalf("precondition: cache dir %s must not exist yet", cacheDir)
	}
	// The parent must be missing too — that is the exact condition the eager
	// normalizer could not survive.
	if _, err := os.Stat(filepath.Dir(cacheDir)); err == nil {
		t.Fatalf("precondition: parent of %s must not exist yet", cacheDir)
	}

	probe := filepath.Join(cacheDir, "truncated-output.txt")
	got, err := confinedPath(ctx, probe)
	if err != nil {
		t.Fatalf("confinedPath(%q) error: %v", probe, err)
	}
	// got is symlink-resolved, so compare its basename rather than the literal
	// probe (a symlinked /Users or /var would otherwise fail spuriously).
	if filepath.Base(got) != filepath.Base(probe) {
		t.Errorf("confinedPath(%q) = %q, want basename %q", probe, got, filepath.Base(probe))
	}
}

// TestConfinedPathRejectsSymlinkEscapingCacheRoot bounds the previous test: the
// managed cache root must remain the boundary even once it exists. A symlink
// planted inside it must not carry a path outside.
func TestConfinedPathRejectsSymlinkEscapingCacheRoot(t *testing.T) {
	ctx, home := cacheRootTestCtx(t)

	cacheDir := toolResultCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(cacheDir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	probe := filepath.Join(link, "secret.txt")
	if got, err := confinedPath(ctx, probe); err == nil {
		t.Fatalf("confinedPath(%q) = %q; a symlink escaped the managed cache root", probe, got)
	}
}

// TestConfinedPathStillRejectsSiblingOfLazyCacheRoot guards against widening:
// accepting a not-yet-created cache root must not admit its siblings.
func TestConfinedPathStillRejectsSiblingOfLazyCacheRoot(t *testing.T) {
	ctx, home := cacheRootTestCtx(t)

	probe := filepath.Join(home, ".local", "state", "elsewhere", "secret.txt")
	if got, err := confinedPath(ctx, probe); err == nil {
		t.Fatalf("confinedPath(%q) = %q; confinement widened past the managed cache root", probe, got)
	}
}

// TestCacheRootsNeverEmpty pins the invariant confinedPath's loop relies on: an
// empty root string would make pathWithinRoot match every absolute path.
func TestCacheRootsNeverEmpty(t *testing.T) {
	for _, root := range CacheRoots() {
		if root == "" {
			t.Fatalf("CacheRoots returned an empty root; it would match every path")
		}
		if !filepath.IsAbs(root) {
			t.Fatalf("CacheRoots returned non-absolute root %q", root)
		}
	}
}
