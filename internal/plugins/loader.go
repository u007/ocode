package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
}

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

func pluginSearchPaths() []string {
	var paths []string

	home, _ := os.UserHomeDir()
	globalPath := filepath.Join(home, ".config", "opencode", "plugins")
	if runtime.GOOS == "windows" {
		globalPath = filepath.Join(os.Getenv("APPDATA"), "opencode", "plugins")
	}
	paths = append(paths, globalPath)

	projectRoot := findProjectRoot()
	if projectRoot != "" {
		paths = append(paths, filepath.Join(projectRoot, ".opencode", "plugins"))
	}

	return paths
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
			toolsDir := filepath.Join(dir, e.Name(), "tools")
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
			cmdsDir := filepath.Join(dir, e.Name(), "commands")
			if _, err := os.Stat(cmdsDir); err == nil {
				paths = append(paths, cmdsDir)
			}
		}
	}

	return paths
}
