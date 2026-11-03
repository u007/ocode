package appearance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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

// A colour-only settings save must not drop the discovery snapshot or the
// conversion cache. SetExtraRoots with the roots that are already configured is
// a no-op, so a save that only changes the theme does not rescan every installed
// editor or invalidate a conversion that is still valid.
func TestServiceSetExtraRootsUnchangedKeepsSnapshotAndCache(t *testing.T) {
	root := t.TempDir()
	fixtureExtension(t, root, "acme", "themes", "First")

	original := discoverFn
	var scans int32
	discoverFn = func(ctx context.Context, roots []string) ([]Manifest, []string, error) {
		atomic.AddInt32(&scans, 1)
		return original(ctx, roots)
	}
	t.Cleanup(func() { discoverFn = original })
	conversions := countConversions(t)

	svc := NewService(t.TempDir(), []string{root})
	refs, _, err := svc.Themes(context.Background())
	if err != nil {
		t.Fatalf("Themes: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want 1", refs)
	}
	if _, err := svc.Resolve(context.Background(), refs[0].ID); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	baseScans := atomic.LoadInt32(&scans)
	baseConversions := atomic.LoadInt32(conversions)
	if baseScans != 1 || baseConversions != 1 {
		t.Fatalf("baseline scans=%d conversions=%d, want 1 and 1", baseScans, baseConversions)
	}

	// An equal but distinct slice must be recognised as no change.
	svc.SetExtraRoots([]string{root})
	if got := svc.ExtraRoots(); !reflect.DeepEqual(got, []string{root}) {
		t.Fatalf("ExtraRoots = %#v, want [%q]", got, root)
	}
	// The snapshot and the conversion cache must still be live: reading both
	// back must neither rescan nor reconvert.
	refs, _, err = svc.Themes(context.Background())
	if err != nil {
		t.Fatalf("Themes after unchanged SetExtraRoots: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs after unchanged SetExtraRoots = %+v, want 1", refs)
	}
	if _, err := svc.Resolve(context.Background(), refs[0].ID); err != nil {
		t.Fatalf("Resolve after unchanged SetExtraRoots: %v", err)
	}
	if got := atomic.LoadInt32(&scans); got != baseScans {
		t.Fatalf("scans after unchanged SetExtraRoots = %d, want %d: an unrelated save must not rescan", got, baseScans)
	}
	if got := atomic.LoadInt32(conversions); got != baseConversions {
		t.Fatalf("conversions after unchanged SetExtraRoots = %d, want %d: the cache must survive", got, baseConversions)
	}

	// A real change still refreshes immediately, so the new root is scanned.
	second := t.TempDir()
	svc.SetExtraRoots([]string{root, second})
	if _, _, err := svc.Themes(context.Background()); err != nil {
		t.Fatalf("Themes after change: %v", err)
	}
	if got := atomic.LoadInt32(&scans); got != baseScans+1 {
		t.Fatalf("scans after a changed root set = %d, want %d", got, baseScans+1)
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

// A cancellation in the middle of a walk must not be memoised: readExtension
// leaves a partial manifest behind, and caching it would show a truncated theme
// list for the whole five-minute window.
func TestServiceCancelledMidScanIsNotCached(t *testing.T) {
	root := t.TempDir()
	extDir := filepath.Join(root, "acme.themes-1.0.0")
	writeTestFile(t, filepath.Join(extDir, "package.json"), `{
  "name": "themes",
  "publisher": "acme",
  "contributes": {
    "themes": [
      {"label": "First", "uiTheme": "vs-dark", "path": "first.json"},
      {"label": "Second", "uiTheme": "vs-dark", "path": "second.json"}
    ]
  }
}`)
	writeTestFile(t, filepath.Join(extDir, "first.json"), `{"name":"First","type":"dark"}`)
	writeTestFile(t, filepath.Join(extDir, "second.json"), `{"name":"Second","type":"dark"}`)

	svc := NewService("", []string{root})
	// Same consultation order as Discover: root, entry, first contribution, then
	// cancelled on the extension's SECOND contribution (see errAfterCalls). home
	// is empty so this is the ONLY root: a following editor root would hit its
	// own context check and mask the last-extension case being tested.
	ctx := &errAfterCalls{Context: context.Background(), remaining: 3}
	if _, _, err := svc.Themes(ctx); err == nil {
		t.Fatal("Themes on a mid-scan cancellation = nil error, want cancellation")
	}

	refs, _, err := svc.Themes(context.Background())
	if err != nil {
		t.Fatalf("Themes after cancellation: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("refs = %+v, want both themes: a cancelled scan must not be cached", refs)
	}
}

// A Refresh that lands while a scan is walking invalidates it. Without a
// generation guard the older walk's list is installed after the refresh and the
// new extension stays invisible until the memo expires.
func TestServiceScanStartedBeforeRefreshDoesNotWin(t *testing.T) {
	root := t.TempDir()
	fixtureExtension(t, root, "acme", "themes", "First")
	svc := NewService(t.TempDir(), []string{root})

	original := discoverFn
	var once sync.Once
	discoverFn = func(ctx context.Context, roots []string) ([]Manifest, []string, error) {
		manifests, warnings, err := original(ctx, roots)
		once.Do(func() {
			// A settings change (or an install) lands while this scan is in
			// flight: a new extension appears and Refresh drops the memo.
			fixtureExtension(t, root, "acme", "second", "Second")
			svc.Refresh()
		})
		return manifests, warnings, err
	}
	t.Cleanup(func() { discoverFn = original })

	refs, _, err := svc.Themes(context.Background())
	if err != nil {
		t.Fatalf("Themes: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("refs = %+v, want 2: the scan that started before Refresh must be discarded", refs)
	}
	// The caller must not have left the stale result in the memo either.
	refs, _, err = svc.Themes(context.Background())
	if err != nil {
		t.Fatalf("Themes (cached): %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("refs after the retry = %+v, want 2 (the pre-Refresh list must not be cached)", refs)
	}
}

// A relative extra root stored on disk must warn and be skipped, never block and
// never be scanned: scanning it would read whatever directory the process
// started in.
func TestServiceWarnsOnStoredRelativeExtraRoot(t *testing.T) {
	cwd := t.TempDir()
	// Populate the directory a relative root would resolve to, so a regression
	// that still scans it is observable.
	fixtureExtension(t, filepath.Join(cwd, "relative", "extensions"), "acme", "themes", "Should Not Appear")
	t.Chdir(cwd)

	svc := NewService("", []string{"relative/extensions"})
	refs, warnings, err := svc.Themes(context.Background())
	if err != nil {
		t.Fatalf("Themes: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("refs = %+v, want none: a relative root must never be scanned", refs)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one naming the stored relative root", warnings)
	}
	if !strings.Contains(warnings[0], "relative/extensions") || !strings.Contains(warnings[0], "working directory") {
		t.Fatalf("warning = %q, want it to name the root and the working-directory rule", warnings[0])
	}
}

// A bare "~" stored on disk must warn and be skipped, exactly like a relative
// path: expanding it would make the user's whole home an extension root.
func TestServiceWarnsOnStoredBareTilde(t *testing.T) {
	svc := NewService(t.TempDir(), []string{"~"})
	refs, warnings, err := svc.Themes(context.Background())
	if err != nil {
		t.Fatalf("Themes: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("refs = %+v, want none: a bare ~ must never be scanned", refs)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one naming the stored bare ~", warnings)
	}
	if !strings.Contains(warnings[0], `"~"`) {
		t.Fatalf("warning = %q, want it to name the bare ~ entry", warnings[0])
	}
}
