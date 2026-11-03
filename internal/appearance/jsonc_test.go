package appearance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDecodeJSONCPreservesCommentMarkersInsideStrings(t *testing.T) {
	data := []byte(`{"colors":{"textLink.foreground":"https://x.example/a//b"}, /* c */ "type":"dark",}`)
	var out map[string]any
	if err := DecodeJSONC(data, &out); err != nil {
		t.Fatal(err)
	}
	got := out["colors"].(map[string]any)["textLink.foreground"]
	if got != "https://x.example/a//b" {
		t.Fatalf("got %v", got)
	}
}

func TestDecodeJSONCCommentsAndTrailingCommasFromFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "jsonc", "comments.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var theme RawTheme
	if err := DecodeJSONC(data, &theme); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if theme.Type != "dark" {
		t.Fatalf("type = %q, want dark", theme.Type)
	}
	if theme.Name != "Comment Sample" {
		t.Fatalf("name = %q, want Comment Sample", theme.Name)
	}
	if got := theme.Colors["editor.background"]; got != "#101010" {
		t.Fatalf("editor.background = %q, want #101010", got)
	}
	// The "//" inside the URL must not be treated as a comment marker.
	if got := theme.Colors["textLink.foreground"]; got != "https://example.test/a//b" {
		t.Fatalf("textLink.foreground = %q, want the URL verbatim", got)
	}
}

func TestLoadThemeTreeMergesIncludeAndChildWins(t *testing.T) {
	theme, warn, err := LoadThemeTree("testdata/include/child.json", "testdata/include", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warn) != 0 {
		t.Fatalf("unexpected warnings: %v", warn)
	}
	// Inherited from base.json.
	if got := theme.Colors["editor.foreground"]; got != "#eeeeee" {
		t.Fatalf("editor.foreground = %q, want the inherited #eeeeee", got)
	}
	// The child overrides the same key the base layer set.
	if got := theme.Colors["editor.background"]; got != "#111111" {
		t.Fatalf("editor.background = %q, want the child's #111111", got)
	}
	// A child without tokenColors keeps the included layer's rules.
	if len(theme.TokenColors.Rules) != 1 {
		t.Fatalf("rules = %#v, want the one inherited comment rule", theme.TokenColors.Rules)
	}
	rule := theme.TokenColors.Rules[0]
	if len(rule.Scope) != 1 || rule.Scope[0] != "comment" {
		t.Fatalf("scope = %#v, want [comment]", rule.Scope)
	}
	if rule.Settings.Foreground != "#888888" || rule.Settings.FontStyle != "italic" {
		t.Fatalf("settings = %#v, want the inherited comment settings", rule.Settings)
	}
}

func TestLoadThemeTreeRejectsEscapingInclude(t *testing.T) {
	theme, warn, err := LoadThemeTree("testdata/include/escape.json", "testdata/include", 0)
	if err != nil {
		t.Fatalf("an escaping include must not be fatal: %v", err)
	}
	if len(warn) != 1 {
		t.Fatalf("got %d warnings (%v), want exactly 1", len(warn), warn)
	}
	if !strings.Contains(strings.ToLower(warn[0]), "containment") {
		t.Fatalf("warning %q does not mention containment", warn[0])
	}
	if theme == nil {
		t.Fatal("theme is nil, want the child colours that were read")
	}
	// Nothing from the rejected include may appear.
	if got, ok := theme.Colors["editor.foreground"]; ok {
		t.Fatalf("inherited editor.foreground = %q from an escaping include", got)
	}
	if got, ok := theme.Colors["sideBar.background"]; ok {
		t.Fatalf("inherited sideBar.background = %q from an escaping include", got)
	}
	if got := theme.Colors["editor.background"]; got != "#222222" {
		t.Fatalf("editor.background = %q, want the file's own #222222", got)
	}
}

func TestTokenColorsAcceptsPathString(t *testing.T) {
	const raw = `{"name":"Path Sample","type":"dark","tokenColors":"./theme.tmTheme"}`

	var theme RawTheme
	if err := DecodeJSONC([]byte(raw), &theme); err != nil {
		t.Fatalf("decode theme: %v", err)
	}
	if theme.TokenColors.Rules != nil {
		t.Fatalf("Rules = %#v, want nil for a path tokenColors", theme.TokenColors.Rules)
	}
	if theme.TokenColors.File != "./theme.tmTheme" {
		t.Fatalf("File = %q, want ./theme.tmTheme", theme.TokenColors.File)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "theme.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write temp theme: %v", err)
	}
	loaded, warn, err := LoadThemeTree(path, dir, 0)
	if err != nil {
		t.Fatalf("load theme: %v", err)
	}
	if len(warn) != 1 {
		t.Fatalf("got %d warnings (%v), want exactly 1 for a non-JSON tokenColors", len(warn), warn)
	}
	if !strings.Contains(warn[0], "JSON") {
		t.Fatalf("warning %q does not explain the JSON requirement", warn[0])
	}
	if loaded.TokenColors.Rules != nil {
		t.Fatalf("Rules = %#v, want nil when the tokenColors file is not JSON", loaded.TokenColors.Rules)
	}
	if loaded.TokenColors.File != "./theme.tmTheme" {
		t.Fatalf("File = %q, want the raw reference", loaded.TokenColors.File)
	}
}

func TestTokenColorsScopeListAcceptsStringOrArray(t *testing.T) {
	const raw = `{"tokenColors":[
		{"scope":"comment","settings":{"foreground":"#111111"}},
		{"scope":["string","constant.numeric"],"settings":{"fontStyle":"italic"}}
	]}`
	var theme RawTheme
	if err := DecodeJSONC([]byte(raw), &theme); err != nil {
		t.Fatalf("decode theme: %v", err)
	}
	if len(theme.TokenColors.Rules) != 2 {
		t.Fatalf("rules = %#v, want 2", theme.TokenColors.Rules)
	}
	single := theme.TokenColors.Rules[0]
	if len(single.Scope) != 1 || single.Scope[0] != "comment" {
		t.Fatalf("string scope = %#v, want [comment]", single.Scope)
	}
	multiple := theme.TokenColors.Rules[1]
	want := []string{"string", "constant.numeric"}
	if len(multiple.Scope) != len(want) {
		t.Fatalf("array scope = %#v, want %#v", multiple.Scope, want)
	}
	for i, scope := range want {
		if multiple.Scope[i] != scope {
			t.Fatalf("array scope = %#v, want %#v", multiple.Scope, want)
		}
	}
	if multiple.Settings.FontStyle != "italic" {
		t.Fatalf("fontStyle = %q, want italic", multiple.Settings.FontStyle)
	}
}

func TestTokenColorsLoadsJSONFile(t *testing.T) {
	dir := t.TempDir()
	tokens := `[{"scope":"comment","settings":{"foreground":"#abcdef","fontStyle":"italic"}}]`
	if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte(tokens), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	theme := `{"name":"External Tokens","type":"dark","tokenColors":"./tokens.json"}`
	path := filepath.Join(dir, "theme.json")
	if err := os.WriteFile(path, []byte(theme), 0o600); err != nil {
		t.Fatalf("write theme: %v", err)
	}

	loaded, warn, err := LoadThemeTree(path, dir, 0)
	if err != nil {
		t.Fatalf("load theme: %v", err)
	}
	if len(warn) != 0 {
		t.Fatalf("unexpected warnings: %v", warn)
	}
	if len(loaded.TokenColors.Rules) != 1 {
		t.Fatalf("rules = %#v, want the one rule from tokens.json", loaded.TokenColors.Rules)
	}
	rule := loaded.TokenColors.Rules[0]
	if len(rule.Scope) != 1 || rule.Scope[0] != "comment" {
		t.Fatalf("scope = %#v, want [comment]", rule.Scope)
	}
	if rule.Settings.Foreground != "#abcdef" || rule.Settings.FontStyle != "italic" {
		t.Fatalf("settings = %#v, want the loaded rule settings", rule.Settings)
	}
	if loaded.TokenColors.File != "./tokens.json" {
		t.Fatalf("File = %q, want the raw reference", loaded.TokenColors.File)
	}
}

func TestLoadThemeTreeStopsAtIncludeDepthLimit(t *testing.T) {
	dir := t.TempDir()
	const levels = 8
	for i := 0; i < levels; i++ {
		body := `{"name":"level ` + string(rune('0'+i)) + `","colors":{"level.` + string(rune('0'+i)) + `":"#00000` + string(rune('0'+i)) + `"}}`
		if i+1 < levels {
			body = `{"name":"level ` + string(rune('0'+i)) + `","include":"./level` + string(rune('0'+i+1)) + `.json","colors":{"level.` + string(rune('0'+i)) + `":"#00000` + string(rune('0'+i)) + `"}}`
		}
		path := filepath.Join(dir, "level"+string(rune('0'+i))+".json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write level %d: %v", i, err)
		}
	}

	theme, warn, err := LoadThemeTree(filepath.Join(dir, "level0.json"), dir, 0)
	if err != nil {
		t.Fatalf("depth overflow must not be fatal: %v", err)
	}
	if len(warn) != 1 {
		t.Fatalf("got %d warnings (%v), want exactly 1 depth warning", len(warn), warn)
	}
	if !strings.Contains(warn[0], "depth limit") {
		t.Fatalf("warning %q does not mention the depth limit", warn[0])
	}
	// Levels 0..5 are loadable; the sixth hop is refused.
	if _, ok := theme.Colors["level.5"]; !ok {
		t.Fatalf("level.5 missing from %#v; the cap stopped one level too early", theme.Colors)
	}
	if _, ok := theme.Colors["level.6"]; ok {
		t.Fatalf("level.6 present in %#v; the depth cap did not stop the chain", theme.Colors)
	}
}

func TestLoadThemeTreeResolvesIncludeRelativeToIncludingFile(t *testing.T) {
	root := t.TempDir()
	themes := filepath.Join(root, "themes")
	if err := os.MkdirAll(themes, 0o755); err != nil {
		t.Fatalf("mkdir themes: %v", err)
	}
	write := func(name, body string) string {
		path := filepath.Join(themes, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}
	write("base.json", `{"colors":{"editor.foreground":"#eeeeee"}}`)
	child := write("child.json", `{"include":"./base.json","colors":{"editor.background":"#111111"}}`)

	theme, warn, err := LoadThemeTree(child, root, 0)
	if err != nil {
		t.Fatalf("load theme: %v", err)
	}
	if len(warn) != 0 {
		t.Fatalf("unexpected warnings: %v", warn)
	}
	// base.json exists only beside the child. A root-relative join looks for
	// <root>/base.json, misses it, and drops the inherited colour.
	if got := theme.Colors["editor.foreground"]; got != "#eeeeee" {
		t.Fatalf("editor.foreground = %q, want the inherited #eeeeee", got)
	}
	if got := theme.Colors["editor.background"]; got != "#111111" {
		t.Fatalf("editor.background = %q, want the child's #111111", got)
	}
}

func TestLoadThemeTreeRejectsEmptyContainmentRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "theme.json")
	if err := os.WriteFile(path, []byte(`{"colors":{"editor.background":"#111111"}}`), 0o600); err != nil {
		t.Fatalf("write theme: %v", err)
	}
	for _, root := range []string{"", "   ", "\t\n"} {
		theme, _, err := LoadThemeTree(path, root, 0)
		if err == nil {
			t.Fatalf("root %q accepted, want an empty-root error", root)
		}
		if theme != nil {
			t.Fatalf("root %q returned a theme alongside its error", root)
		}
		if !strings.Contains(err.Error(), "containment root is empty") {
			t.Fatalf("root %q error = %q, want it to name the empty containment root", root, err)
		}
	}
}

func TestLoadThemeTreeCapsThemeFileSize(t *testing.T) {
	oversize := []byte(strings.Repeat(" ", maxThemeFileBytes+1))

	t.Run("entry file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "theme.json")
		if err := os.WriteFile(path, oversize, 0o600); err != nil {
			t.Fatalf("write oversize theme: %v", err)
		}
		theme, _, err := LoadThemeTree(path, dir, 0)
		if err == nil {
			t.Fatal("an oversize entry file must be fatal")
		}
		if theme != nil {
			t.Fatalf("theme = %#v, want nil on a fatal read", theme)
		}
		if !strings.Contains(err.Error(), "limit") {
			t.Fatalf("error %q does not mention the size limit", err)
		}
	})

	t.Run("include", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "base.json"), oversize, 0o600); err != nil {
			t.Fatalf("write oversize include: %v", err)
		}
		path := filepath.Join(dir, "child.json")
		if err := os.WriteFile(path, []byte(`{"include":"./base.json","colors":{"editor.background":"#111111"}}`), 0o600); err != nil {
			t.Fatalf("write child: %v", err)
		}
		theme, warn, err := LoadThemeTree(path, dir, 0)
		if err != nil {
			t.Fatalf("an oversize include must not be fatal: %v", err)
		}
		if len(warn) != 1 || !strings.Contains(warn[0], "limit") {
			t.Fatalf("warnings = %v, want exactly one naming the size limit", warn)
		}
		if got := theme.Colors["editor.background"]; got != "#111111" {
			t.Fatalf("editor.background = %q, want the file's own #111111", got)
		}
	})

	t.Run("tokenColors reference", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "tokens.json"), oversize, 0o600); err != nil {
			t.Fatalf("write oversize tokens: %v", err)
		}
		path := filepath.Join(dir, "theme.json")
		if err := os.WriteFile(path, []byte(`{"tokenColors":"./tokens.json"}`), 0o600); err != nil {
			t.Fatalf("write theme: %v", err)
		}
		theme, warn, err := LoadThemeTree(path, dir, 0)
		if err != nil {
			t.Fatalf("an oversize tokenColors reference must not be fatal: %v", err)
		}
		if len(warn) != 1 || !strings.Contains(warn[0], "limit") {
			t.Fatalf("warnings = %v, want exactly one naming the size limit", warn)
		}
		if len(theme.TokenColors.Rules) != 0 {
			t.Fatalf("rules = %#v, want none from an oversize reference", theme.TokenColors.Rules)
		}
	})
}

func TestTokenColorsRejectsEscapingReference(t *testing.T) {
	dir := t.TempDir()
	themes := filepath.Join(dir, "themes")
	if err := os.MkdirAll(themes, 0o755); err != nil {
		t.Fatalf("mkdir themes: %v", err)
	}
	// The target exists and holds a rule; containment must refuse it anyway.
	outside := `[{"scope":"comment","settings":{"foreground":"#999999"}}]`
	if err := os.WriteFile(filepath.Join(dir, "outside.json"), []byte(outside), 0o600); err != nil {
		t.Fatalf("write outside tokens: %v", err)
	}
	path := filepath.Join(themes, "theme.json")
	if err := os.WriteFile(path, []byte(`{"tokenColors":"../outside.json"}`), 0o600); err != nil {
		t.Fatalf("write theme: %v", err)
	}

	theme, warn, err := LoadThemeTree(path, themes, 0)
	if err != nil {
		t.Fatalf("an escaping tokenColors reference must not be fatal: %v", err)
	}
	if len(warn) != 1 {
		t.Fatalf("got %d warnings (%v), want exactly 1", len(warn), warn)
	}
	if !strings.Contains(warn[0], "rejected") {
		t.Fatalf("warning %q does not say the reference was rejected", warn[0])
	}
	if len(theme.TokenColors.Rules) != 0 {
		t.Fatalf("rules = %#v, want none from an escaping reference", theme.TokenColors.Rules)
	}
}

func TestLoadThemeTreeNeverPairsTokenFileWithForeignRules(t *testing.T) {
	writeTheme := func(t *testing.T, dir, name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}

	t.Run("base rules with a child file", func(t *testing.T) {
		dir := t.TempDir()
		writeTheme(t, dir, "base.json", `{"tokenColors":[{"scope":"comment","settings":{"foreground":"#111111"}}]}`)
		child := writeTheme(t, dir, "child.json", `{"include":"./base.json","tokenColors":"./theme.tmTheme"}`)

		theme, warn, err := LoadThemeTree(child, dir, 0)
		if err != nil {
			t.Fatalf("load theme: %v", err)
		}
		// The .tmTheme reference is not JSON, so it yields one warning and no rules.
		if len(warn) != 1 {
			t.Fatalf("got %d warnings (%v), want exactly 1", len(warn), warn)
		}
		if len(theme.TokenColors.Rules) != 1 {
			t.Fatalf("rules = %#v, want the inherited comment rule", theme.TokenColors.Rules)
		}
		if theme.TokenColors.File != "" {
			t.Fatalf("File = %q, want it cleared: the rules came from base.json, not the child's reference", theme.TokenColors.File)
		}
	})

	t.Run("base file with child rules", func(t *testing.T) {
		dir := t.TempDir()
		writeTheme(t, dir, "base.json", `{"tokenColors":"./base.tmTheme"}`)
		child := writeTheme(t, dir, "child.json", `{"include":"./base.json","tokenColors":[{"scope":"string","settings":{"foreground":"#222222"}}]}`)

		theme, warn, err := LoadThemeTree(child, dir, 0)
		if err != nil {
			t.Fatalf("load theme: %v", err)
		}
		if len(warn) != 1 {
			t.Fatalf("got %d warnings (%v), want exactly 1 for the base .tmTheme", len(warn), warn)
		}
		if len(theme.TokenColors.Rules) != 1 || theme.TokenColors.Rules[0].Settings.Foreground != "#222222" {
			t.Fatalf("rules = %#v, want the child's #222222 rule", theme.TokenColors.Rules)
		}
		if theme.TokenColors.File != "" {
			t.Fatalf("File = %q, want it cleared: the rules came from the child, not the base's reference", theme.TokenColors.File)
		}
	})
}

func TestLoadThemeTreeHandlesIncludeCycle(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("a.json", `{"name":"A","include":"./b.json","colors":{"cycle.a":"#aaaaaa"}}`)
	write("b.json", `{"name":"B","include":"./a.json","colors":{"cycle.b":"#bbbbbb"}}`)

	type loadResult struct {
		theme *RawTheme
		warn  []string
		err   error
	}
	done := make(chan loadResult, 1)
	go func() {
		theme, warn, err := LoadThemeTree(filepath.Join(dir, "a.json"), dir, 0)
		done <- loadResult{theme: theme, warn: warn, err: err}
	}()
	var res loadResult
	select {
	case res = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("an include cycle did not terminate")
	}
	if res.err != nil {
		t.Fatalf("an include cycle must not be fatal: %v", res.err)
	}
	if len(res.warn) != 1 || !strings.Contains(res.warn[0], "depth limit") {
		t.Fatalf("warnings = %v, want exactly one depth-limit warning", res.warn)
	}
	if got := res.theme.Colors["cycle.a"]; got != "#aaaaaa" {
		t.Fatalf("cycle.a = %q, want the outer file's colour", got)
	}
	if got := res.theme.Colors["cycle.b"]; got != "#bbbbbb" {
		t.Fatalf("cycle.b = %q, want the included file's colour", got)
	}
}

func TestTokenColorsScopeListSplitsCommaSeparatedString(t *testing.T) {
	const raw = `{"tokenColors":[{"scope":"comment, string","settings":{"foreground":"#222222"}}]}`
	var theme RawTheme
	if err := DecodeJSONC([]byte(raw), &theme); err != nil {
		t.Fatalf("decode theme: %v", err)
	}
	if len(theme.TokenColors.Rules) != 1 {
		t.Fatalf("rules = %#v, want 1", theme.TokenColors.Rules)
	}
	want := []string{"comment", "string"}
	got := theme.TokenColors.Rules[0].Scope
	if len(got) != len(want) {
		t.Fatalf("scope = %#v, want %#v", got, want)
	}
	for i, scope := range want {
		if got[i] != scope {
			t.Fatalf("scope = %#v, want %#v", got, want)
		}
	}
}

func TestDecodeJSONCTrailingCommaInsideStringValue(t *testing.T) {
	data := []byte(`{"a":"x,}","b":"y,]"} `)
	var out map[string]string
	if err := DecodeJSONC(data, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["a"] != "x,}" {
		t.Fatalf("a = %q, want %q", out["a"], "x,}")
	}
	if out["b"] != "y,]" {
		t.Fatalf("b = %q, want %q", out["b"], "y,]")
	}
}

func TestDecodeJSONCBlockCommentInsideStringValue(t *testing.T) {
	data := []byte(`{"a":"x /* not a comment */ y"}`)
	var out map[string]string
	if err := DecodeJSONC(data, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["a"] != "x /* not a comment */ y" {
		t.Fatalf("a = %q, want the block-comment markers verbatim", out["a"])
	}
}
