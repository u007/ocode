package plugins

import (
	"path/filepath"
	"testing"
)

// writeClaudePluginAgent adds one agent file to a Claude Code plugin dir.
func writeClaudePluginAgent(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "agents", "context.md"),
		"---\nname: context\ndescription: read-only lookup\n---\nbody\n")
}

func TestLoadClaudeCodePluginAgentsDirs_EnabledInstall(t *testing.T) {
	home := isolatePluginEnv(t)
	dir := filepath.Join(home, ".claude", "plugins", "cache", "okf", "1.2.3")
	writeClaudePlugin(t, dir, "okf")
	writeClaudePluginAgent(t, dir)
	installInClaudeCode(t, home, map[string]string{"okf@market": dir})

	got := LoadClaudeCodePluginAgentsDirs(nil)
	if len(got) != 1 {
		t.Fatalf("got %d agents dirs, want 1: %+v", len(got), got)
	}
	if got[0].Plugin != "okf" || got[0].Dir != filepath.Join(dir, "agents") {
		t.Fatalf("got %+v, want plugin okf with dir %s", got[0], filepath.Join(dir, "agents"))
	}
}

func TestLoadClaudeCodePluginAgentsDirs_SkipsDisabled(t *testing.T) {
	home := isolatePluginEnv(t)
	dir := filepath.Join(home, ".claude", "plugins", "cache", "okf", "1.2.3")
	writeClaudePlugin(t, dir, "okf")
	writeClaudePluginAgent(t, dir)
	installInClaudeCode(t, home, map[string]string{"okf@market": dir})

	// ocode's own config turns the plugin off by name.
	if got := LoadClaudeCodePluginAgentsDirs(map[string]bool{"okf": false}); len(got) != 0 {
		t.Fatalf("disabled plugin returned agents dirs: %+v", got)
	}
}

func TestLoadClaudeCodePluginAgentsDirs_SkipsClaudeDisabled(t *testing.T) {
	home := isolatePluginEnv(t)
	dir := filepath.Join(home, ".claude", "plugins", "cache", "okf", "1.2.3")
	writeClaudePlugin(t, dir, "okf")
	writeClaudePluginAgent(t, dir)
	installInClaudeCode(t, home, map[string]string{"okf@market": dir})
	// Claude Code's own enabledPlugins turns it off; ocode uses that as default.
	writeFile(t, filepath.Join(home, ".claude", "settings.json"),
		`{"enabledPlugins":{"okf@market":false}}`)

	if got := LoadClaudeCodePluginAgentsDirs(nil); len(got) != 0 {
		t.Fatalf("plugin disabled in Claude Code returned agents dirs: %+v", got)
	}
}

func TestLoadClaudeCodePluginAgentsDirs_SkipsWithoutAgentsDir(t *testing.T) {
	home := isolatePluginEnv(t)
	dir := filepath.Join(home, ".claude", "plugins", "cache", "okf", "1.2.3")
	writeClaudePlugin(t, dir, "okf") // skills only, no agents/
	installInClaudeCode(t, home, map[string]string{"okf@market": dir})

	if got := LoadClaudeCodePluginAgentsDirs(nil); len(got) != 0 {
		t.Fatalf("plugin without agents/ returned agents dirs: %+v", got)
	}
}

func TestLoadClaudeCodePluginAgentsDirs_IgnoresOcodePlugins(t *testing.T) {
	home := isolatePluginEnv(t)
	// An ocode-owned plugin under the global plugin dir is covered by
	// LoadGlobalPluginAgentsDirPaths and must not be returned here too.
	ocodeDir := filepath.Join(home, ".config", "opencode", "plugins", "ocodeonly")
	writeFile(t, filepath.Join(ocodeDir, "plugin.json"), `{"name":"ocodeonly"}`)
	writeClaudePluginAgent(t, ocodeDir)

	// Prove the fixture is real: ocode's own loader must see it, or the
	// negative assertion below would pass for the wrong reason.
	if got := LoadGlobalPluginAgentsDirPaths(nil); len(got) != 1 {
		t.Fatalf("fixture not found by ocode's global loader: %v", got)
	}
	if got := LoadClaudeCodePluginAgentsDirs(nil); len(got) != 0 {
		t.Fatalf("ocode plugin returned by Claude loader: %+v", got)
	}
}
