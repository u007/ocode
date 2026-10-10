package cdp

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/paths"
)

// requireEmbeddedHTRAssets skips a test that exercises the bundled HTR release
// assets when the build did not embed them. The archive is produced by
// `make prepare-htr-assets` from out-of-tree sources and is never committed, so
// an ordinary source build (no `htr` tag) and CI cannot run these paths.
// Without this guard the tests resolve empty asset paths, write relative paths
// against the package directory, and fail for reasons unrelated to the change.
//
// Run the HTR asset tests with: go test -tags htr ./internal/browse/cdp/...
func requireEmbeddedHTRAssets(t *testing.T) {
	t.Helper()
	if len(embeddedHTRArchive) == 0 {
		t.Skip("HTR release assets are not embedded in this build; rebuild with -tags htr (make prepare-htr-assets) to run")
	}
}

// TestResolveHTRAssetsErrorsWhenBundleNotEmbedded pins the contract that an
// archive-less build says so instead of returning empty asset paths with a nil
// error. internal/server/htr.go reports whatever this returns as the
// user-visible notice, so a nil error here silently left HTR marked ENABLED with
// nothing bundled — the regression this test locks in.
//
// Only meaningful in a build without the `htr` tag; with the tag the archive is
// present and resolution legitimately succeeds.
func TestResolveHTRAssetsErrorsWhenBundleNotEmbedded(t *testing.T) {
	if len(embeddedHTRArchive) != 0 {
		t.Skip("built with -tags htr; the HTR archive is embedded")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	_, err := ResolveHTRAssetsForHost("", "", DefaultHTRNativeHostName)
	if err == nil {
		t.Fatal("a build with no embedded HTR archive must report an error, not empty paths with nil")
	}
	if !strings.Contains(err.Error(), "no HTR bundle") {
		t.Fatalf("error must name the missing bundle and how to get it, got: %v", err)
	}
}

// TestResolveHTRAssetsDevOverridesWithoutBundle is the other half of the
// contract: the development path must keep working without the tag, but only
// with an extension to launch. An htrcli override alone leaves no extension for
// the native host manifest, so it must error now instead of failing later at
// daemon start.
func TestResolveHTRAssetsDevOverridesWithoutBundle(t *testing.T) {
	if len(embeddedHTRArchive) != 0 {
		t.Skip("built with -tags htr; the HTR archive is embedded")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("OCODE_HTR_EXTENSION_ORIGIN", "")

	// ResolveHTRCliBinary only needs a non-directory path to exist.
	cli := filepath.Join(t.TempDir(), "htrcli")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ext := t.TempDir()

	if _, err := ResolveHTRAssetsForHost("", cli, DefaultHTRNativeHostName); err == nil || !strings.Contains(err.Error(), "no HTR bundle") {
		t.Fatalf("an htrcli override with no extension must report the missing bundle, got: %v", err)
	}

	assets, err := ResolveHTRAssetsForHost(ext, cli, DefaultHTRNativeHostName)
	if err != nil {
		t.Fatalf("extension + htrcli overrides must bypass the missing-bundle error, got: %v", err)
	}
	if assets.CliPath != cli || assets.ExtensionDir != ext {
		t.Fatalf("assets = %+v, want CliPath %q and ExtensionDir %q", assets, cli, ext)
	}

	t.Setenv("OCODE_HTR_EXTENSION_ORIGIN", "chrome-extension://abc/")
	if _, err := ResolveHTRAssetsForHost("", cli, DefaultHTRNativeHostName); err != nil {
		t.Fatalf("an explicit extension origin supplies the host manifest origin, got: %v", err)
	}
}

func TestResolveHTRAssetsConcurrentInstallIsAtomic(t *testing.T) {
	requireEmbeddedHTRAssets(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	const workers = 8
	results := make(chan HTRAssetPaths, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			assets, err := ResolveHTRAssetsForHost("", "", DefaultHTRNativeHostName)
			if err != nil {
				errs <- err
				return
			}
			results <- assets
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var first HTRAssetPaths
	for assets := range results {
		if first.ExtensionDir == "" {
			first = assets
			continue
		}
		if assets != first {
			t.Fatalf("concurrent resolution returned different paths: %+v vs %+v", first, assets)
		}
	}
	if first.ExtensionDir == "" || first.CliPath == "" {
		t.Fatalf("expected embedded assets, got %+v", first)
	}
	if _, err := os.Stat(filepath.Join(first.ExtensionDir, ".DS_Store")); !os.IsNotExist(err) {
		t.Fatalf(".DS_Store was extracted: %v", err)
	}
	if _, err := paths.OcodeGlobalDataDir(); err != nil {
		t.Fatal(err)
	}
}

func TestResolveHTRAssetsRepairsCorruptExtension(t *testing.T) {
	requireEmbeddedHTRAssets(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	assets, err := ResolveHTRAssetsForHost("", "", DefaultHTRNativeHostName)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(assets.ExtensionDir, "manifest.json")
	if err := os.WriteFile(manifest, []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveHTRAssetsForHost("", "", DefaultHTRNativeHostName); err != nil {
		t.Fatalf("repair corrupt extension: %v", err)
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "not-json" {
		t.Fatal("corrupt manifest was not repaired")
	}
}
