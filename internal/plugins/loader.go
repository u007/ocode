package plugins

import (
	"encoding/json"
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
	// Dir is the absolute filesystem directory containing plugin.json. Not
	// persisted in plugin.json; populated by LoadPlugins from the scan.
	Dir string `json:"-"`
}

func LoadPlugins(enabled map[string]bool) []Plugin {
	return LoadPluginsForProject(enabled, "")
}

// LoadPluginsForProject loads plugins from the standard search paths, using
// projectRoot for project-scoped discovery instead of os.Getwd(). When
// projectRoot is empty, falls back to the legacy findProjectRoot() path.
func LoadPluginsForProject(enabled map[string]bool, projectRoot string) []Plugin {
	var plugins []Plugin
	// seen dedupes by plugin name so a disk copy (listed first in
	// pluginSearchPaths) wins over the bundled/embedded copy.
	seen := make(map[string]bool)

	for _, dir := range pluginSearchPathsForProject(projectRoot) {
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
			if seen[p.Name] {
				continue
			}
			if enabled != nil {
				if on, ok := enabled[p.Name]; ok && !on {
					continue
				}
			}
			seen[p.Name] = true
			p.Dir = filepath.Join(dir, e.Name())
			plugins = append(plugins, p)
		}
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
		candidate := filepath.Join(dir, name, "plugin.json")
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Join(dir, name)
		}
		// Also handle case where plugin.json's name field differs from dir
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
	return loadPluginSubdirPaths(pluginSearchPaths(), "commands", enabled)
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
				pluginPath := filepath.Join(dir, e.Name(), "plugin.json")
				if data, err := os.ReadFile(pluginPath); err == nil {
					var p Plugin
					if err := json.Unmarshal(data, &p); err == nil && p.Name != "" {
						name = p.Name
					}
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
