package plugins

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/u007/ocode/internal/bundled"
)

type PluginMCPConfig struct {
	Server       string   `json:"server"`
	AutoRegister bool     `json:"auto_register"`
	Command      []string `json:"command"`
}

type Plugin struct {
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	Version      string           `json:"version"`
	Commands     []string         `json:"commands"`
	Tools        []string         `json:"tools"`
	Instructions string           `json:"instructions"`
	OnInstall    []string         `json:"on_install"`
	MCP          *PluginMCPConfig `json:"mcp"`
	// Dir is the absolute filesystem directory containing the manifest. Not
	// persisted in plugin.json; populated by LoadPlugins from the scan.
	Dir string `json:"-"`
	// Format is FormatOcode (has plugin.json) or FormatClaude (has only a
	// Claude Code .claude-plugin/plugin.json).
	Format string `json:"-"`
	// HasClaudeManifest is true when the plugin ships a Claude Code manifest,
	// which opts it into Claude Code conventions (hooks/hooks.json).
	HasClaudeManifest bool `json:"-"`
	// Source is SourceOcode for plugins in ocode's plugin dirs and
	// SourceClaudeCode for plugins installed by Claude Code.
	Source string `json:"-"`
	// SkillDirs are the plugin's skill roots (skills/ plus manifest extras).
	SkillDirs []string `json:"-"`
	// DefaultEnabled is the plugin's state when ocode's config has no entry
	// for it: true for ocode plugins; for a Claude Code install, whatever
	// Claude Code's own enabledPlugins says (enabled when unset). An ocode
	// config entry always overrides it, and ocode never writes Claude Code's
	// settings, so toggling in ocode leaves Claude Code untouched.
	DefaultEnabled bool `json:"-"`
}

func LoadPlugins(enabled map[string]bool) []Plugin {
	return LoadPluginsForProject(enabled, "")
}

// LoadPluginsForProject loads the enabled plugins from the standard search
// paths, using projectRoot for project-scoped discovery instead of
// os.Getwd(). When projectRoot is empty, falls back to the legacy
// findProjectRoot() path.
//
// enabled is ocode's per-plugin config (name → on). A plugin with an entry
// follows it; one without falls back to its DefaultEnabled.
func LoadPluginsForProject(enabled map[string]bool, projectRoot string) []Plugin {
	var out []Plugin
	for _, p := range LoadAllPluginsForProject(projectRoot) {
		on := p.DefaultEnabled
		if v, ok := enabled[p.Name]; ok {
			on = v
		}
		if on {
			out = append(out, p)
		}
	}
	return out
}

// LoadAllPluginsForProject returns every discoverable plugin, enabled or
// not, one per name. Candidates arrive in precedence order (ocode's global,
// project and bundled dirs, then Claude Code's installed plugins), so
// first-wins dedupe makes a disk copy win over the bundled one and any ocode
// plugin shadow a Claude Code install of the same name — even when the ocode
// copy is disabled, so disabling it never lets the other copy through.
func LoadAllPluginsForProject(projectRoot string) []Plugin {
	var plugins []Plugin
	seen := make(map[string]bool)
	for _, c := range pluginCandidatesForProject(projectRoot) {
		p, ok := readPluginManifest(c.dir)
		if !ok {
			continue
		}
		if p.Name == "" {
			p.Name = filepath.Base(c.dir)
		}
		if seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		p.Dir = c.dir
		p.Source = c.source
		p.DefaultEnabled = c.defaultEnabled
		plugins = append(plugins, p)
	}
	return plugins
}

func pluginSearchPaths() []string {
	return pluginSearchPathsForProject("")
}

// pluginSearchPathsForProject returns the plugin search paths, using
// projectRoot for the project-local .opencode/plugins directory. When
// projectRoot is empty, falls back to findProjectRoot() (os.Getwd()).
func pluginSearchPathsForProject(projectRoot string) []string {
	paths := make([]string, 0, 3)
	if global := globalPluginSearchPath(); global != "" {
		paths = append(paths, global)
	}
	if project := projectPluginSearchPathForRoot(projectRoot); project != "" {
		paths = append(paths, project)
	}
	// Embedded (bundled) plugins — lowest precedence; disk copies above win.
	if bundled.PluginsDir != "" {
		paths = append(paths, bundled.PluginsDir)
	}
	return paths
}

// FindPluginDir returns the directory containing plugin.json for the named
// plugin, searching the same precedence order as LoadPlugins. Empty if not
// found.
func FindPluginDir(name string) string {
	return FindPluginDirForProject(name, "")
}

// FindPluginDirForProject returns the directory containing plugin.json for the
// named plugin, using projectRoot for project-scoped discovery. When
// projectRoot is empty, falls back to the legacy os.Getwd() path.
func FindPluginDirForProject(name, projectRoot string) string {
	for _, dir := range pluginSearchPathsForProject(projectRoot) {
		candidate := filepath.Join(dir, name)
		if _, ok := readPluginManifest(candidate); ok {
			return candidate
		}
		// Also handle case where the manifest's name field differs from dir
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p, ok := readPluginManifest(filepath.Join(dir, e.Name()))
			if !ok {
				continue
			}
			if p.Name == "" {
				p.Name = e.Name()
			}
			if p.Name == name {
				return filepath.Join(dir, e.Name())
			}
		}
	}
	return ""
}

// LoadBundledPluginAgentsDirPaths returns the agents/ subdirectories for the
// embedded (bundled) plugins. The agent registry consumes it as the
// lowest-precedence source so any disk-based plugin agent always overrides the
// bundled copy.
func LoadBundledPluginAgentsDirPaths(enabled map[string]bool) []string {
	root := bundled.PluginsDir
	if root == "" {
		return nil
	}
	return loadPluginSubdirPaths([]string{root}, "agents", enabled)
}

func globalPluginSearchPath() string {
	home, _ := os.UserHomeDir()
	globalPath := filepath.Join(home, ".config", "opencode", "plugins")
	if runtime.GOOS == "windows" {
		globalPath = filepath.Join(os.Getenv("APPDATA"), "opencode", "plugins")
	}
	return globalPath
}

func projectPluginSearchPath() string {
	return projectPluginSearchPathForRoot("")
}

// projectPluginSearchPathForRoot returns the project-local plugin directory.
// When projectRoot is non-empty it is used directly; otherwise falls back to
// findProjectRoot() which uses os.Getwd() (legacy behavior for TUI/tests).
func projectPluginSearchPathForRoot(projectRoot string) string {
	if projectRoot == "" {
		projectRoot = findProjectRoot()
	}
	if projectRoot == "" {
		return ""
	}
	return filepath.Join(projectRoot, ".opencode", "plugins")
}

func findProjectRoot() string {
	curr, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		if _, err := os.Stat(filepath.Join(curr, "opencode.json")); err == nil {
			return curr
		}
		if _, err := os.Stat(filepath.Join(curr, ".git")); err == nil {
			return curr
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}

	return ""
}

func LoadPluginInstructions(enabled map[string]bool) string {
	plugins := LoadPlugins(enabled)
	if len(plugins) == 0 {
		return ""
	}

	var instructions string
	for _, p := range plugins {
		if p.Instructions != "" {
			instructions += "\n--- Plugin: " + p.Name + " ---\n" + p.Instructions + "\n"
		}
	}
	return instructions
}

func LoadPluginToolsDirPaths(enabled map[string]bool) []string {
	return loadPluginSubdirPaths(pluginSearchPaths(), "tools", enabled)
}

func LoadPluginCommandDirPaths(enabled map[string]bool) []string {
	paths := loadPluginSubdirPaths(pluginSearchPaths(), "commands", enabled)
	// Claude Code-installed plugins contribute commands too, unless an ocode
	// plugin of the same name shadows them (LoadPlugins drops those).
	for _, p := range LoadPlugins(enabled) {
		if p.Source != SourceClaudeCode {
			continue
		}
		cmdDir := filepath.Join(p.Dir, "commands")
		if info, err := os.Stat(cmdDir); err == nil && info.IsDir() {
			paths = append(paths, cmdDir)
		}
	}
	return paths
}

// LoadGlobalPluginAgentsDirPaths returns the agents/ subdirectories for global
// plugins.
func LoadGlobalPluginAgentsDirPaths(enabled map[string]bool) []string {
	root := globalPluginSearchPath()
	if root == "" {
		return nil
	}
	return loadPluginSubdirPaths([]string{root}, "agents", enabled)
}

// LoadProjectPluginAgentsDirPaths returns the agents/ subdirectories for
// project-local plugins under the discovered project root.
func LoadProjectPluginAgentsDirPaths(enabled map[string]bool) []string {
	root := projectPluginSearchPath()
	if root == "" {
		return nil
	}
	return loadPluginSubdirPaths([]string{root}, "agents", enabled)
}

func loadPluginSubdirPaths(roots []string, subdir string, enabled map[string]bool) []string {
	var paths []string

	for _, dir := range roots {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if enabled != nil {
				name := e.Name()
				if p, ok := readPluginManifest(filepath.Join(dir, e.Name())); ok && p.Name != "" {
					name = p.Name
				}
				if on, ok := enabled[name]; ok && !on {
					continue
				}
			}
			subdirPath := filepath.Join(dir, e.Name(), subdir)
			if info, err := os.Stat(subdirPath); err == nil && info.IsDir() {
				paths = append(paths, subdirPath)
			}
		}
	}

	return paths
}
