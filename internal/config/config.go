package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

type MCPConfig struct {
	Type        string            `json:"type"`
	Command     []string          `json:"command,omitempty"`
	URL         string            `json:"url,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Enabled     bool              `json:"enabled"`
}

type TUIConfig struct {
	Theme  string  `json:"theme"`
	Mouse  *bool   `json:"mouse"`
	Scroll float64 `json:"scroll_speed"`
}

type Config struct {
	Model        string                 `json:"model"`
	SmallModel   string                 `json:"small_model"`
	Provider     map[string]interface{} `json:"provider"`
	Tools        map[string]bool        `json:"tools"`
	Permission   map[string]interface{} `json:"permission"`
	Agent        map[string]interface{} `json:"agent"`
	DefaultAgent string                 `json:"default_agent"`
	MCP          map[string]MCPConfig   `json:"mcp"`
	TUI          TUIConfig              `json:"tui"`
}

func Load() (*Config, error) {
	config := &Config{
		Tools:      make(map[string]bool),
		Permission: make(map[string]interface{}),
		Provider:   make(map[string]interface{}),
	}

	// Default TUI values
	mouseDefault := true
	config.TUI.Mouse = &mouseDefault
	config.TUI.Scroll = 3.0

	// 1. Global config
	globalPath, err := getGlobalConfigPath()
	if err == nil {
		if err := loadFromFile(globalPath, config); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load global config: %w", err)
		}
	}

	// 2. Project config
	projectPath, err := getProjectConfigPath()
	if err == nil {
		if err := loadFromFile(projectPath, config); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load project config: %w", err)
		}
	}

	// 3. Env overrides (simplified for now)
	if model := os.Getenv("OPENCODE_MODEL"); model != "" {
		config.Model = model
	}

	// 4. TUI config files
	loadTUIConfig(config)

	return config, nil
}

func loadTUIConfig(config *Config) {
	home, _ := os.UserHomeDir()
	globalTUI := filepath.Join(home, ".config", "opencode", "tui.json")
	if runtime.GOOS == "windows" {
		globalTUI = filepath.Join(os.Getenv("APPDATA"), "opencode", "tui.json")
	}

	projectTUI := "tui.json"

	// Load global then project
	mergeTUI(globalTUI, config)
	mergeTUI(projectTUI, config)
}

func mergeTUI(path string, config *Config) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	cleanData := jsoncComments.ReplaceAll(data, []byte(""))
	var temp struct {
		TUI TUIConfig `json:"tui"`
		// Also support top-level theme/mouse etc in tui.json
		Theme  string  `json:"theme"`
		Mouse  *bool   `json:"mouse"`
		Scroll float64 `json:"scroll_speed"`
	}
	if err := json.Unmarshal(cleanData, &temp); err == nil {
		if temp.Theme != "" { config.TUI.Theme = temp.Theme }
		if temp.TUI.Theme != "" { config.TUI.Theme = temp.TUI.Theme }
		if temp.Mouse != nil { config.TUI.Mouse = temp.Mouse }
		if temp.TUI.Mouse != nil { config.TUI.Mouse = temp.TUI.Mouse }
		if temp.Scroll != 0 { config.TUI.Scroll = temp.Scroll }
		if temp.TUI.Scroll != 0 { config.TUI.Scroll = temp.TUI.Scroll }
	}
}

func getGlobalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "opencode", "opencode.json"), nil
	}
	return filepath.Join(home, ".config", "opencode", "opencode.json"), nil
}

func getProjectConfigPath() (string, error) {
	curr, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		path := filepath.Join(curr, "opencode.json")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}

		// Also check .jsonc if needed, but for now stick to .json

		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}

		// Stop at git root
		if _, err := os.Stat(filepath.Join(curr, ".git")); err == nil {
			break
		}

		curr = parent
	}

	return "", os.ErrNotExist
}

var jsoncComments = regexp.MustCompile(`(?m)^\s*//.*$|/\*[\s\S]*?\*/|//.*$`)

func loadFromFile(path string, config *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Basic JSONC support: remove comments
	cleanData := jsoncComments.ReplaceAll(data, []byte(""))

	// Simplified merging logic: unmarshal into a temp config and merge
	var temp Config
	if err := json.Unmarshal(cleanData, &temp); err != nil {
		return err
	}

	if temp.Model != "" {
		config.Model = temp.Model
	}
	if temp.SmallModel != "" {
		config.SmallModel = temp.SmallModel
	}
	if temp.DefaultAgent != "" {
		config.DefaultAgent = temp.DefaultAgent
	}
	if config.MCP == nil {
		config.MCP = make(map[string]MCPConfig)
	}
	for k, v := range temp.MCP {
		config.MCP[k] = v
	}
	for k, v := range temp.Tools {
		config.Tools[k] = v
	}
	for k, v := range temp.Permission {
		config.Permission[k] = v
	}
	for k, v := range temp.Provider {
		config.Provider[k] = v
	}

	return nil
}
