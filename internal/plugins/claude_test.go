package plugins

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/bundled"
)

// isolatePluginEnv points HOME, the cwd and the bundled plugin dir at empty
// temp dirs so only the fixtures a test writes are discovered. It returns the
// fake home.
func isolatePluginEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Chdir(t.TempDir())
	prev := bundled.PluginsDir
	bundled.PluginsDir = ""
	t.Cleanup(func() { bundled.PluginsDir = prev })
	InvalidateSessionStartCache()
	t.Cleanup(InvalidateSessionStartCache)
	return home
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeClaudePlugin creates a Claude Code-format plugin (no ocode
// plugin.json) with one skill.
func writeClaudePlugin(t *testing.T, dir, name string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"),
		`{"name":"`+name+`","description":"cc `+name+`","version":"1.2.3"}`)
	writeFile(t, filepath.Join(dir, "skills", "brainstorming", "SKILL.md"),
		"---\nname: brainstorming\ndescription: from "+name+"\n---\nbody\n")
}

// installInClaudeCode registers dir in Claude Code's installed_plugins.json
// (the v2 map-of-arrays shape) under id.
func installInClaudeCode(t *testing.T, home string, ids map[string]string) {
	t.Helper()
	var b strings.Builder
	b.WriteString(`{"version":2,"plugins":{`)
	first := true
	for id, dir := range ids {
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString(`"` + id + `":[{"scope":"user","installPath":` + jsonString(dir) + `,"version":"1.2.3"}]`)
	}
	b.WriteString("}}")
	writeFile(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), b.String())
}

func jsonString(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

func TestReadPluginManifestClaudeOnly(t *testing.T) {
	dir := t.TempDir()
	writeClaudePlugin(t, dir, "sp")
	writeFile(t, filepath.Join(dir, "extra", "x", "SKILL.md"), "# x\n")
	// Extra skill root via the manifest's "skills" field, plus one that
	// escapes the plugin dir and must be dropped.
	writeFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"),
		`{"name":"sp","description":"d","skills":["./extra","../outside"]}`)

	p, ok := readPluginManifest(dir)
	if !ok {
		t.Fatal("Claude Code-only plugin not recognised")
	}
	if p.Name != "sp" || p.Format != FormatClaude || !p.HasClaudeManifest {
		t.Fatalf("got name=%q format=%q hasClaude=%v", p.Name, p.Format, p.HasClaudeManifest)
	}
	want := []string{filepath.Join(dir, "skills"), filepath.Join(dir, "extra")}
	if strings.Join(p.SkillDirs, "|") != strings.Join(want, "|") {
		t.Fatalf("SkillDirs = %v, want %v", p.SkillDirs, want)
	}
}

func TestReadPluginManifestOcodeWinsFieldsClaudeFillsBlanks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "plugin.json"), `{"name":"dual","instructions":"be brief"}`)
	writeFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"),
		`{"name":"other","description":"from cc","version":"2.0.0"}`)

	p, ok := readPluginManifest(dir)
	if !ok {
		t.Fatal("dual-manifest plugin not recognised")
	}
	if p.Name != "dual" || p.Format != FormatOcode || !p.HasClaudeManifest {
		t.Fatalf("got name=%q format=%q hasClaude=%v", p.Name, p.Format, p.HasClaudeManifest)
	}
	if p.Description != "from cc" || p.Version != "2.0.0" || p.Instructions != "be brief" {
		t.Fatalf("blank fields not filled from Claude manifest: %+v", p)
	}
}

func TestReadPluginManifestNeither(t *testing.T) {
	if _, ok := readPluginManifest(t.TempDir()); ok {
		t.Fatal("dir without a manifest must not be a plugin")
	}
}

// TestLoadPluginsOcodeShadowsClaudeCode pins the precedence the user asked
// for: an ocode plugin wins over a Claude Code install of the same name, and a
// Claude Code plugin with no ocode counterpart is loaded.
func TestLoadPluginsOcodeShadowsClaudeCode(t *testing.T) {
	home := isolatePluginEnv(t)

	ocodeDir := filepath.Join(home, ".config", "opencode", "plugins", "superpowers")
	writeFile(t, filepath.Join(ocodeDir, "plugin.json"), `{"name":"superpowers","description":"ocode fork"}`)

	ccStore := filepath.Join(home, ".claude", "plugins", "cache", "mkt")
	ccShadowed := filepath.Join(ccStore, "superpowers", "6.4.2")
	ccOnly := filepath.Join(ccStore, "extras", "1.0.0")
	writeClaudePlugin(t, ccShadowed, "superpowers")
	writeClaudePlugin(t, ccOnly, "extras")
	installInClaudeCode(t, home, map[string]string{
		"superpowers@mkt": ccShadowed,
		"extras@mkt":      ccOnly,
	})

	got := map[string]Plugin{}
	for _, p := range LoadPlugins(nil) {
		got[p.Name] = p
	}
	if p := got["superpowers"]; p.Dir != ocodeDir || p.Source != SourceOcode {
		t.Fatalf("superpowers should come from ocode, got dir=%q source=%q", p.Dir, p.Source)
	}
	if p := got["extras"]; p.Dir != ccOnly || p.Source != SourceClaudeCode {
		t.Fatalf("extras should come from Claude Code, got dir=%q source=%q", p.Dir, p.Source)
	}

	// ocode's own enable map can switch a Claude Code plugin off too.
	for _, p := range LoadPlugins(map[string]bool{"extras": false}) {
		if p.Name == "extras" {
			t.Fatal("extras disabled in ocode config but still loaded")
		}
	}
}

func TestClaudeCodePluginDirsHonoursSettingsAndScope(t *testing.T) {
	home := isolatePluginEnv(t)
	proj := t.TempDir()
	other := t.TempDir()
	store := filepath.Join(home, ".claude", "plugins")
	writeFile(t, filepath.Join(store, "installed_plugins.json"), `{"plugins":{
		"on@m":       [{"scope":"user","installPath":"cache/m/on/1"}],
		"off@m":      [{"scope":"user","installPath":"cache/m/off/1"}],
		"projoff@m":  [{"scope":"project","installPath":"cache/m/projoff/1","projectPath":`+jsonString(proj)+`}],
		"here@m":     [{"scope":"local","installPath":"cache/m/here/1","projectPath":`+jsonString(proj)+`}],
		"elsewhere@m":[{"scope":"project","installPath":"cache/m/elsewhere/1","projectPath":`+jsonString(other)+`}]
	}}`)
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), `{"enabledPlugins":{"off@m":false,"projoff@m":true}}`)
	// Project settings override user settings.
	writeFile(t, filepath.Join(proj, ".claude", "settings.json"), `{"enabledPlugins":{"projoff@m":false}}`)

	got := map[string]bool{}
	for _, cp := range claudeCodePlugins(proj) {
		rel, _ := filepath.Rel(filepath.Join(store, "cache", "m"), cp.dir)
		got[filepath.Dir(rel)] = cp.enabled
	}
	// Disabled-in-Claude-Code plugins are still discovered (ocode may turn
	// them on); only another project's install is excluded.
	want := map[string]bool{"on": true, "off": false, "projoff": false, "here": true}
	if len(got) != len(want) {
		t.Fatalf("claudeCodePlugins = %v, want %v", got, want)
	}
	for k, v := range want {
		if on, ok := got[k]; !ok || on != v {
			t.Fatalf("claudeCodePlugins = %v, want %v", got, want)
		}
	}
}

// TestOcodeConfigOverridesClaudeCodeState pins that ocode's own plugin config
// decides, with Claude Code's enabledPlugins only as the default: ocode can
// turn on a plugin Claude Code has disabled, turn off one it has enabled, and
// disabling an ocode plugin never lets a same-named Claude Code copy through.
func TestOcodeConfigOverridesClaudeCodeState(t *testing.T) {
	home := isolatePluginEnv(t)
	ccStore := filepath.Join(home, ".claude", "plugins", "cache", "m")
	onDir := filepath.Join(ccStore, "ccon", "1")
	offDir := filepath.Join(ccStore, "ccoff", "1")
	dupDir := filepath.Join(ccStore, "dup", "1")
	writeClaudePlugin(t, onDir, "ccon")
	writeClaudePlugin(t, offDir, "ccoff")
	writeClaudePlugin(t, dupDir, "dup")
	installInClaudeCode(t, home, map[string]string{"ccon@m": onDir, "ccoff@m": offDir, "dup@m": dupDir})
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), `{"enabledPlugins":{"ccoff@m":false}}`)
	writeFile(t, filepath.Join(home, ".config", "opencode", "plugins", "dup", "plugin.json"), `{"name":"dup"}`)

	names := func(enabled map[string]bool) string {
		var n []string
		for _, p := range LoadPlugins(enabled) {
			n = append(n, p.Name)
		}
		return strings.Join(n, ",")
	}
	if got := names(nil); got != "dup,ccon" {
		t.Fatalf("defaults: got %q, want Claude Code's own state (dup,ccon)", got)
	}
	if got := names(map[string]bool{"ccoff": true, "ccon": false, "dup": false}); got != "ccoff" {
		t.Fatalf("ocode overrides: got %q, want ccoff", got)
	}
	settings, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if string(settings) != `{"enabledPlugins":{"ccoff@m":false}}` {
		t.Fatalf("Claude Code settings modified: %s", settings)
	}

	all := map[string]Plugin{}
	for _, p := range LoadAllPluginsForProject("") {
		all[p.Name] = p
	}
	if p := all["ccoff"]; p.Dir != offDir || p.DefaultEnabled {
		t.Fatalf("ccoff should be listed with DefaultEnabled=false: %+v", p)
	}
}

func TestParseClaudeInstalledShapes(t *testing.T) {
	for name, doc := range map[string]string{
		"map of arrays":  `{"plugins":{"a@m":[{"installPath":"/x"}]}}`,
		"map of objects": `{"plugins":{"a@m":{"installPath":"/x"}}}`,
		"array":          `{"plugins":[{"id":"a@m","installPath":"/x"}]}`,
	} {
		got := parseClaudeInstalled([]byte(doc))
		if len(got) != 1 || got[0].ID != "a@m" || got[0].InstallPath != "/x" {
			t.Errorf("%s: got %+v", name, got)
		}
	}
}

func TestSessionStartContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook fixtures use bash")
	}
	home := isolatePluginEnv(t)
	proj := t.TempDir()

	jsonDir := filepath.Join(home, ".claude", "plugins", "cache", "m", "jsonhook", "1")
	writeClaudePlugin(t, jsonDir, "jsonhook")
	writeFile(t, filepath.Join(jsonDir, "hooks", "hooks.json"), `{"hooks":{"SessionStart":[
		{"matcher":"startup|clear|compact","hooks":[{"type":"command","command":"\"${CLAUDE_PLUGIN_ROOT}/hooks/start.sh\""}]},
		{"matcher":"resume","hooks":[{"type":"command","command":"echo RESUME_ONLY"}]}
	]}}`)
	writeFile(t, filepath.Join(jsonDir, "hooks", "start.sh"),
		"#!/bin/bash\nprintf '{\"hookSpecificOutput\":{\"hookEventName\":\"SessionStart\",\"additionalContext\":\"root=%s cwd=%s\"}}' \"$CLAUDE_PLUGIN_ROOT\" \"$(pwd -P)\"\n")
	if err := os.Chmod(filepath.Join(jsonDir, "hooks", "start.sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	textDir := filepath.Join(home, ".config", "opencode", "plugins", "texthook")
	writeClaudePlugin(t, textDir, "texthook")
	writeFile(t, filepath.Join(textDir, "hooks", "hooks.json"),
		`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo plain context"}]}]}}`)

	// An ocode-only plugin's hooks/ dir is not a Claude Code convention and
	// must never be executed.
	ocodeDir := filepath.Join(home, ".config", "opencode", "plugins", "ocodeonly")
	writeFile(t, filepath.Join(ocodeDir, "plugin.json"), `{"name":"ocodeonly"}`)
	writeFile(t, filepath.Join(ocodeDir, "hooks", "hooks.json"),
		`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo MUST_NOT_RUN"}]}]}}`)

	installInClaudeCode(t, home, map[string]string{"jsonhook@m": jsonDir})

	realProj, err := filepath.EvalSymlinks(proj)
	if err != nil {
		t.Fatal(err)
	}
	got := SessionStartContext(nil, proj)
	for _, want := range []string{
		"--- Plugin: jsonhook (SessionStart) ---\nroot=" + jsonDir + " cwd=" + realProj,
		"--- Plugin: texthook (SessionStart) ---\nplain context",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, bad := range []string{"RESUME_ONLY", "MUST_NOT_RUN"} {
		if strings.Contains(got, bad) {
			t.Errorf("unexpected %q in:\n%s", bad, got)
		}
	}

	// Cached: a changed hook is not re-run until the cache is invalidated.
	writeFile(t, filepath.Join(textDir, "hooks", "hooks.json"),
		`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo changed"}]}]}}`)
	if again := SessionStartContext(nil, proj); again != got {
		t.Fatalf("second call re-ran hooks:\n%s", again)
	}
	InvalidateSessionStartCache()
	if again := SessionStartContext(nil, proj); !strings.Contains(again, "changed") {
		t.Fatalf("invalidated cache did not re-run hooks:\n%s", again)
	}
}

func TestParseHookOutput(t *testing.T) {
	cases := map[string]string{
		"":                "",
		"  plain text \n": "plain text",
		`{"hookSpecificOutput":{"additionalContext":"a"}}`: "a",
		`{"additionalContext":"b"}`:                        "b",
		`{"additional_context":"c"}`:                       "c",
		`{"hookSpecificOutput":{"sessionTitle":"t"}}`:      "",
		`{not json`: "{not json",
	}
	for in, want := range cases {
		if got := parseHookOutput(in); got != want {
			t.Errorf("parseHookOutput(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRemoveRefusesClaudeCodeInstall(t *testing.T) {
	home := isolatePluginEnv(t)
	dir := filepath.Join(home, ".claude", "plugins", "cache", "m", "p", "1")
	writeClaudePlugin(t, dir, "p")
	if err := Remove(dir); err == nil {
		t.Fatal("Remove deleted a Claude Code-installed plugin")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("plugin dir gone: %v", err)
	}
}

// TestInstallLocalKeepsExecutableBit pins that a local install preserves file
// modes: a Claude Code plugin's SessionStart hook (and an ocode on_install
// script) is an executable the copy must not strip.
func TestInstallLocalKeepsExecutableBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no executable bit on Windows")
	}
	src := t.TempDir()
	writeClaudePlugin(t, src, "exe")
	script := filepath.Join(src, "hooks", "run.sh")
	writeFile(t, script, "#!/bin/sh\necho hi\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "exe")
	if _, err := InstallLocal(src, dest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dest, "hooks", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("executable bit lost: %v", info.Mode())
	}
}
