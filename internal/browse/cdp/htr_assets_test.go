package cdp

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/paths"
)

func TestResolveHTRAssetsConcurrentInstallIsAtomic(t *testing.T) {
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
