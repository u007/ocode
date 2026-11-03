package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/appearance"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/remote"
	"github.com/u007/ocode/internal/theme"
)

// writeServerTestFile writes content to path, creating parent directories.
func writeServerTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// installServerAppearanceExtension installs one extension contributing a theme
// named `name` in root. The id it produces is
// `vscode:<publisher>.<extension>/<slug>`.
func installServerAppearanceExtension(t *testing.T, root, dirName, publisher, name, label, background string) string {
	t.Helper()
	extDir := filepath.Join(root, dirName)
	writeServerTestFile(t, filepath.Join(extDir, "package.json"), `{
  "name": "`+name+`",
  "publisher": "`+publisher+`",
  "contributes": {
    "themes": [
      {"label": "`+label+`", "uiTheme": "vs-dark", "path": "themes/main.json"}
    ]
  }
}`)
	writeServerTestFile(t, filepath.Join(extDir, "themes", "main.json"), `{
  "name": "`+label+`",
  "type": "dark",
  "colors": {
    "editor.background": "`+background+`",
    "editor.foreground": "#eeeeee",
    "editorCursor.foreground": "#ff0000",
    "sideBar.background": "#202020",
    "sideBar.foreground": "#dddddd",
    "statusBar.background": "#303030",
    "statusBar.foreground": "#ffffff",
    "terminal.background": "`+background+`",
    "terminal.foreground": "#eeeeee"
  },
  "tokenColors": [{"scope": "comment", "settings": {"foreground": "#888888"}}]
}`)
	return extDir
}

// newAppearanceRouter builds a server whose appearance service scans one temp
// extension root and whose config lives under an isolated temp home. It returns
// the server and the extension root so a test can add more extensions.
func newAppearanceRouter(t *testing.T) (*Server, string) {
	t.Helper()
	home := t.TempDir()
	setHomeTree(t, home)
	root := t.TempDir()

	srv := New("127.0.0.1:0", "", "", nil)
	srv.handler.mu.Lock()
	srv.handler.cfg = &config.Config{}
	srv.handler.mu.Unlock()
	srv.handler.appearance = appearance.NewService(home, []string{root})
	return srv, root
}

func decodeAppearanceThemes(t *testing.T, rec *httptest.ResponseRecorder) AppearanceThemesResponse {
	t.Helper()
	var resp AppearanceThemesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode themes response: %v (body=%s)", err, rec.Body.String())
	}
	return resp
}

func decodeAppearanceSettings(t *testing.T, rec *httptest.ResponseRecorder) AppearanceSettingsResponse {
	t.Helper()
	var resp AppearanceSettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode settings response: %v (body=%s)", err, rec.Body.String())
	}
	return resp
}

func TestHandleAppearanceThemesListsDiscoveredThemes(t *testing.T) {
	srv, root := newAppearanceRouter(t)
	installServerAppearanceExtension(t, root, "acme.themes-1.0.0", "acme", "themes", "Acme Dark", "#101010")

	rec := httptest.NewRecorder()
	srv.serveHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/themes?limit=1000", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/appearance/themes = %d body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeAppearanceThemes(t, rec)
	// The list is unified: every registry theme plus the discovered one.
	if want := len(theme.AvailableThemes()) + 1; resp.Total != want || len(resp.Themes) != want {
		t.Fatalf("Total = %d (len %d), want %d (registry + 1)", resp.Total, len(resp.Themes), want)
	}
	if len(resp.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none for a clean scan", resp.Warnings)
	}

	item, idx := findAppearanceTheme(t, resp.Themes, "vscode:acme.themes/acme-dark")
	if item.Label != "Acme Dark" || item.Source != "vscode" || item.Kind != appearance.PolarityDark {
		t.Errorf("item = %+v", item)
	}
	if item.Extension != "acme.themes" {
		t.Errorf("Extension = %q", item.Extension)
	}
	if item.HasIconTheme {
		t.Errorf("HasIconTheme = true for an extension with no icon theme")
	}
	// Registry entries come first; the discovered block after them.
	if idx < len(theme.AvailableThemes()) {
		t.Fatalf("VS Code entry at %d, want it after the %d registry entries", idx, len(theme.AvailableThemes()))
	}
	for i, entry := range resp.Themes[:len(theme.AvailableThemes())] {
		if want := theme.Source(entry.ID); entry.Source != want {
			t.Fatalf("registry entry %d (%+v) has source %q, want %q", i, entry, entry.Source, want)
		}
		if entry.Extension != "" {
			t.Fatalf("registry entry %d carries extension %q", i, entry.Extension)
		}
	}
	// The registry is a MIX of origins, so pin one id from each: a list that
	// classified every entry as the same source would still satisfy a bare
	// "builtin or opencode" check.
	var wantBuiltin, wantOpencode string
	for _, name := range theme.AvailableThemes() {
		switch theme.Source(name) {
		case theme.SourceBuiltin:
			if wantBuiltin == "" {
				wantBuiltin = name
			}
		case theme.SourceOpencode:
			if wantOpencode == "" {
				wantOpencode = name
			}
		}
	}
	if wantBuiltin == "" || wantOpencode == "" {
		t.Fatalf("theme registry is not mixed: builtin=%q opencode=%q", wantBuiltin, wantOpencode)
	}
	if item, _ := findAppearanceTheme(t, resp.Themes, wantBuiltin); item.Source != theme.SourceBuiltin {
		t.Fatalf("registry entry %q has source %q, want %q", wantBuiltin, item.Source, theme.SourceBuiltin)
	}
	if item, _ := findAppearanceTheme(t, resp.Themes, wantOpencode); item.Source != theme.SourceOpencode {
		t.Fatalf("registry entry %q has source %q, want %q", wantOpencode, item.Source, theme.SourceOpencode)
	}
}

// findAppearanceTheme returns the entry with id and its position in items.
func findAppearanceTheme(t *testing.T, items []AppearanceThemeItem, id string) (AppearanceThemeItem, int) {
	t.Helper()
	for i, item := range items {
		if item.ID == id {
			return item, i
		}
	}
	t.Fatalf("theme %q missing from %d entries", id, len(items))
	return AppearanceThemeItem{}, -1
}

func TestHandleAppearanceThemesPaginates(t *testing.T) {
	srv, root := newAppearanceRouter(t)
	installServerAppearanceExtension(t, root, "acme.one-1.0.0", "acme", "one", "Alpha", "#101010")
	installServerAppearanceExtension(t, root, "acme.two-1.0.0", "acme", "two", "Bravo", "#101010")
	installServerAppearanceExtension(t, root, "acme.three-1.0.0", "acme", "three", "Charlie", "#101010")

	registry := len(theme.AvailableThemes())
	rec := httptest.NewRecorder()
	srv.serveHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/appearance/themes?limit=1&offset=%d", registry+1), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeAppearanceThemes(t, rec)
	if want := registry + 3; resp.Total != want {
		t.Errorf("Total = %d, want %d (registry + 3 discovered)", resp.Total, want)
	}
	if resp.Limit != 1 || resp.Offset != registry+1 {
		t.Errorf("Limit/Offset = %d/%d, want 1/%d", resp.Limit, resp.Offset, registry+1)
	}
	if !resp.Truncated {
		t.Errorf("Truncated = false for a %d-item page of %d", len(resp.Themes), resp.Total)
	}
	if len(resp.Themes) != 1 || resp.Themes[0].Label != "Bravo" {
		t.Fatalf("page = %+v, want the single Bravo entry", resp.Themes)
	}

	// A malformed limit must fall back to the default rather than hide the list.
	rec = httptest.NewRecorder()
	srv.serveHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/themes?limit=nope&offset=-4", nil))
	resp = decodeAppearanceThemes(t, rec)
	if resp.Limit != appearanceThemesDefaultLimit || resp.Offset != 0 {
		t.Fatalf("fallback Limit/Offset = %d/%d", resp.Limit, resp.Offset)
	}
	if len(resp.Themes) != registry+3 {
		t.Fatalf("themes = %d, want %d (registry + 3)", len(resp.Themes), registry+3)
	}
	if resp.Truncated {
		t.Errorf("Truncated = true for a full page of %d", resp.Total)
	}
}

// A fresh install has no extra extension roots. The response used to emit
// `"extra_extension_dirs":null`, which a client that maps the field would crash
// on; the key must be absent instead.
func TestHandleAppearanceSettingsOmitsEmptyExtraRoots(t *testing.T) {
	srv, _ := newAppearanceRouter(t)
	rec := httptest.NewRecorder()
	srv.serveHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"extra_extension_dirs"`) {
		t.Fatalf("body = %s, want no extra_extension_dirs key when there are no roots", rec.Body.String())
	}
}

// The unified list reports has_icon_theme for an extension that ships an icon
// theme alongside its colour themes. The existing tests only ever pinned the
// false case, so a broken flag (always false) would have gone unnoticed.
func TestHandleAppearanceThemesReportsIconThemeFlag(t *testing.T) {
	srv, root := newAppearanceRouter(t)
	extDir := installServerAppearanceExtension(t, root, "acme.both-1.0.0", "acme", "both", "Both Dark", "#101010")
	writeServerTestFile(t, filepath.Join(extDir, "package.json"), `{
  "name": "both",
  "publisher": "acme",
  "contributes": {
    "themes": [{"label": "Both Dark", "uiTheme": "vs-dark", "path": "themes/main.json"}],
    "iconThemes": [{"id": "both-icons", "label": "Both Icons", "path": "icons.json"}]
  }
}`)
	writeServerTestFile(t, filepath.Join(extDir, "icons.json"), `{"iconDefinitions":{}}`)

	rec := httptest.NewRecorder()
	srv.serveHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/themes?limit=1000", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeAppearanceThemes(t, rec)
	item, _ := findAppearanceTheme(t, resp.Themes, "vscode:acme.both/both-dark")
	if !item.HasIconTheme {
		t.Fatalf("HasIconTheme = false for %+v, want true (the extension ships an icon theme)", item)
	}
}

// Discovery warnings must reach the client: the settings page is where a user
// learns that an installed extension was skipped, and a warning the endpoint
// drops is a silent empty picker.
func TestHandleAppearanceThemesReturnsDiscoveryWarnings(t *testing.T) {
	srv, root := newAppearanceRouter(t)
	extDir := installServerAppearanceExtension(t, root, "acme.themes-1.0.0", "acme", "themes", "Acme Dark", "#101010")
	// A second contribution whose file does not exist is skipped with a warning.
	writeServerTestFile(t, filepath.Join(extDir, "package.json"), `{
  "name": "themes",
  "publisher": "acme",
  "contributes": {
    "themes": [
      {"label": "Acme Dark", "uiTheme": "vs-dark", "path": "themes/main.json"},
      {"label": "Gone", "uiTheme": "vs-dark", "path": "themes/gone.json"}
    ]
  }
}`)

	rec := httptest.NewRecorder()
	srv.serveHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/themes?limit=1000", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeAppearanceThemes(t, rec)
	if len(resp.Warnings) == 0 {
		t.Fatal("Warnings is empty, want the skipped contribution reported")
	}
	if !strings.Contains(strings.Join(resp.Warnings, "; "), "gone.json") {
		t.Fatalf("Warnings = %v, want one naming gone.json", resp.Warnings)
	}
}

func TestHandleAppearanceThemeRejectsInvalidAndUnknownIDs(t *testing.T) {
	srv, root := newAppearanceRouter(t)
	installServerAppearanceExtension(t, root, "acme.themes-1.0.0", "acme", "themes", "Acme Dark", "#101010")
	handler := srv.serveHandler()

	// `%2F` is NOT a path separator in the escaped path, so this reaches the
	// handler as one wildcard value and is rejected by the id grammar.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/theme/vscode:acme.themes/..%2F..%2Fetc%2Fpasswd", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed id status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/theme/vscode:nope/none", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id status = %d body=%s, want 404", rec.Code, rec.Body.String())
	}
}

// The two endpoints must agree: /api/theme?name=vscode:… is the legacy shape and
// /api/appearance/theme/{id} is the rich one; their colors are identical.
func TestGetThemeResolvesVSCodeID(t *testing.T) {
	srv, root := newAppearanceRouter(t)
	installServerAppearanceExtension(t, root, "acme.themes-1.0.0", "acme", "themes", "Acme Dark", "#123456")
	handler := srv.serveHandler()
	id := "vscode:acme.themes/acme-dark"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/theme/"+id, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("rich endpoint status = %d body=%s", rec.Code, rec.Body.String())
	}
	var rich appearance.ConvertedTheme
	if err := json.Unmarshal(rec.Body.Bytes(), &rich); err != nil {
		t.Fatalf("decode rich theme: %v", err)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/theme?name="+id, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/theme status = %d body=%s", rec.Code, rec.Body.String())
	}
	var legacy ThemeColorsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &legacy); err != nil {
		t.Fatalf("decode legacy theme: %v", err)
	}
	if legacy.Name != id {
		t.Errorf("legacy Name = %q, want %q", legacy.Name, id)
	}
	if !reflect.DeepEqual(legacy.Colors, rich.Colors) {
		t.Fatalf("colors disagree between endpoints:\n legacy=%+v\n rich=%+v", legacy.Colors, rich.Colors)
	}
	var zero theme.ThemeColors
	if legacy.Colors == zero {
		t.Fatal("legacy colors are empty")
	}
}

// A vscode: name that no longer resolves is a 404, never a silent substitution
// of the default palette.
func TestGetThemeUnknownVSCodeIDIsNotFound(t *testing.T) {
	srv, _ := newAppearanceRouter(t)
	rec := httptest.NewRecorder()
	srv.serveHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/theme?name=vscode:gone/theme", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s, want 404", rec.Code, rec.Body.String())
	}
}

// /api/themes keeps its shape and appends the discovered themes.
func TestListThemesAppendsVSCodeThemes(t *testing.T) {
	srv, root := newAppearanceRouter(t)
	installServerAppearanceExtension(t, root, "acme.themes-1.0.0", "acme", "themes", "Acme Dark", "#101010")

	rec := httptest.NewRecorder()
	srv.serveHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/themes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp ThemesListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var found bool
	for _, item := range resp.Themes {
		if item.Name == "vscode:acme.themes/acme-dark" {
			found = true
			if item.Label != "Acme Dark" {
				t.Errorf("label = %q", item.Label)
			}
		}
	}
	if !found {
		t.Fatalf("vscode theme not appended: %+v", resp.Themes)
	}
	if resp.Current == "" {
		t.Error("Current is empty")
	}
}

func TestAppearanceSettingsRoundTripOverHTTP(t *testing.T) {
	srv, root := newAppearanceRouter(t)
	installServerAppearanceExtension(t, root, "acme.themes-1.0.0", "acme", "themes", "Acme Dark", "#101010")
	handler := srv.serveHandler()

	body := `{"color_theme":"vscode:acme.themes/acme-dark","icon_theme":"","extra_extension_dirs":[" ` + root + ` ","","` + root + `"]}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/appearance/settings", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", rec.Code, rec.Body.String())
	}
	put := decodeAppearanceSettings(t, rec)
	if put.ColorTheme != "vscode:acme.themes/acme-dark" {
		t.Fatalf("PUT ColorTheme = %q", put.ColorTheme)
	}
	if put.EffectiveColorTheme != "vscode:acme.themes/acme-dark" {
		t.Fatalf("PUT EffectiveColorTheme = %q, want the stored id", put.EffectiveColorTheme)
	}
	if !reflect.DeepEqual(put.ExtraExtensionDirs, []string{root}) {
		t.Fatalf("PUT extra roots = %#v, want the normalized [%q]", put.ExtraExtensionDirs, root)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", rec.Code, rec.Body.String())
	}
	got := decodeAppearanceSettings(t, rec)
	if got.ColorTheme != put.ColorTheme || !reflect.DeepEqual(got.ExtraExtensionDirs, put.ExtraExtensionDirs) {
		t.Fatalf("GET = %+v, want the stored %+v", got, put)
	}

	// It reached disk, not just the handler's memory.
	stored, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	if stored.Appearance.ColorTheme != "vscode:acme.themes/acme-dark" {
		t.Fatalf("persisted ColorTheme = %q", stored.Appearance.ColorTheme)
	}
}

// A PUT naming a theme that does not exist must be rejected and must not touch
// the stored config.
func TestSetAppearanceSettingsRejectsUnknownTheme(t *testing.T) {
	srv, _ := newAppearanceRouter(t)
	handler := srv.serveHandler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/appearance/settings",
		strings.NewReader(`{"color_theme":"vscode:nope/none"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
	stored, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	if !stored.Appearance.IsZero() {
		t.Fatalf("rejected PUT persisted %+v", stored.Appearance)
	}

	// A malformed id is a 400 too, before any filesystem work.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/appearance/settings",
		strings.NewReader(`{"color_theme":"vscode:../../etc/passwd"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed id status = %d, want 400", rec.Code)
	}
}

// A write that ADDS an extra root and selects a theme living in it must succeed.
// Validation used to run against the service's previous roots, so the selection
// 400ed even though both halves of the request were valid together.
func TestSetAppearanceSettingsValidatesAgainstNewRoots(t *testing.T) {
	srv, _ := newAppearanceRouter(t)
	newRoot := t.TempDir()
	installServerAppearanceExtension(t, newRoot, "acme.themes-1.0.0", "acme", "themes", "Acme Dark", "#101010")
	handler := srv.serveHandler()

	body := `{"color_theme":"vscode:acme.themes/acme-dark","extra_extension_dirs":["` + newRoot + `"]}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/appearance/settings", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d body=%s, want 200 (the new root must be scannable before validation)", rec.Code, rec.Body.String())
	}
	got := decodeAppearanceSettings(t, rec)
	if got.EffectiveColorTheme != "vscode:acme.themes/acme-dark" {
		t.Fatalf("EffectiveColorTheme = %q, want the stored id", got.EffectiveColorTheme)
	}

	stored, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	if stored.Appearance.ColorTheme != "vscode:acme.themes/acme-dark" {
		t.Fatalf("persisted ColorTheme = %q", stored.Appearance.ColorTheme)
	}
	if !reflect.DeepEqual(stored.Appearance.ExtraExtensionDirs, []string{newRoot}) {
		t.Fatalf("persisted roots = %#v", stored.Appearance.ExtraExtensionDirs)
	}

	// A selection that is invalid against BOTH root sets still fails, and the
	// rejected write must leave the service scanning the stored roots.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/appearance/settings",
		strings.NewReader(`{"color_theme":"vscode:nope/none","extra_extension_dirs":["`+t.TempDir()+`"]}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid selection status = %d, want 400", rec.Code)
	}
	stored, err = config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	if !reflect.DeepEqual(stored.Appearance.ExtraExtensionDirs, []string{newRoot}) {
		t.Fatalf("rejected PUT persisted roots %#v, want the previous %q", stored.Appearance.ExtraExtensionDirs, newRoot)
	}
}

// A relative extra root would resolve against the process working directory, so
// the write is rejected rather than persisted and scanned.
func TestSetAppearanceSettingsRejectsRelativeExtraRoot(t *testing.T) {
	srv, _ := newAppearanceRouter(t)
	handler := srv.serveHandler()

	for _, body := range []string{
		`{"extra_extension_dirs":["relative/extensions"]}`,
		`{"extra_extension_dirs":["../sibling"]}`,
		`{"extra_extension_dirs":["~otheruser/extensions"]}`,
		// A bare "~" must not become the home directory as an extensions root.
		`{"extra_extension_dirs":["~"]}`,
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/appearance/settings", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("PUT %s = %d body=%s, want 400", body, rec.Code, rec.Body.String())
		}
	}
	stored, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	if !stored.Appearance.IsZero() {
		t.Fatalf("rejected PUT persisted %+v", stored.Appearance)
	}

	// "~/" and an absolute path are still accepted.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/appearance/settings",
		strings.NewReader(`{"extra_extension_dirs":["~/extensions","/opt/extensions"]}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("valid roots PUT = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
}

// appearanceInterleaveSlowRootExtensions is how many extension directories the
// rejected write in TestSetAppearanceSettingsConcurrentWritesKeepServiceAndDiskInSync
// asks the validator to scan. It only has to outlast one accepted save; this many
// tiny manifests gives an order-of-magnitude margin on any machine while keeping
// the setup cheap.
const appearanceInterleaveSlowRootExtensions = 400

// Two interleaved PUTs must never leave the live discovery service scanning
// roots different from what is on disk.
//
// The rejected write used to apply its candidate roots to the LIVE service
// before validating, then restore "its" idea of the previous roots when
// validation failed — so a slow rejection could undo the roots an accepted write
// had just installed. The rejected request here scans a large root, which holds
// it inside Resolve long enough for the accepted write to save. With the fix,
// validation runs against a throwaway service and only the accepted write
// touches the live one.
func TestSetAppearanceSettingsConcurrentWritesKeepServiceAndDiskInSync(t *testing.T) {
	srv, _ := newAppearanceRouter(t)
	handler := srv.serveHandler()
	svc := srv.handler.appearance

	validRoot := t.TempDir()
	installServerAppearanceExtension(t, validRoot, "acme.themes-1.0.0", "acme", "themes", "Acme Dark", "#101010")

	slowRoot := t.TempDir()
	for i := 0; i < appearanceInterleaveSlowRootExtensions; i++ {
		writeServerTestFile(t, filepath.Join(slowRoot, fmt.Sprintf("ext%04d", i), "package.json"),
			`{"name":"e","publisher":"p","contributes":{}}`)
	}

	rejected := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/appearance/settings",
			strings.NewReader(`{"color_theme":"vscode:nope/none","extra_extension_dirs":["`+slowRoot+`"]}`)))
		rejected <- rec.Code
	}()
	// Let the rejected write get past its (now throwaway) root selection and
	// into the scan, so the accepted save lands while the rejection is in flight.
	time.Sleep(10 * time.Millisecond)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/appearance/settings",
		strings.NewReader(`{"color_theme":"vscode:acme.themes/acme-dark","extra_extension_dirs":["`+validRoot+`"]}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("accepted PUT = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if code := <-rejected; code != http.StatusBadRequest {
		t.Fatalf("rejected PUT = %d, want 400", code)
	}

	stored, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	if want := []string{validRoot}; !reflect.DeepEqual(stored.Appearance.ExtraExtensionDirs, want) {
		t.Fatalf("disk roots = %#v, want %#v", stored.Appearance.ExtraExtensionDirs, want)
	}
	if got := svc.ExtraRoots(); !reflect.DeepEqual(got, stored.Appearance.ExtraExtensionDirs) {
		t.Fatalf("service roots = %#v, disk roots = %#v: a rejected write must never change the live service",
			got, stored.Appearance.ExtraExtensionDirs)
	}
}

// Concurrent accepted PUTs must converge: whichever save lands last must also be
// the root set the live service scans. The save, the read-back and the apply run
// under one lock, so the pair can never disagree.
func TestSetAppearanceSettingsConcurrentAcceptedWritesConverge(t *testing.T) {
	srv, _ := newAppearanceRouter(t)
	handler := srv.serveHandler()
	svc := srv.handler.appearance

	const writers = 8
	roots := make([]string, writers)
	for i := range roots {
		roots[i] = t.TempDir()
	}

	var wg sync.WaitGroup
	codes := make(chan int, writers)
	for _, root := range roots {
		wg.Add(1)
		go func(root string) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/appearance/settings",
				strings.NewReader(`{"extra_extension_dirs":["`+root+`"]}`)))
			codes <- rec.Code
		}(root)
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != http.StatusOK {
			t.Fatalf("concurrent PUT = %d, want 200", code)
		}
	}

	stored, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	if got := svc.ExtraRoots(); !reflect.DeepEqual(got, stored.Appearance.ExtraExtensionDirs) {
		t.Fatalf("service roots = %#v, disk roots = %#v: the last save must also be the applied roots",
			got, stored.Appearance.ExtraExtensionDirs)
	}
}

// The legacy endpoint must reject a malformed vscode: id with 400, not surface
// the service's "invalid theme id" as a 500. /api/appearance/theme/{id} already
// enforced the grammar; /api/theme?name= only checked the prefix.
func TestGetThemeRejectsMalformedVSCodeID(t *testing.T) {
	srv, _ := newAppearanceRouter(t)
	rec := httptest.NewRecorder()
	srv.serveHandler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/api/theme?name=vscode:../../etc/passwd", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestGetAppearanceSettingsFallsBackToTUIThemeThenTokyoNight(t *testing.T) {
	srv, _ := newAppearanceRouter(t)
	handler := srv.serveHandler()

	// A stored theme from an extension that is no longer installed.
	srv.handler.mu.Lock()
	srv.handler.cfg.Ocode.Appearance.ColorTheme = "vscode:gone/theme"
	srv.handler.cfg.Ocode.TUI.Theme = "gruvbox"
	srv.handler.mu.Unlock()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/settings", nil))
	got := decodeAppearanceSettings(t, rec)
	if got.EffectiveColorTheme != "gruvbox" {
		t.Fatalf("EffectiveColorTheme = %q, want tui.theme gruvbox", got.EffectiveColorTheme)
	}
	if len(got.Warnings) == 0 || !strings.Contains(got.Warnings[0], "vscode:gone/theme") {
		t.Fatalf("Warnings = %v, want one naming the unresolvable id", got.Warnings)
	}
	if got.ColorTheme != "vscode:gone/theme" {
		t.Fatalf("stored ColorTheme = %q, want the raw stored value", got.ColorTheme)
	}

	srv.handler.mu.Lock()
	srv.handler.cfg.Ocode.TUI.Theme = ""
	srv.handler.mu.Unlock()
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/settings", nil))
	got = decodeAppearanceSettings(t, rec)
	if got.EffectiveColorTheme != "tokyonight" {
		t.Fatalf("EffectiveColorTheme = %q, want tokyonight", got.EffectiveColorTheme)
	}
}

func TestGetAppearanceSettingsWithoutSelectionUsesTUITheme(t *testing.T) {
	srv, _ := newAppearanceRouter(t)
	srv.handler.mu.Lock()
	srv.handler.cfg.Ocode.TUI.Theme = "tokyonight-storm"
	srv.handler.mu.Unlock()

	rec := httptest.NewRecorder()
	srv.serveHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/settings", nil))
	got := decodeAppearanceSettings(t, rec)
	if got.EffectiveColorTheme != "tokyonight-storm" {
		t.Fatalf("EffectiveColorTheme = %q, want tokyonight-storm", got.EffectiveColorTheme)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none when nothing was configured", got.Warnings)
	}
}

// Every appearance route is registered on the authenticated mux. A missing route
// would fall through to the SPA handler and answer 200 with HTML, which is why
// this drives the real serve handler instead of calling methods directly.
func TestAppearanceRoutesAreRegisteredAndAuthenticated(t *testing.T) {
	srv, root := newAppearanceRouter(t)
	installServerAppearanceExtension(t, root, "acme.themes-1.0.0", "acme", "themes", "Acme Dark", "#101010")
	handler := srv.serveHandler()

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/appearance/themes"},
		{http.MethodGet, "/api/appearance/theme/vscode:acme.themes/acme-dark"},
		{http.MethodGet, "/api/appearance/settings"},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s = %d body=%s (404/HTML means the route is missing)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("%s %s Content-Type = %q, want JSON", tc.method, tc.path, ct)
		}
	}

	// With credentials configured the same routes must reject an anonymous call.
	authSrv := New("127.0.0.1:0", "appearance-user", "appearance-pass", nil)
	authHTTP := authSrv.serveHandler()
	rec := httptest.NewRecorder()
	authHTTP.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/themes", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET = %d, want 401", rec.Code)
	}
	rec = httptest.NewRecorder()
	authHTTP.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/theme/vscode:acme.themes/acme-dark", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated theme GET = %d, want 401", rec.Code)
	}
	rec = httptest.NewRecorder()
	authHTTP.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/appearance/settings", strings.NewReader(`{}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated PUT = %d, want 401", rec.Code)
	}
}

// Appearance is local-only: the routes take no host parameter, and a
// remote-prefixed path is refused by the proxy gate instead of reaching the
// local handlers.
func TestAppearanceRoutesAreLocalOnly(t *testing.T) {
	srv, root := newAppearanceRouter(t)
	installServerAppearanceExtension(t, root, "acme.themes-1.0.0", "acme", "themes", "Acme Dark", "#101010")
	handler := srv.serveHandler()

	plain := httptest.NewRecorder()
	handler.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/api/appearance/themes", nil))
	withHost := httptest.NewRecorder()
	handler.ServeHTTP(withHost, httptest.NewRequest(http.MethodGet, "/api/appearance/themes?host=some-remote-host", nil))
	if plain.Code != http.StatusOK || withHost.Code != http.StatusOK {
		t.Fatalf("status plain=%d withHost=%d", plain.Code, withHost.Code)
	}
	if plain.Body.String() != withHost.Body.String() {
		t.Fatalf("?host= changed the response:\n plain=%s\n withHost=%s", plain.Body.String(), withHost.Body.String())
	}

	// The remote proxy catch-all exists, but the host is not a saved project, so
	// the request never touches an appearance handler (and never returns its
	// JSON). This only covers the unknown-host path: a SAVED host is the case
	// that used to be proxied, and TestRemoteProxyRefusesAppearanceForSavedHost
	// is the assertion that matters.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/remote/nosuchhost/api/appearance/themes", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("remote-prefixed appearance request = 200 body=%s, want refusal", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "vscode:acme.themes/acme-dark") {
		t.Fatalf("remote-prefixed request saw the local theme list: %s", rec.Body.String())
	}
}

// A SAVED remote host must be refused before any connect: the route exists for
// saved projects, so the unknown-host case above passes without exercising the
// rule at all. Appearance is local-only (spec §12) even when the host is real.
func TestRemoteProxyRefusesAppearanceForSavedHost(t *testing.T) {
	installFakeSSH(t)
	projectDir := t.TempDir()
	h := newTestHandlerWithRemote(t, "ci.local", projectDir)
	var connectCalled atomic.Bool
	h.remoteHosts = newTestRegistry(func(remote.Target, string) (remoteHostWorkspace, error) {
		connectCalled.Store(true)
		return nil, fmt.Errorf("connect must not start for a local-only route")
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet,
		"/api/remote/ci.local/api/appearance/themes?token=local&project="+url.QueryEscape(projectDir), nil)
	setPathValues(r, map[string]string{"host": "ci.local", "rest": "appearance/themes"})
	h.HandleRemoteProxy(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s, want 403", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "local-only") {
		t.Fatalf("body = %s, want the local-only rule named", w.Body.String())
	}
	if connectCalled.Load() {
		t.Fatal("a connect was started for /api/appearance/*")
	}
}

// The handlers must not panic and must not pretend an empty list when no service
// was constructed.
func TestAppearanceHandlersWithoutService(t *testing.T) {
	srv, root := newAppearanceRouter(t)
	installServerAppearanceExtension(t, root, "acme.themes-1.0.0", "acme", "themes", "Acme Dark", "#101010")
	srv.handler.mu.Lock()
	srv.handler.appearance = nil
	srv.handler.mu.Unlock()
	handler := srv.serveHandler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/themes", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("themes without service = %d body=%s, want 503", rec.Code, rec.Body.String())
	}

	// Settings still answers, with the configured/builtin fallback and no panic.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("settings without service = %d body=%s", rec.Code, rec.Body.String())
	}
	got := decodeAppearanceSettings(t, rec)
	if got.EffectiveColorTheme == "" {
		t.Fatal("EffectiveColorTheme is empty")
	}

	// /api/themes keeps serving the builtin registry.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/themes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/themes without service = %d", rec.Code)
	}
}

// The converted theme endpoint runs discovery outside the handler lock, so a
// concurrent list must not deadlock or race (also covered by -race).
func TestAppearanceConcurrentReads(t *testing.T) {
	srv, root := newAppearanceRouter(t)
	installServerAppearanceExtension(t, root, "acme.themes-1.0.0", "acme", "themes", "Acme Dark", "#101010")
	handler := srv.serveHandler()

	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/themes", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("concurrent GET = %d", rec.Code)
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	_ = context.Background()
}

// A built-in name that the registry does not know is just as unresolvable as a
// removed extension theme: it must warn and fall back, and the fallback itself
// must not name another unregistered theme.
func TestGetAppearanceSettingsWarnsOnUnknownBuiltinTheme(t *testing.T) {
	srv, _ := newAppearanceRouter(t)
	srv.handler.mu.Lock()
	srv.handler.cfg.Ocode.Appearance.ColorTheme = "not-a-theme"
	srv.handler.cfg.Ocode.TUI.Theme = "also-not-a-theme"
	srv.handler.mu.Unlock()

	rec := httptest.NewRecorder()
	srv.serveHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/settings", nil))
	got := decodeAppearanceSettings(t, rec)
	if got.EffectiveColorTheme != "tokyonight" {
		t.Fatalf("EffectiveColorTheme = %q, want tokyonight", got.EffectiveColorTheme)
	}
	if len(got.Warnings) == 0 || !strings.Contains(got.Warnings[0], "not a known built-in theme") {
		t.Fatalf("Warnings = %v, want one about an unknown built-in theme", got.Warnings)
	}
}
