package paths

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOfficeAddinPathsForDarwin(t *testing.T) {
	home := filepath.FromSlash("/Users/test")
	got := officeAddinPathsFor("darwin", home, "/Users/test/Library/Application Support")

	want := []struct {
		app    string
		label  string
		bundle string
	}{
		{"excel", "Excel", "com.microsoft.Excel"},
		{"word", "Word", "com.microsoft.Word"},
		{"powerpoint", "PowerPoint", "com.microsoft.Powerpoint"},
		{"outlook", "Outlook", "com.microsoft.Outlook"},
		{"onenote", "OneNote", "com.microsoft.onenote.mac"},
	}
	if len(got) != len(want) {
		t.Fatalf("darwin presets = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		wantPath := filepath.Join(home, "Library", "Containers", w.bundle, "Data", "Documents", "wef")
		if got[i].App != w.app || got[i].Label != w.label || got[i].Path != wantPath {
			t.Errorf("preset %d = %+v, want app=%q label=%q path=%q", i, got[i], w.app, w.label, wantPath)
		}
		if got[i].Allowed {
			t.Errorf("preset %d: Allowed must be false in the paths package", i)
		}
	}
}

func TestOfficeAddinPathsForWindows(t *testing.T) {
	appData := filepath.FromSlash(`C:\Users\test\AppData\Roaming`)
	got := officeAddinPathsFor("windows", filepath.FromSlash(`C:\Users\test`), appData)

	want := []struct {
		app   string
		label string
		path  string
	}{
		{"addins", "Office Add-ins (classic)", filepath.Join(appData, "Microsoft", "AddIns")},
		{"word-startup", "Word startup folder", filepath.Join(appData, "Microsoft", "Word", "STARTUP")},
	}
	if len(got) != len(want) {
		t.Fatalf("windows presets = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].App != w.app || got[i].Label != w.label || got[i].Path != w.path {
			t.Errorf("preset %d = %+v, want app=%q label=%q path=%q", i, got[i], w.app, w.label, w.path)
		}
		// The path must be the EXPANDED %APPDATA%, never the literal env-var
		// token: extra_allowed_paths does not expand environment variables, so a
		// "%APPDATA%\..." entry would be saved but never match.
		if strings.Contains(got[i].Path, "%") {
			t.Errorf("preset %d path %q still contains an unexpanded environment variable", i, got[i].Path)
		}
	}
	if got[0].Note == "" {
		t.Error("windows Office Add-ins preset must explain the shared-folder catalog")
	}
}

func TestOfficeAddinPathsForWindowsFallsBackToHome(t *testing.T) {
	home := filepath.FromSlash(`C:\Users\test`)
	got := officeAddinPathsFor("windows", home, "")
	if len(got) == 0 {
		t.Fatal("expected presets when APPDATA is unset but home is known")
	}
	want := filepath.Join(home, "AppData", "Roaming", "Microsoft", "AddIns")
	if got[0].Path != want {
		t.Errorf("first preset path = %q, want %q", got[0].Path, want)
	}
}

func TestOfficeAddinPathsForUnsupportedPlatforms(t *testing.T) {
	for _, goos := range []string{"linux", "freebsd", ""} {
		if got := officeAddinPathsFor(goos, "/home/test", "/home/test/.config"); len(got) != 0 {
			t.Errorf("%s presets = %+v, want none", goos, got)
		}
	}
}

func TestOfficeAddinPathsCurrentPlatform(t *testing.T) {
	got := OfficeAddinPaths()
	if runtime.GOOS == "linux" && len(got) != 0 {
		t.Errorf("linux must have no presets, got %+v", got)
	}
	for _, p := range got {
		if p.App == "" || p.Label == "" {
			t.Errorf("preset missing app/label: %+v", p)
		}
		if !filepath.IsAbs(p.Path) {
			t.Errorf("preset path %q is not absolute", p.Path)
		}
		if p.Exists && !p.Available {
			t.Errorf("preset %q exists but is not available", p.Path)
		}
	}
}
