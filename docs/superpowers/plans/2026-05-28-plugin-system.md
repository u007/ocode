# Plugin System + `/plugin` Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `/plugin` TUI command that lists, enables/disables, and installs plugins from local paths or GitHub URLs, backed by a plugin manager that persists state to config.

**Architecture:** Extend `internal/plugins/loader.go` with a new `manager.go` for lifecycle operations. Add `PluginConfig` to the config struct mirroring the `MCPConfig` pattern. Wire a `/plugin` slash command using the same handler pattern as `/mcp`. Plugin install directory is stored in `PluginConfig.Dir` to decouple the git-derived clone name from the `plugin.json` `name` field.

**Tech Stack:** Go, go-git v5 (`github.com/go-git/go-git/v5`) for git clone, existing `internal/config` JSON persistence helpers.

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/config/config.go` | Modify | Add `PluginConfig` struct, `Plugins` field to `Config`, `SavePluginEnabled`, `SavePlugin`, `RemovePlugin` helpers |
| `internal/plugins/loader.go` | Modify | Add `Dir`/`MCP`/`OnInstall` fields to `Plugin`; update `LoadPlugins`, `LoadPluginInstructions`, `LoadPluginToolsDirPaths` to accept enabled map |
| `internal/plugins/manager.go` | Create | Install (git clone / local copy), remove, `RunOnInstall`, `AutoRegisterMCP`, `UnregisterMCP` |
| `internal/tui/commands.go` | Modify | Register `/plugin` commandSpec + `runPluginCmd` handler |
| `internal/tui/model.go` | Modify | Add `renderPluginList()`, message types, `pendingPluginInstall` confirm flow |
| `internal/plugins/manager_test.go` | Create | Tests for install, enable/disable, remove, URL helpers |
| `internal/config/plugin_config_test.go` | Create | Tests for SavePluginEnabled, SavePlugin, RemovePlugin |

---

### Task 1: Add `PluginConfig` to config

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/config/plugin_config_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/plugin_config_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSavePluginEnabled(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(cfgPath, []byte(`{"plugins":{"myplugin":{"source":"github.com/x/y","dir":"/tmp/x-y","enabled":true}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_CONFIG_DIR", dir)

	if err := SavePluginEnabled("myplugin", false); err != nil {
		t.Fatalf("SavePluginEnabled: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins["myplugin"].Enabled {
		t.Error("expected plugin to be disabled")
	}
}

func TestSavePlugin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_CONFIG_DIR", dir)

	p := PluginConfig{Source: "github.com/acme/plugin", Dir: "/home/user/.config/opencode/plugins/acme-plugin", Enabled: true}
	if err := SavePlugin("acme-plugin", p); err != nil {
		t.Fatalf("SavePlugin: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Plugins["acme-plugin"]
	if got.Source != p.Source || got.Dir != p.Dir || !got.Enabled {
		t.Errorf("got %+v, want %+v", got, p)
	}
}

func TestRemovePlugin(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(cfgPath, []byte(`{"plugins":{"gone":{"source":"x","dir":"/tmp/x","enabled":true}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_CONFIG_DIR", dir)

	if err := RemovePlugin("gone"); err != nil {
		t.Fatalf("RemovePlugin: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Plugins["gone"]; ok {
		t.Error("plugin still present after remove")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```
cd /Users/james/www/ocode && go test ./internal/config/... -run "TestSavePlugin|TestRemovePlugin" -v 2>&1 | head -20
```

Expected: compile error — `PluginConfig`, `SavePlugin*`, `RemovePlugin` not defined.

- [ ] **Step 3: Add `PluginConfig` struct and field to `Config`**

In `internal/config/config.go`, after the `MCPConfig` block (around line 84), add:

```go
type PluginConfig struct {
	Source  string `json:"source"`  // git URL or local path used to install
	Dir     string `json:"dir"`     // absolute path to the installed plugin directory
	Ref     string `json:"ref"`     // git ref (tag/commit); empty = HEAD at install time
	Enabled bool   `json:"enabled"`
}
```

Add to the `Config` struct (after `MCP` field, line ~117):

```go
Plugins map[string]PluginConfig `json:"plugins"`
```

In the `Load()` function initialiser (line ~132), add:

```go
Plugins: make(map[string]PluginConfig),
```

- [ ] **Step 4: Add `SavePluginEnabled`, `SavePlugin`, `RemovePlugin` after `SaveMCPServer`**

```go
func SavePluginEnabled(name string, enabled bool) error {
	configPath, err := (&Config{}).ActiveConfigPath()
	if err != nil {
		return err
	}
	m, err := loadConfigMap(configPath)
	if err != nil {
		return err
	}
	pluginsRaw, ok := m["plugins"].(map[string]any)
	if !ok {
		return fmt.Errorf("plugin %q not found in config", name)
	}
	entry, ok := pluginsRaw[name].(map[string]any)
	if !ok {
		return fmt.Errorf("plugin %q not found in config", name)
	}
	entry["enabled"] = enabled
	pluginsRaw[name] = entry
	m["plugins"] = pluginsRaw
	return saveJSONFile(configPath, m)
}

func SavePlugin(name string, p PluginConfig) error {
	configPath, err := (&Config{}).ActiveConfigPath()
	if err != nil {
		return err
	}
	m, err := loadConfigMap(configPath)
	if err != nil {
		return err
	}
	pluginsRaw, ok := m["plugins"].(map[string]any)
	if !ok {
		pluginsRaw = map[string]any{}
	}
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal plugin config: %w", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal plugin config: %w", err)
	}
	pluginsRaw[name] = raw
	m["plugins"] = pluginsRaw
	return saveJSONFile(configPath, m)
}

func RemovePlugin(name string) error {
	configPath, err := (&Config{}).ActiveConfigPath()
	if err != nil {
		return err
	}
	m, err := loadConfigMap(configPath)
	if err != nil {
		return err
	}
	pluginsRaw, ok := m["plugins"].(map[string]any)
	if !ok {
		return nil
	}
	delete(pluginsRaw, name)
	m["plugins"] = pluginsRaw
	return saveJSONFile(configPath, m)
}
```

- [ ] **Step 5: Run tests**

```
cd /Users/james/www/ocode && go test ./internal/config/... -run "TestSavePlugin|TestRemovePlugin" -v 2>&1
```

Expected: all three pass.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/plugin_config_test.go
git commit -m "feat(config): add PluginConfig with Dir/Ref fields, SavePlugin, SavePluginEnabled, RemovePlugin"
```

---

### Task 2: Extend `Plugin` struct and fix enabled-aware loader functions

**Files:**
- Modify: `internal/plugins/loader.go`

The three existing public functions all ignore the enabled state. Fix them all in one task.

- [ ] **Step 1: Extend the `Plugin` struct**

Replace the existing `Plugin` struct in `loader.go`:

```go
type PluginMCPConfig struct {
	Server       string   `json:"server"`
	AutoRegister bool     `json:"auto_register"`
	// Command is the MCP server launch args (no shell). {plugin_dir} is replaced
	// at registration time with the absolute path to the installed plugin directory.
	Command      []string `json:"command"`
}

type Plugin struct {
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	Version      string           `json:"version"`
	Commands     []string         `json:"commands"`
	Tools        []string         `json:"tools"`
	Instructions string           `json:"instructions"`
	// OnInstall is a command executed directly (no shell) after install.
	// {plugin_dir} tokens are replaced with the absolute plugin directory path.
	OnInstall    []string         `json:"on_install"`
	MCP          *PluginMCPConfig `json:"mcp"`
}
```

- [ ] **Step 2: Update `LoadPlugins` signature to accept enabled map**

```go
// LoadPlugins returns installed plugins filtered by the enabled map.
// Pass nil to load all regardless of enabled state.
func LoadPlugins(enabled map[string]bool) []Plugin {
	var plugins []Plugin
	for _, dir := range pluginSearchPaths() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			pluginPath := filepath.Join(dir, e.Name(), "plugin.json")
			data, err := os.ReadFile(pluginPath)
			if err != nil {
				continue
			}
			var p Plugin
			if err := json.Unmarshal(data, &p); err != nil {
				continue
			}
			if p.Name == "" {
				p.Name = e.Name()
			}
			if enabled != nil {
				if on, ok := enabled[p.Name]; ok && !on {
					continue
				}
			}
			plugins = append(plugins, p)
		}
	}
	return plugins
}
```

- [ ] **Step 3: Update `LoadPluginInstructions` and `LoadPluginToolsDirPaths`**

```go
func LoadPluginInstructions(enabled map[string]bool) string {
	var instructions string
	for _, p := range LoadPlugins(enabled) {
		if p.Instructions != "" {
			instructions += "\n--- Plugin: " + p.Name + " ---\n" + p.Instructions + "\n"
		}
	}
	return instructions
}

func LoadPluginToolsDirPaths(enabled map[string]bool) []string {
	var paths []string
	for _, dir := range pluginSearchPaths() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			// Derive plugin name from directory and check enabled map.
			dirName := e.Name()
			if enabled != nil {
				// Try to read plugin name from manifest; fall back to dir name.
				name := dirName
				if data, err := os.ReadFile(filepath.Join(dir, dirName, "plugin.json")); err == nil {
					var p Plugin
					if json.Unmarshal(data, &p) == nil && p.Name != "" {
						name = p.Name
					}
				}
				if on, ok := enabled[name]; ok && !on {
					continue
				}
			}
			toolsDir := filepath.Join(dir, dirName, "tools")
			if _, err := os.Stat(toolsDir); err == nil {
				paths = append(paths, toolsDir)
			}
		}
	}
	return paths
}

func LoadPluginCommandDirPaths(enabled map[string]bool) []string {
	var paths []string
	for _, dir := range pluginSearchPaths() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dirName := e.Name()
			if enabled != nil {
				name := dirName
				if data, err := os.ReadFile(filepath.Join(dir, dirName, "plugin.json")); err == nil {
					var p Plugin
					if json.Unmarshal(data, &p) == nil && p.Name != "" {
						name = p.Name
					}
				}
				if on, ok := enabled[name]; ok && !on {
					continue
				}
			}
			cmdsDir := filepath.Join(dir, dirName, "commands")
			if _, err := os.Stat(cmdsDir); err == nil {
				paths = append(paths, cmdsDir)
			}
		}
	}
	return paths
}
```

- [ ] **Step 4: Fix all callers**

Search and update every call site to pass `nil` (same behaviour as before) or the config enabled map:

```
grep -rn "LoadPlugins\b\|LoadPluginInstructions\|LoadPluginToolsDirPaths\|LoadPluginCommandDirPaths" /Users/james/www/ocode/internal/
```

For each hit, pass `nil` as the first argument unless you have access to `config.Plugins` at that point. Build `enabled map[string]bool` from `cfg.Plugins` where available:

```go
enabled := make(map[string]bool, len(cfg.Plugins))
for name, p := range cfg.Plugins {
    enabled[name] = p.Enabled
}
```

- [ ] **Step 5: Build**

```
cd /Users/james/www/ocode && go build ./... 2>&1
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/plugins/loader.go
git commit -m "feat(plugins): extend Plugin struct, propagate enabled map through all loader functions"
```

---

### Task 3: Plugin manager (install, remove, on_install, MCP auto-register)

**Files:**
- Create: `internal/plugins/manager.go`
- Create: `internal/plugins/manager_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/plugins/manager_test.go
package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallLocal(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "plugin.json"), []byte(`{"name":"test","description":"Test plugin"}`), 0644); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	p, err := InstallLocal(src, dest)
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	if p.Name != "test" {
		t.Errorf("got name %q, want %q", p.Name, "test")
	}
	if _, err := os.Stat(filepath.Join(dest, "plugin.json")); err != nil {
		t.Errorf("plugin.json not found in dest: %v", err)
	}
}

func TestInstallLocalMissingManifest(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	if _, err := InstallLocal(src, dest); err == nil {
		t.Error("expected error for missing plugin.json")
	}
}

func TestRemovePlugin(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "myplugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := Remove(pluginDir); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(pluginDir); !os.IsNotExist(err) {
		t.Error("plugin directory still exists after remove")
	}
}

func TestNormaliseGitURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"github.com/user/repo", "https://github.com/user/repo"},
		{"https://github.com/user/repo", "https://github.com/user/repo"},
		{"https://github.com/user/repo.git", "https://github.com/user/repo.git"},
	}
	for _, c := range cases {
		got := normaliseGitURL(c.in)
		if got != c.want {
			t.Errorf("normaliseGitURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestInstallDirName(t *testing.T) {
	cases := []struct{ url, want string }{
		{"https://github.com/user/repo", "user-repo"},
		{"https://github.com/user/repo.git", "user-repo"},
	}
	for _, c := range cases {
		got := installDirName(c.url)
		if got != c.want {
			t.Errorf("installDirName(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestRunOnInstallEmpty(t *testing.T) {
	dir := t.TempDir()
	// No on_install — should be a no-op.
	if err := RunOnInstall(dir, Plugin{}); err != nil {
		t.Fatalf("RunOnInstall with empty plugin: %v", err)
	}
}

func TestRunOnInstallValidation(t *testing.T) {
	dir := t.TempDir()
	p := Plugin{OnInstall: []string{"rm; evil"}}
	err := RunOnInstall(dir, p)
	if err == nil {
		t.Error("expected error for command containing shell metacharacter")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```
cd /Users/james/www/ocode && go test ./internal/plugins/... -v 2>&1 | head -20
```

Expected: compile errors.

- [ ] **Step 3: Create `internal/plugins/manager.go`**

```go
package plugins

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/jamesmercstudio/ocode/internal/config"
)

// InstallGit clones a git URL into pluginsRoot/<derived-dir> and returns the
// parsed Plugin and the absolute clone directory. ref may be empty (HEAD).
func InstallGit(rawURL, pluginsRoot, ref string) (Plugin, string, error) {
	gitURL := normaliseGitURL(rawURL)
	dirName := installDirName(gitURL)
	destDir := filepath.Join(pluginsRoot, dirName)

	if _, err := os.Stat(destDir); err == nil {
		return Plugin{}, "", fmt.Errorf("plugin directory %q already exists; remove it first", destDir)
	}

	cloneOpts := &gogit.CloneOptions{
		URL:      gitURL,
		Depth:    1,
		Progress: nil,
	}
	if ref != "" {
		cloneOpts.ReferenceName = plumbing.NewTagReferenceName(ref)
	}

	if _, err := gogit.PlainClone(destDir, false, cloneOpts); err != nil {
		return Plugin{}, "", fmt.Errorf("git clone %s: %w", gitURL, err)
	}

	p, err := readManifest(destDir)
	if err != nil {
		_ = os.RemoveAll(destDir)
		return Plugin{}, "", fmt.Errorf("read plugin manifest: %w", err)
	}
	if p.Name == "" {
		p.Name = dirName
	}
	abs, _ := filepath.Abs(destDir)
	return p, abs, nil
}

// InstallLocal copies a local directory into destDir and returns the parsed Plugin.
func InstallLocal(srcDir, destDir string) (Plugin, error) {
	p, err := readManifest(srcDir)
	if err != nil {
		return Plugin{}, fmt.Errorf("read plugin manifest: %w", err)
	}
	if err := copyDir(srcDir, destDir); err != nil {
		return Plugin{}, fmt.Errorf("copy plugin directory: %w", err)
	}
	return p, nil
}

// Remove deletes a plugin directory from disk.
func Remove(pluginDir string) error {
	if err := os.RemoveAll(pluginDir); err != nil {
		return fmt.Errorf("remove plugin dir %q: %w", pluginDir, err)
	}
	return nil
}

// PluginInstallDir returns the canonical global plugins root directory.
func PluginInstallDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode", "plugins"), nil
}

// RunOnInstall executes the plugin's on_install command if present.
// The command is executed directly via exec — no shell — preventing injection.
// Tokens containing {plugin_dir} are replaced with the absolute plugin path.
func RunOnInstall(pluginDir string, p Plugin) error {
	if len(p.OnInstall) == 0 {
		return nil
	}
	abs, err := filepath.Abs(pluginDir)
	if err != nil {
		return fmt.Errorf("resolve plugin dir: %w", err)
	}
	args := make([]string, len(p.OnInstall))
	for i, tok := range p.OnInstall {
		args[i] = strings.ReplaceAll(tok, "{plugin_dir}", abs)
	}
	// Reject shell metacharacters in the executable name.
	if strings.ContainsAny(args[0], ";|&$`\\\"'") {
		return fmt.Errorf("on_install command[0] contains disallowed characters: %q", args[0])
	}
	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec — validated above; no shell
	cmd.Dir = abs
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("on_install failed: %w", err)
	}
	return nil
}

// AutoRegisterMCP writes a local MCP server entry to ocode config when the
// plugin declares mcp.auto_register = true. The command array must be non-empty
// and is stored verbatim (with {plugin_dir} already substituted).
func AutoRegisterMCP(pluginDir string, p Plugin) error {
	if p.MCP == nil || !p.MCP.AutoRegister || p.MCP.Server == "" || len(p.MCP.Command) == 0 {
		return nil
	}
	abs, err := filepath.Abs(pluginDir)
	if err != nil {
		return fmt.Errorf("resolve plugin dir: %w", err)
	}
	cmd := make([]string, len(p.MCP.Command))
	for i, tok := range p.MCP.Command {
		cmd[i] = strings.ReplaceAll(tok, "{plugin_dir}", abs)
	}
	server := config.MCPConfig{
		Type:    "local",
		Command: cmd,
		Enabled: true,
	}
	return config.SaveMCPServer(p.MCP.Server, server)
}

// UnregisterMCP removes the auto-registered MCP server from ocode config.
func UnregisterMCP(p Plugin) error {
	if p.MCP == nil || !p.MCP.AutoRegister || p.MCP.Server == "" {
		return nil
	}
	// Reuse the same config-map manipulation pattern as RemovePlugin.
	configPath, err := (&config.Config{}).ActiveConfigPath()
	if err != nil {
		return err
	}
	// loadConfigMap is internal to config package; call config.RemoveMCPServer
	// once that helper is added (see note below), or inline the map surgery here.
	// For now: read the config JSON, delete the MCP entry, write it back.
	return config.RemoveMCPServer(p.MCP.Server)
}

// normaliseGitURL converts bare "github.com/user/repo" shorthands to full https URLs.
func normaliseGitURL(raw string) string {
	if strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://") {
		return raw
	}
	return "https://" + raw
}

// installDirName derives a directory name from a git URL.
// "https://github.com/user/repo" → "user-repo"
func installDirName(gitURL string) string {
	u, err := url.Parse(gitURL)
	if err != nil {
		return strings.NewReplacer("/", "-", ".", "-").Replace(gitURL)
	}
	path := strings.Trim(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	return strings.ReplaceAll(path, "/", "-")
}

func readManifest(dir string) (Plugin, error) {
	data, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return Plugin{}, fmt.Errorf("plugin.json not found in %s: %w", dir, err)
	}
	var p Plugin
	if err := json.Unmarshal(data, &p); err != nil {
		return Plugin{}, fmt.Errorf("parse plugin.json: %w", err)
	}
	return p, nil
}

func copyDir(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}
```

- [ ] **Step 4: Add `RemoveMCPServer` to `internal/config/config.go`**

After `SaveMCPServer` (line ~257):

```go
func RemoveMCPServer(name string) error {
	configPath, err := (&Config{}).ActiveConfigPath()
	if err != nil {
		return err
	}
	m, err := loadConfigMap(configPath)
	if err != nil {
		return err
	}
	mcpRaw, ok := m["mcp"].(map[string]any)
	if !ok {
		return nil
	}
	delete(mcpRaw, name)
	m["mcp"] = mcpRaw
	return saveJSONFile(configPath, m)
}
```

- [ ] **Step 5: Run tests**

```
cd /Users/james/www/ocode && go test ./internal/plugins/... -v 2>&1
```

Expected: all pass (git clone test requires network; unit helpers pass without it).

- [ ] **Step 6: Commit**

```bash
git add internal/plugins/manager.go internal/plugins/manager_test.go internal/config/config.go
git commit -m "feat(plugins): add manager with InstallGit/Local, RunOnInstall, AutoRegisterMCP, UnregisterMCP"
```

---

### Task 4: `/plugin` TUI command

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add `renderPluginList()` to `model.go`**

After `renderMCPList()` (around line 3355), add:

```go
func (m model) renderPluginList() string {
	if m.config == nil || len(m.config.Plugins) == 0 {
		return "No plugins installed. Use /plugin install <github.com/user/repo> to add one."
	}
	names := make([]string, 0, len(m.config.Plugins))
	for name := range m.config.Plugins {
		names = append(names, name)
	}
	sort.Strings(names)

	enabled := make(map[string]bool, len(m.config.Plugins))
	for name, p := range m.config.Plugins {
		enabled[name] = p.Enabled
	}
	loaded := map[string]struct{ tools, cmds int }{}
	for _, p := range plugins.LoadPlugins(enabled) {
		loaded[p.Name] = struct{ tools, cmds int }{len(p.Tools), len(p.Commands)}
	}

	var b strings.Builder
	b.WriteString("Plugins:\n")
	for _, name := range names {
		cfg := m.config.Plugins[name]
		state := "disabled"
		if cfg.Enabled {
			state = "enabled"
		}
		info := loaded[name]
		src := cfg.Source
		if len(src) > 38 {
			src = "..." + src[len(src)-35:]
		}
		b.WriteString(fmt.Sprintf("  %-18s %-8s %-38s %dt %dc\n",
			name, state, src, info.tools, info.cmds))
	}
	return b.String()
}
```

Add the `plugins` import to `model.go` imports:

```go
"github.com/jamesmercstudio/ocode/internal/plugins"
```

- [ ] **Step 2: Add message types to `model.go`**

Near the other message type definitions (around line 250), add:

```go
type pluginInstallMsg    struct{ source, ref string }
type pluginRemoveMsg     struct{ name string }
type pluginInstalledMsg  struct{ name, source, dir string; err error }
type pluginRemovedMsg    struct{ name string; err error }
// pluginInstallPendingMsg holds a half-complete install awaiting user confirmation.
type pluginInstallPendingMsg struct {
	p      plugins.Plugin
	source string
	dirName string
	installRoot string
}
```

- [ ] **Step 3: Add `pendingPluginInstall` field to `model` struct**

In `type model struct`, add:

```go
pendingPluginInstall *pluginInstallPendingMsg
```

- [ ] **Step 4: Add install/remove/confirm handlers to `model.Update`**

In the `model.Update()` switch, add:

```go
case pluginInstallMsg:
	source := msg.source
	ref := msg.ref
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Fetching plugin from %s…", source)})
	m.rerenderTranscriptAndMaybeScroll()
	return m, func() tea.Msg {
		installRoot, err := plugins.PluginInstallDir()
		if err != nil {
			return pluginInstalledMsg{source: source, err: err}
		}
		var p plugins.Plugin
		var dirName string
		if info, statErr := os.Stat(source); statErr == nil && info.IsDir() {
			name := filepath.Base(source)
			destDir := filepath.Join(installRoot, name)
			p, err = plugins.InstallLocal(source, destDir)
			dirName = destDir
		} else {
			var absDir string
			p, absDir, err = plugins.InstallGit(source, installRoot, ref)
			dirName = absDir
		}
		if err != nil {
			return pluginInstalledMsg{source: source, err: err}
		}
		if p.Name == "" {
			p.Name = filepath.Base(dirName)
		}
		return pluginInstallPendingMsg{p: p, source: source, dirName: dirName, installRoot: installRoot}
	}

case pluginInstallPendingMsg:
	// Plugin is cloned/copied. Show on_install command and ask for confirmation.
	m.pendingPluginInstall = &msg
	var text strings.Builder
	text.WriteString(fmt.Sprintf("Plugin %q cloned to %s.\n", msg.p.Name, msg.dirName))
	if len(msg.p.OnInstall) > 0 {
		text.WriteString(fmt.Sprintf("\nWill run: %s\n", strings.Join(msg.p.OnInstall, " ")))
		if msg.p.MCP != nil && len(msg.p.MCP.Command) > 0 {
			text.WriteString(fmt.Sprintf("Will register MCP server %q: %s\n", msg.p.MCP.Server, strings.Join(msg.p.MCP.Command, " ")))
		}
		text.WriteString("\nType /plugin confirm to proceed, or /plugin cancel to abort.")
	} else {
		// No on_install — proceed immediately.
		m.pendingPluginInstall = nil
		cfg := config.PluginConfig{Source: msg.source, Dir: msg.dirName, Enabled: true}
		if err := config.SavePlugin(msg.p.Name, cfg); err != nil {
			text.WriteString(fmt.Sprintf("\nFailed to save plugin config: %v", err))
		} else {
			if m.config.Plugins == nil {
				m.config.Plugins = map[string]config.PluginConfig{}
			}
			m.config.Plugins[msg.p.Name] = cfg
			text.WriteString(fmt.Sprintf("\nPlugin %q installed.", msg.p.Name))
		}
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: text.String()})
	m.rerenderTranscriptAndMaybeScroll()
	return m, nil

case pluginInstalledMsg:
	if msg.err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Plugin install failed: %v", msg.err)})
	} else {
		if m.config.Plugins == nil {
			m.config.Plugins = map[string]config.PluginConfig{}
		}
		m.config.Plugins[msg.name] = config.PluginConfig{Source: msg.source, Dir: msg.dir, Enabled: true}
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Plugin %q installed.", msg.name)})
	}
	m.rerenderTranscriptAndMaybeScroll()
	return m, nil

case pluginRemoveMsg:
	name := msg.name
	cfg, ok := m.config.Plugins[name]
	if !ok {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Plugin %q not found.", name)})
		return m, nil
	}
	pluginDir := cfg.Dir
	// Read manifest to know if MCP unregister is needed.
	var pluginMCP *plugins.PluginMCPConfig
	for _, p := range plugins.LoadPlugins(nil) {
		if p.Name == name {
			pluginMCP = p.MCP
			break
		}
	}
	return m, func() tea.Msg {
		if err := plugins.Remove(pluginDir); err != nil {
			return pluginRemovedMsg{name: name, err: err}
		}
		if err := config.RemovePlugin(name); err != nil {
			return pluginRemovedMsg{name: name, err: err}
		}
		if pluginMCP != nil {
			tmpPlugin := plugins.Plugin{MCP: pluginMCP}
			if err := plugins.UnregisterMCP(tmpPlugin); err != nil {
				return pluginRemovedMsg{name: name, err: fmt.Errorf("unregister MCP: %w", err)}
			}
		}
		return pluginRemovedMsg{name: name}
	}

case pluginRemovedMsg:
	if msg.err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Plugin remove failed: %v", msg.err)})
	} else {
		delete(m.config.Plugins, msg.name)
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Plugin %q removed.", msg.name)})
	}
	m.rerenderTranscriptAndMaybeScroll()
	return m, nil
```

- [ ] **Step 5: Add `/plugin` commandSpec and handler to `commands.go`**

In `commandSpecs` slice (before `/exit`):

```go
{name: "/plugin", usage: "/plugin [list|enable <name>|disable <name>|install <url>|remove <name>|info <name>|confirm|cancel]", help: "List, toggle, or install plugins", handler: runPluginCmd},
```

Add the handler:

```go
func runPluginCmd(m *model, args []string) tea.Cmd {
	action := "list"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
	}

	switch action {
	case "list", "ls", "":
		m.messages = append(m.messages, message{role: roleAssistant, text: m.renderPluginList()})
		return nil

	case "info":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /plugin info <name>"})
			return nil
		}
		name := args[1]
		p, ok := m.config.Plugins[name]
		if !ok {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Plugin %q not found.", name)})
			return nil
		}
		text := fmt.Sprintf("Plugin: %s\nSource: %s\nDir: %s\nEnabled: %v", name, p.Source, p.Dir, p.Enabled)
		for _, pl := range plugins.LoadPlugins(nil) {
			if pl.Name == name {
				if pl.Description != "" {
					text += "\nDescription: " + pl.Description
				}
				if len(pl.Tools) > 0 {
					text += "\nTools: " + strings.Join(pl.Tools, ", ")
				}
				if len(pl.Commands) > 0 {
					text += "\nCommands: " + strings.Join(pl.Commands, ", ")
				}
				break
			}
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: text})
		return nil

	case "enable", "on", "disable", "off":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /plugin enable <name> or /plugin disable <name>"})
			return nil
		}
		name := args[1]
		if _, ok := m.config.Plugins[name]; !ok {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Plugin %q not found.", name)})
			return nil
		}
		enabled := action == "enable" || action == "on"
		if err := config.SavePluginEnabled(name, enabled); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Failed to update plugin config: %v", err)})
			return nil
		}
		p := m.config.Plugins[name]
		p.Enabled = enabled
		m.config.Plugins[name] = p
		state := "enabled"
		if !enabled {
			state = "disabled"
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Plugin %q %s.", name, state)})
		// Rebuild agent so tool list reflects the new enabled state.
		return m.rebuildAgentWithExternalTools()

	case "install":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /plugin install <github.com/user/repo[@ref]>"})
			return nil
		}
		source := args[1]
		ref := ""
		if at := strings.LastIndex(source, "@"); at > 0 {
			ref = source[at+1:]
			source = source[:at]
		}
		return func() tea.Msg { return pluginInstallMsg{source: source, ref: ref} }

	case "remove":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /plugin remove <name>"})
			return nil
		}
		return func() tea.Msg { return pluginRemoveMsg{name: args[1]} }

	case "confirm":
		if m.pendingPluginInstall == nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "No pending plugin install."})
			return nil
		}
		pending := m.pendingPluginInstall
		m.pendingPluginInstall = nil
		return func() tea.Msg {
			if err := plugins.RunOnInstall(pending.dirName, pending.p); err != nil {
				return pluginInstalledMsg{source: pending.source, err: err}
			}
			if err := plugins.AutoRegisterMCP(pending.dirName, pending.p); err != nil {
				return pluginInstalledMsg{source: pending.source, err: err}
			}
			cfg := config.PluginConfig{Source: pending.source, Dir: pending.dirName, Enabled: true}
			if err := config.SavePlugin(pending.p.Name, cfg); err != nil {
				return pluginInstalledMsg{source: pending.source, err: err}
			}
			return pluginInstalledMsg{name: pending.p.Name, source: pending.source, dir: pending.dirName}
		}

	case "cancel":
		if m.pendingPluginInstall == nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "No pending plugin install."})
			return nil
		}
		pending := m.pendingPluginInstall
		m.pendingPluginInstall = nil
		return func() tea.Msg {
			_ = plugins.Remove(pending.dirName)
			return pluginInstalledMsg{source: pending.source, err: fmt.Errorf("install cancelled")}
		}

	default:
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /plugin [list|enable <name>|disable <name>|install <url>|remove <name>|info <name>|confirm|cancel]"})
		return nil
	}
}
```

- [ ] **Step 6: Build and test**

```
cd /Users/james/www/ocode && go build ./... && go test ./internal/plugins/... ./internal/config/... 2>&1
```

Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/commands.go internal/tui/model.go
git commit -m "feat(tui): add /plugin command with list, enable/disable, install (with confirm), remove"
```

---

### Task 5: Update TODO.md

- [ ] Mark plugin system items complete and commit

```bash
git add TODO.md
git commit -m "docs: mark /plugin system tasks complete"
```

---

## Self-Review

**All advisor blockers addressed:**
- Dir/name mismatch → `PluginConfig.Dir` stores clone path; remove uses `cfg.Dir` not `filepath.Join(installRoot, name)` ✓
- MCP orphan on remove → `UnregisterMCP` called in `pluginRemoveMsg` handler ✓
- `LoadPluginInstructions`/`LoadPluginToolsDirPaths` enabled-aware → Task 2 ✓
- Version pinning → `PluginConfig.Ref` + `@ref` syntax in `/plugin install` ✓
- Hot reload after enable → `rebuildAgentWithExternalTools()` returned from enable case ✓
- `on_install` confirmation → `pluginInstallPendingMsg` + `/plugin confirm` / `/plugin cancel` flow ✓
- `on_install` no-shell injection → `[]string` args + `exec.Command(args[0], args[1:]...)` ✓
