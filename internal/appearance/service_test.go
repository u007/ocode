package appearance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/theme"
)

// fixtureExtension installs one extension contributing a single dark theme in
// its own temp root, and returns the extension directory.
func fixtureExtension(t *testing.T, root, publisher, name, slugLabel string) string {
	t.Helper()
	extDir := filepath.Join(root, publisher+"."+name+"-1.0.0")
	writeTestFile(t, filepath.Join(extDir, "package.json"), `{
  "name": "`+name+`",
  "publisher": "`+publisher+`",
  "contributes": {
    "themes": [
      {"label": "`+slugLabel+`", "uiTheme": "vs-dark", "path": "themes/dark.json"}
    ]
  }
}`)
	writeTestFile(t, filepath.Join(extDir, "themes", "dark.json"), `{
  "name": "`+slugLabel+`",
  "type": "dark",
  "colors": {"editor.background": "#101010", "editor.foreground": "#eeeeee"}
}`)
	return extDir
}

// countConversions replaces the conversion hook for the duration of a test and
// returns a counter of how many times it ran.
func countConversions(t *testing.T) *int32 {
	t.Helper()
	var calls int32
	original := convertThemeFn
	convertThemeFn = func(id, label string, raw *RawTheme) (ConvertedTheme, error) {
		atomic.AddInt32(&calls, 1)
		return original(id, label, raw)
	}
	t.Cleanup(func() { convertThemeFn = original })
	return &calls
}

// frozenClock pins the discovery TTL clock and returns a function that advances
// it, so the five-minute memo can be tested without sleeping.
func frozenClock(t *testing.T) func(time.Duration) {
	t.Helper()
	base := time.Unix(1_700_000_000, 0)
	original := nowFn
	nowFn = func() time.Time { return base }
	t.Cleanup(func() { nowFn = original })
	return func(d time.Duration) { base = base.Add(d) }
}

func TestServiceResolveReturnsConvertedPalettes(t *testing.T) {
	root := t.TempDir()
	fixtureExtension(t, root, "acme", "themes", "Acme Dark")

	svc := NewService(t.TempDir(), []string{root})
	converted, err := svc.Resolve(context.Background(), "vscode:acme.themes/acme-dark")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if converted.ID != "vscode:acme.themes/acme-dark" {
		t.Errorf("ID = %q", converted.ID)
	}
	if converted.Label != "Acme Dark" {
		t.Errorf("Label = %q, want Acme Dark", converted.Label)
	}
	if converted.Kind != PolarityDark {
		t.Errorf("Kind = %q, want dark", converted.Kind)
	}
	var zero theme.ThemeColors
	if converted.Colors == zero {
		t.Errorf("Colors is empty: %+v", converted.Colors)
	}
	if len(converted.UI) == 0 {
		t.Error("UI palette is empty")
	}
}

// The malformed ids in the brief: an id that tries to escape the extension root
// must be rejected before anything touches the filesystem, and a valid id for a
// discovered theme must still resolve.
func TestServiceResolveRejectsMalformedID(t *testing.T) {
	root := t.TempDir()
	fixtureExtension(t, root, "acme", "themes", "Okay")

	svc := NewService(t.TempDir(), []string{root})
	malformed := []string{
		"vscode:../../etc/passwd",
		"vscode:acme.themes/../escape",
		"vscode:acme.themes/..",
		"vscode:../dark",
		"vscode:acme.themes/",
		"vscode:/dark",
		"vscode:acme.themes/Dark",
		"vscode:acme.themes/dark.json",
		"acme.themes/dark",
		"",
	}
	for _, id := range malformed {
		if _, err := svc.Resolve(context.Background(), id); err == nil {
			t.Errorf("Resolve(%q) = nil error, want rejection", id)
		}
		if _, err := svc.Resolve(context.Background(), id); errors.Is(err, ErrThemeNotFound) {
			t.Errorf("Resolve(%q) reported not-found, want a malformed-id error", id)
		}
	}

	if _, err := svc.Resolve(context.Background(), "vscode:acme.themes/okay"); err != nil {
		t.Fatalf("Resolve(valid) = %v, want success", err)
	}
}

func TestServiceResolveUnknownIDIsNotFound(t *testing.T) {
	root := t.TempDir()
	fixtureExtension(t, root, "acme", "themes", "Okay")

	svc := NewService(t.TempDir(), []string{root})
	_, err := svc.Resolve(context.Background(), "vscode:other.ext/missing")
	if !errors.Is(err, ErrThemeNotFound) {
		t.Fatalf("Resolve(unknown) = %v, want ErrThemeNotFound", err)
	}
}

// Resolve twice must convert once; touching the file must invalidate the entry.
func TestServiceCachesByModTime(t *testing.T) {
	root := t.TempDir()
	extDir := fixtureExtension(t, root, "acme", "themes", "Cached")
	calls := countConversions(t)

	svc := NewService(t.TempDir(), []string{root})
	id := "vscode:acme.themes/cached"
	for i := 0; i < 2; i++ {
		if _, err := svc.Resolve(context.Background(), id); err != nil {
			t.Fatalf("Resolve #%d: %v", i+1, err)
		}
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("conversions = %d, want 1 (second resolve must hit the cache)", got)
	}

	themePath := filepath.Join(extDir, "themes", "dark.json")
	later := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(themePath, later, later); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if _, err := svc.Resolve(context.Background(), id); err != nil {
		t.Fatalf("Resolve after touch: %v", err)
	}
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Fatalf("conversions = %d, want 2 (touch must invalidate)", got)
	}
}

// Discovery is memoised for five minutes (spec §6): a newly installed extension
// is not visible until the memo expires, and Refresh makes it visible at once.
func TestServiceDiscoveryMemoisedForFiveMinutes(t *testing.T) {
	root := t.TempDir()
	fixtureExtension(t, root, "acme", "themes", "First")
	advance := frozenClock(t)

	svc := NewService(t.TempDir(), []string{root})
	refs, _, err := svc.Themes(context.Background())
	if err != nil {
		t.Fatalf("Themes: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1", len(refs))
	}

	fixtureExtension(t, root, "acme", "second", "Second")
	if refs, _, err = svc.Themes(context.Background()); err != nil {
		t.Fatalf("Themes: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("got %d refs inside the memo window, want 1: %+v", len(refs), refs)
	}

	advance(discoveryTTL + time.Second)
	if refs, _, err = svc.Themes(context.Background()); err != nil {
		t.Fatalf("Themes after TTL: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("got %d refs after the TTL, want 2: %+v", len(refs), refs)
	}

	svc.Refresh()
	fixtureExtension(t, root, "acme", "third", "Third")
	if refs, _, err = svc.Themes(context.Background()); err != nil {
		t.Fatalf("Themes after Refresh: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("got %d refs after Refresh, want 3: %+v", len(refs), refs)
	}
}

func TestServiceSetExtraRootsRescans(t *testing.T) {
	first := t.TempDir()
	fixtureExtension(t, first, "acme", "themes", "First")
	second := t.TempDir()
	fixtureExtension(t, second, "other", "themes", "Other")

	svc := NewService(t.TempDir(), []string{first})
	refs, _, err := svc.Themes(context.Background())
	if err != nil {
		t.Fatalf("Themes: %v", err)
	}
	if len(refs) != 1 || refs[0].Extension != "acme.themes" {
		t.Fatalf("initial refs = %+v", refs)
	}

	svc.SetExtraRoots([]string{first, second})
	refs, _, err = svc.Themes(context.Background())
	if err != nil {
		t.Fatalf("Themes: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("refs after SetExtraRoots = %+v, want 2", refs)
	}
}

func TestServiceThemesSortedByLabelWithKindAndIconFlag(t *testing.T) {
	root := t.TempDir()
	fixtureExtension(t, root, "zeta", "themes", "Zulu")
	fixtureExtension(t, root, "acme", "themes", "Alpha")
	withIcons := filepath.Join(root, "icons.pack-1.0.0")
	writeTestFile(t, filepath.Join(withIcons, "package.json"), `{
  "name": "pack",
  "publisher": "icons",
  "contributes": {
    "iconThemes": [{"id": "pack-icons", "label": "Pack Icons", "path": "icons.json"}]
  }
}`)
	writeTestFile(t, filepath.Join(withIcons, "icons.json"), `{"iconDefinitions":{}}`)

	svc := NewService(t.TempDir(), []string{root})
	refs, _, err := svc.Themes(context.Background())
	if err != nil {
		t.Fatalf("Themes: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2: %+v", len(refs), refs)
	}
	if refs[0].Label != "Alpha" || refs[1].Label != "Zulu" {
		t.Fatalf("refs not sorted by label: %+v", refs)
	}
	for _, ref := range refs {
		if ref.Source != SourceVSCode {
			t.Errorf("Source = %q, want %q", ref.Source, SourceVSCode)
		}
		if ref.Kind != PolarityDark {
			t.Errorf("Kind = %q, want dark", ref.Kind)
		}
		if ref.HasIconTheme {
			t.Errorf("%s reports has_icon_theme without one", ref.ID)
		}
		if !strings.HasPrefix(ref.ID, "vscode:") {
			t.Errorf("ID = %q, want a vscode: id", ref.ID)
		}
	}
}

// A theme file that disappears between discovery and resolution is an error, not
// an empty palette. The following Resolve after Refresh drops it from the list.
func TestServiceResolveReportsVanishedTheme(t *testing.T) {
	root := t.TempDir()
	extDir := fixtureExtension(t, root, "acme", "themes", "Gone")
	svc := NewService(t.TempDir(), []string{root})

	id := "vscode:acme.themes/gone"
	if _, err := svc.Resolve(context.Background(), id); err != nil {
		t.Fatalf("Resolve before removal: %v", err)
	}
	if err := os.Remove(filepath.Join(extDir, "themes", "dark.json")); err != nil {
		t.Fatalf("remove theme: %v", err)
	}
	if _, err := svc.Resolve(context.Background(), id); err == nil {
		t.Fatal("Resolve of a removed theme file = nil error, want failure")
	}
	if errors.Is(mustResolveErr(svc, id), ErrThemeNotFound) {
		t.Fatal("a present-but-unreadable contribution must not report not-found")
	}
}

func mustResolveErr(svc *Service, id string) error {
	_, err := svc.Resolve(context.Background(), id)
	return err
}

// A cancelled context must not be cached: the next request scans again.
func TestServiceScanErrorIsNotCached(t *testing.T) {
	root := t.TempDir()
	fixtureExtension(t, root, "acme", "themes", "Alpha")
	svc := NewService(t.TempDir(), []string{root})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := svc.Themes(ctx); err == nil {
		t.Fatal("Themes on a cancelled context = nil error, want cancellation")
	}
	refs, _, err := svc.Themes(context.Background())
	if err != nil {
		t.Fatalf("Themes after cancellation: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want 1", refs)
	}
}

func TestValidThemeID(t *testing.T) {
	valid := []string{
		"vscode:acme.themes/dark",
		"vscode:PKief.material-icon-theme/material-icon-theme",
		"vscode:a_b-c.d/e",
	}
	for _, id := range valid {
		if !ValidThemeID(id) {
			t.Errorf("ValidThemeID(%q) = false, want true", id)
		}
	}
	invalid := []string{
		"",
		"vscode:acme.themes",
		"vscode:acme.themes/",
		"vscode:/dark",
		"vscode:acme.themes/Dark",
		"vscode:acme/../../etc/passwd",
		"vscode:.. /dark",
		"tokyonight",
	}
	for _, id := range invalid {
		if ValidThemeID(id) {
			t.Errorf("ValidThemeID(%q) = true, want false", id)
		}
	}
}
