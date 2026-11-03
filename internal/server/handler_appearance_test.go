package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/appearance"
	"github.com/u007/ocode/internal/config"
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
	srv.serveHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/themes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/appearance/themes = %d body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeAppearanceThemes(t, rec)
	if resp.Total != 1 || len(resp.Themes) != 1 {
		t.Fatalf("themes = %+v, want one", resp)
	}
	item := resp.Themes[0]
	if item.ID != "vscode:acme.themes/acme-dark" {
		t.Errorf("ID = %q", item.ID)
	}
	if item.Label != "Acme Dark" || item.Source != "vscode" || item.Kind != appearance.PolarityDark {
		t.Errorf("item = %+v", item)
	}
	if item.Extension != "acme.themes" {
		t.Errorf("Extension = %q", item.Extension)
	}
}

func TestHandleAppearanceThemesPaginates(t *testing.T) {
	srv, root := newAppearanceRouter(t)
	installServerAppearanceExtension(t, root, "acme.one-1.0.0", "acme", "one", "Alpha", "#101010")
	installServerAppearanceExtension(t, root, "acme.two-1.0.0", "acme", "two", "Bravo", "#101010")
	installServerAppearanceExtension(t, root, "acme.three-1.0.0", "acme", "three", "Charlie", "#101010")

	rec := httptest.NewRecorder()
	srv.serveHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/appearance/themes?limit=1&offset=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeAppearanceThemes(t, rec)
	if resp.Total != 3 {
		t.Errorf("Total = %d, want 3", resp.Total)
	}
	if resp.Limit != 1 || resp.Offset != 1 {
		t.Errorf("Limit/Offset = %d/%d, want 1/1", resp.Limit, resp.Offset)
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
	if len(resp.Themes) != 3 {
		t.Fatalf("themes = %d, want 3", len(resp.Themes))
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
	srv, _ := newAppearanceRouter(t)
	handler := srv.serveHandler()

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/appearance/themes"},
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
	// JSON).
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/remote/nosuchhost/api/appearance/themes", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("remote-prefixed appearance request = 200 body=%s, want refusal", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "vscode:acme.themes/acme-dark") {
		t.Fatalf("remote-prefixed request saw the local theme list: %s", rec.Body.String())
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
