package appearance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestFile writes content to path, creating parent directories.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// darkTheme is a minimal valid theme file.
const darkTheme = `{
  // a comment, as real VS Code themes are JSONC
  "name": "Acme Dark",
  "type": "dark",
  "colors": {"editor.background": "#101010", "editor.foreground": "#eeeeee"},
}`

// themeManifest declares one colour theme contribution.
func themeManifest(themePath string) string {
	return `{
  "name": "themes",
  "publisher": "acme",
  "version": "1.2.3",
  "contributes": {
    "themes": [
      {"label": "Acme Dark", "uiTheme": "vs-dark", "path": "` + themePath + `"}
    ]
  }
}`
}

func TestDiscoverReadsThemeContributions(t *testing.T) {
	root := t.TempDir()
	extDir := filepath.Join(root, "acme.themes-1.2.3")
	writeTestFile(t, filepath.Join(extDir, "package.json"), themeManifest("./themes/dark.json"))
	writeTestFile(t, filepath.Join(extDir, "themes", "dark.json"), darkTheme)

	manifests, warnings, err := Discover(context.Background(), []string{root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(manifests) != 1 {
		t.Fatalf("got %d manifests, want 1: %+v", len(manifests), manifests)
	}
	m := manifests[0]
	if m.ID != "acme.themes" || m.Namespace != "acme" || m.Name != "themes" || m.Version != "1.2.3" {
		t.Fatalf("manifest identity wrong: %+v", m)
	}
	if len(m.Themes) != 1 {
		t.Fatalf("got %d themes, want 1: %+v", len(m.Themes), m.Themes)
	}
	got := m.Themes[0]
	if got.Label != "Acme Dark" {
		t.Errorf("Label = %q, want Acme Dark", got.Label)
	}
	if got.ID != "acme-dark" {
		t.Errorf("slug = %q, want acme-dark (from the theme file's name)", got.ID)
	}
	if got.Kind != PolarityDark {
		t.Errorf("Kind = %q, want dark", got.Kind)
	}
	wantPath := filepath.Join(extDir, "themes", "dark.json")
	// Path is canonicalised (symlinks resolved), which is what makes it a stable
	// cache key: on macOS a temp dir resolves /var to /private/var.
	if resolved, err := filepath.EvalSymlinks(wantPath); err == nil {
		wantPath = resolved
	}
	if got.Path != wantPath {
		t.Errorf("Path = %q, want %q", got.Path, wantPath)
	}
}

// A directory with no manifest, a manifest with no contributions, and a plain
// file are not extensions. None of them may produce a manifest or an error.
func TestDiscoverSkipsDirectoriesWithoutManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "no-manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "not-an-extension", "package.json"),
		`{"name":"plain","publisher":"acme"}`)
	writeTestFile(t, filepath.Join(root, "stray-file.txt"), "hello")

	manifests, warnings, err := Discover(context.Background(), []string{root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(manifests) != 0 {
		t.Fatalf("got %d manifests, want 0: %+v", len(manifests), manifests)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
}

// A missing contribution file is a warning and never aborts the rest of the
// scan: the sibling contribution from the same extension is still listed.
func TestDiscoverWarnsOnUnavailableThemeAndKeepsScanning(t *testing.T) {
	root := t.TempDir()
	extDir := filepath.Join(root, "acme.themes-1.0.0")
	writeTestFile(t, filepath.Join(extDir, "package.json"), `{
  "name": "themes",
  "publisher": "acme",
  "contributes": {
    "themes": [
      {"label": "Missing", "uiTheme": "vs-dark", "path": "themes/missing.json"},
      {"label": "Present", "uiTheme": "vs", "path": "themes/present.json"}
    ]
  }
}`)
	writeTestFile(t, filepath.Join(extDir, "themes", "present.json"), `{"name":"Present","type":"light"}`)

	manifests, warnings, err := Discover(context.Background(), []string{root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "missing.json") {
		t.Fatalf("warnings = %v, want one naming missing.json", warnings)
	}
	if len(manifests) != 1 || len(manifests[0].Themes) != 1 {
		t.Fatalf("surviving contribution lost: %+v", manifests)
	}
	if got := manifests[0].Themes[0].Kind; got != PolarityLight {
		t.Errorf("Kind = %q, want light from uiTheme vs", got)
	}
}

// A contribution path may not escape the extension directory: the id would then
// address a file the user never installed.
func TestDiscoverRejectsContributionEscapingExtension(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "outside.json"), darkTheme)
	extDir := filepath.Join(root, "acme.themes-1.0.0")
	writeTestFile(t, filepath.Join(extDir, "package.json"), themeManifest("../outside.json"))

	manifests, warnings, err := Discover(context.Background(), []string{root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(manifests) != 1 || len(manifests[0].Themes) != 0 {
		t.Fatalf("escaping contribution was listed: %+v", manifests)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "unavailable") {
		t.Fatalf("warnings = %v, want one unavailable warning", warnings)
	}
}

// A symlinked package.json must not let an extension point the scan at a
// manifest outside its own directory: the manifest read is contained exactly
// like a contribution path.
func TestDiscoverRejectsManifestSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "package.json")
	writeTestFile(t, outside, themeManifest("dark.json"))
	extDir := filepath.Join(root, "acme.evil-1.0.0")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(extDir, "package.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeTestFile(t, filepath.Join(extDir, "dark.json"), darkTheme)

	manifests, warnings, err := Discover(context.Background(), []string{root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(manifests) != 0 {
		t.Fatalf("manifests = %+v, want none: a symlinked manifest escaping the extension must be refused", manifests)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "manifest") {
		t.Fatalf("warnings = %v, want one naming the manifest", warnings)
	}
}

// Two themes with the same name in one extension must not collide in the id
// space; Light/Dark punctuation and case are folded into the slug.
func TestDiscoverDedupesVariantSlugs(t *testing.T) {
	root := t.TempDir()
	extDir := filepath.Join(root, "acme.themes-1.0.0")
	writeTestFile(t, filepath.Join(extDir, "package.json"), `{
  "name": "themes",
  "publisher": "acme",
  "contributes": {
    "themes": [
      {"label": "Dark", "uiTheme": "vs-dark", "path": "a.json"},
      {"label": "DARK!", "uiTheme": "vs-dark", "path": "b.json"},
      {"label": "Light", "uiTheme": "vs", "path": "c.json"}
    ]
  }
}`)
	for _, name := range []string{"a.json", "b.json", "c.json"} {
		writeTestFile(t, filepath.Join(extDir, name), `{"name":"Dark","type":"dark"}`)
	}

	manifests, _, err := Discover(context.Background(), []string{root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(manifests) != 1 {
		t.Fatalf("got %d manifests, want 1", len(manifests))
	}
	ids := make([]string, 0, 3)
	for _, c := range manifests[0].Themes {
		ids = append(ids, c.ID)
	}
	want := []string{"dark", "dark-2", "dark-3"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("slugs = %v, want %v", ids, want)
	}
}

// Icon themes are discovered and counted even though Task 3 has no endpoint for
// them yet: the theme list reports has_icon_theme per extension.
func TestDiscoverReadsIconThemeContributions(t *testing.T) {
	root := t.TempDir()
	extDir := filepath.Join(root, "pkief.material-icon-theme-5.0.0")
	writeTestFile(t, filepath.Join(extDir, "package.json"), `{
  "name": "material-icon-theme",
  "publisher": "pkief",
  "contributes": {
    "iconThemes": [
      {"id": "material-icon-theme", "label": "Material Icon Theme", "path": "material-icon-theme.json"}
    ]
  }
}`)
	writeTestFile(t, filepath.Join(extDir, "material-icon-theme.json"), `{"iconDefinitions":{}}`)

	manifests, warnings, err := Discover(context.Background(), []string{root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(manifests) != 1 || len(manifests[0].IconThemes) != 1 {
		t.Fatalf("icon theme not discovered: %+v", manifests)
	}
	if got := manifests[0].IconThemes[0]; got.ID != "material-icon-theme" || got.Label != "Material Icon Theme" {
		t.Fatalf("icon contrib = %+v", got)
	}
}

// The first root wins when the same extension is installed in two editors.
func TestDiscoverFirstRootWins(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	for _, root := range []string{first, second} {
		extDir := filepath.Join(root, "acme.themes-1.0.0")
		writeTestFile(t, filepath.Join(extDir, "package.json"), themeManifest("dark.json"))
		writeTestFile(t, filepath.Join(extDir, "dark.json"), darkTheme)
	}

	manifests, _, err := Discover(context.Background(), []string{first, second})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(manifests) != 1 {
		t.Fatalf("got %d manifests, want 1: %+v", len(manifests), manifests)
	}
	if manifests[0].Dir != filepath.Join(first, "acme.themes-1.0.0") {
		t.Fatalf("Dir = %q, want the first root's copy", manifests[0].Dir)
	}
}

// A missing root is normal (not every editor is installed) and must not warn.
func TestDiscoverIgnoresMissingRoots(t *testing.T) {
	manifests, warnings, err := Discover(context.Background(), []string{
		filepath.Join(t.TempDir(), "does-not-exist"),
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(manifests) != 0 || len(warnings) != 0 {
		t.Fatalf("manifests=%v warnings=%v, want empty", manifests, warnings)
	}
}

func TestDiscoverStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Discover(ctx, []string{t.TempDir()}); err == nil {
		t.Fatal("Discover on a cancelled context = nil error, want cancellation")
	}
}

// ExtensionRoots must put extra roots first, then every well-known editor
// location, and expand a leading tilde.
func TestExtensionRootsOrder(t *testing.T) {
	home := "/home/james"
	roots := ExtensionRoots(home, []string{"~/portable/extensions", "", "/opt/extra"})
	want := []string{
		filepath.Join(home, "portable", "extensions"),
		"/opt/extra",
		filepath.Join(home, ".vscode", "extensions"),
		filepath.Join(home, ".vscode-insiders", "extensions"),
		filepath.Join(home, ".vscode-oss", "extensions"),
		filepath.Join(home, ".cursor", "extensions"),
		filepath.Join(home, ".windsurf", "extensions"),
	}
	if len(roots) != len(want) {
		t.Fatalf("roots = %v, want %v", roots, want)
	}
	for i := range want {
		if roots[i] != want[i] {
			t.Fatalf("roots[%d] = %q, want %q (all: %v)", i, roots[i], want[i], roots)
		}
	}
}

// An empty home must contribute no editor roots: a relative path would scan
// whatever directory the process happened to start in.
func TestExtensionRootsEmptyHomeYieldsOnlyExtras(t *testing.T) {
	roots := ExtensionRoots("", []string{"/opt/extra"})
	if len(roots) != 1 || roots[0] != "/opt/extra" {
		t.Fatalf("roots = %v, want only /opt/extra", roots)
	}
}

// errAfterCalls is a context whose Err reports cancellation only after n
// successful reads. It lets a test cancel a scan at an exact point in the walk
// (Discover consults Err per root, per entry and per contribution) instead of
// racing a timer or adding a production hook.
type errAfterCalls struct {
	context.Context
	remaining int
}

func (c *errAfterCalls) Err() error {
	if c.remaining <= 0 {
		return context.Canceled
	}
	c.remaining--
	return nil
}

// A cancellation during the LAST extension has no following iteration to notice
// it. Without propagation out of readExtension (and the final re-check) the walk
// returns a truncated list with a nil error, which the service then memoises for
// five minutes.
func TestDiscoverPropagatesCancellationDuringLastExtension(t *testing.T) {
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

	// Err is consulted for the root, then the extension entry, then each
	// contribution: the cancel lands on the extension's SECOND contribution.
	ctx := &errAfterCalls{Context: context.Background(), remaining: 3}
	manifests, _, err := Discover(ctx, []string{root})
	if err == nil {
		t.Fatalf("Discover = %+v with a nil error; a cancelled scan must never look complete", manifests)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// A relative extra root is neither absolute nor "~"-prefixed, so it must be
// dropped rather than resolved against the process working directory.
func TestExtensionRootsSkipsRelativeExtras(t *testing.T) {
	roots := ExtensionRoots("/home/james", []string{"relative/extensions", "../sibling", "/opt/extra", "~other/x", "~/portable"})
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			t.Fatalf("roots = %v, want every entry absolute", roots)
		}
	}
	if len(roots) != 2+5 {
		t.Fatalf("roots = %v, want /opt/extra and the expanded ~/portable plus 5 editor roots", roots)
	}
	if roots[0] != "/opt/extra" || roots[1] != filepath.Join("/home/james", "portable") {
		t.Fatalf("roots = %v, want the valid extras first", roots)
	}
}

func TestInvalidExtraRoots(t *testing.T) {
	got := InvalidExtraRoots([]string{"", "  ", "/abs/path", "~/home", "~", "relative", "../x", "~other/x"})
	want := []string{"~", "relative", "../x", "~other/x"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("InvalidExtraRoots = %v, want %v", got, want)
	}
}

// A bare "~" must not become an extension root: expanding it would walk the
// user's whole home directory (and every installed editor's tree under it).
func TestExtensionRootsRejectsBareTilde(t *testing.T) {
	home := "/home/james"
	roots := ExtensionRoots(home, []string{"~"})
	for _, root := range roots {
		if root == home {
			t.Fatalf("roots = %v: a bare ~ was expanded to the home directory", roots)
		}
	}
	// Only the five well-known editor roots remain.
	if len(roots) != 5 {
		t.Fatalf("roots = %v, want only the five editor roots", roots)
	}
	if got := expandHomeDir("~", home); got != "~" {
		t.Fatalf("expandHomeDir(\"~\") = %q, want it left unexpanded", got)
	}
	// "~/" still expands.
	if got := expandHomeDir("~/extensions", home); got != filepath.Join(home, "extensions") {
		t.Fatalf("expandHomeDir(\"~/extensions\") = %q", got)
	}
}
