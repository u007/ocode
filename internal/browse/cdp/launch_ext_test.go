package cdp

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeManifest(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"name":"t","manifest_version":3,"version":"1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestChromeArgsFor_Extension(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir)
	args := chromeArgsFor(t.TempDir(), dir)
	if !slices.Contains(args, "--load-extension="+dir) {
		t.Fatalf("args missing --load-extension: %v", args)
	}
	if slices.Contains(args, "--disable-extensions") {
		t.Fatalf("args must not contain --disable-extensions when extension loads: %v", args)
	}
}

func TestChromeArgsFor_Default(t *testing.T) {
	for _, ext := range []string{"", t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		args := chromeArgsFor(t.TempDir(), ext)
		if !slices.Contains(args, "--disable-extensions") {
			t.Fatalf("ext %q: args missing --disable-extensions: %v", ext, args)
		}
		for _, a := range args {
			if len(a) > 17 && a[:17] == "--load-extension" {
				t.Fatalf("ext %q: unexpected load-extension flag: %v", ext, args)
			}
		}
	}
}

func TestHasExtensionDir(t *testing.T) {
	dir := t.TempDir()
	if hasExtensionDir(dir) {
		t.Fatal("empty dir must not count as extension")
	}
	writeManifest(t, dir)
	if !hasExtensionDir(dir) {
		t.Fatal("dir with manifest.json must count as extension")
	}
	if hasExtensionDir(filepath.Join(dir, "missing")) {
		t.Fatal("missing dir must not count as extension")
	}
}

func TestIsBrandedChrome(t *testing.T) {
	if !isBrandedChrome("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome") {
		t.Fatal("branded Chrome path must report branded")
	}
	for _, p := range []string{
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		"/usr/bin/chromium",
	} {
		if isBrandedChrome(p) {
			t.Fatalf("%q must not report branded", p)
		}
	}
}

func TestHTRBrowserCompatibilityNotice(t *testing.T) {
	if notice := HTRBrowserCompatibilityNotice("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"); notice == "" {
		t.Fatal("branded Chrome must produce a visible HTR notice")
	}
	if notice := HTRBrowserCompatibilityNotice("/usr/bin/chromium"); notice != "" {
		t.Fatalf("Chromium must not produce an HTR notice: %q", notice)
	}
}
