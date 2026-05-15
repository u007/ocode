package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

type MCPOAuthConfig struct {
	Enabled          *bool    `json:"enabled"`
	AuthorizationURL string   `json:"authorization_url"`
	TokenURL         string   `json:"token_url"`
	ClientID         string   `json:"client_id"`
	Scopes           []string `json:"scopes"`
}

type MCPConfig struct {
	Type        string            `json:"type"`
	Command     []string          `json:"command,omitempty"`
	URL         string            `json:"url,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Enabled     bool              `json:"enabled"`
	Timeout     int               `json:"timeout"`
	OAuth       *MCPOAuthConfig   `json:"oauth"`
}

type TUIConfig struct {
	Theme         string            `json:"theme"`
	Mouse         *bool             `json:"mouse"`
	Scroll        float64           `json:"scroll_speed"`
	Keybinds      map[string]string `json:"keybinds"`
	LeaderTimeout int               `json:"leader_timeout"`
}

type WatcherConfig struct {
	Ignore []string `json:"ignore"`
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
	Watcher      WatcherConfig          `json:"watcher"`
	Ocode        *OcodeConfig           `json:"-"`
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
	config.TUI.LeaderTimeout = 2000

	// 1. Global config
	globalPath, err := getGlobalConfigPath()
	if err == nil {
		if err := loadFromFile(globalPath, config); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load global config: %w", err)
		}
	}

	// 2. Custom config dir (OPENCODE_CONFIG_DIR)
	if customDir := os.Getenv("OPENCODE_CONFIG_DIR"); customDir != "" {
		if err := loadFromDir(customDir, config); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load custom config dir: %w", err)
		}
	}

	// 3. Project config
	projectPath, err := getProjectConfigPath()
	if err == nil {
		if err := loadFromFile(projectPath, config); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load project config: %w", err)
		}
	}

	// 4. .opencode/ directory in project root
	projectRoot := FindProjectRoot()
	if projectRoot != "" {
		for _, dirName := range []string{".opencode", ".opencodes"} {
			dir := filepath.Join(projectRoot, dirName)
			if err := loadFromDir(dir, config); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to load %s config: %w", dirName, err)
			}
		}
	}

	// 5. Simple env overrides
	if model := os.Getenv("OPENCODE_MODEL"); model != "" {
		config.Model = model
	}

	// 6. TUI config files
	loadTUIConfig(config)

	// 6. Ocode sidecar config
	if err := LoadOcodeConfig(config); err != nil {
		return nil, fmt.Errorf("failed to load ocode config: %w", err)
	}

	// 7. OPENCODE_CONFIG_CONTENT (inline JSON, highest priority)
	if content := os.Getenv("OPENCODE_CONFIG_CONTENT"); content != "" {
		if err := loadFromString(content, config); err != nil {
			return nil, fmt.Errorf("failed to parse OPENCODE_CONFIG_CONTENT: %w", err)
		}
	}

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
		Theme         string            `json:"theme"`
		Mouse         *bool             `json:"mouse"`
		Scroll        float64           `json:"scroll_speed"`
		Keybinds      map[string]string `json:"keybinds"`
		LeaderTimeout int               `json:"leader_timeout"`
	}
	if err := json.Unmarshal(cleanData, &temp); err == nil {
		if temp.Theme != "" {
			config.TUI.Theme = temp.Theme
		}
		if temp.TUI.Theme != "" {
			config.TUI.Theme = temp.TUI.Theme
		}
		if temp.Mouse != nil {
			config.TUI.Mouse = temp.Mouse
		}
		if temp.TUI.Mouse != nil {
			config.TUI.Mouse = temp.TUI.Mouse
		}
		if temp.Scroll != 0 {
			config.TUI.Scroll = temp.Scroll
		}
		if temp.TUI.Scroll != 0 {
			config.TUI.Scroll = temp.TUI.Scroll
		}
		if temp.LeaderTimeout != 0 {
			config.TUI.LeaderTimeout = temp.LeaderTimeout
		}
		if temp.TUI.LeaderTimeout != 0 {
			config.TUI.LeaderTimeout = temp.TUI.LeaderTimeout
		}
		if config.TUI.Keybinds == nil {
			config.TUI.Keybinds = make(map[string]string)
		}
		for k, v := range temp.TUI.Keybinds {
			config.TUI.Keybinds[k] = v
		}
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
	dir, err := findProjectConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "opencode.json"), nil
}

func findProjectConfigDir() (string, error) {
	curr, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		path := filepath.Join(curr, "opencode.json")
		if _, err := os.Stat(path); err == nil {
			return curr, nil
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}

		if _, err := os.Stat(filepath.Join(curr, ".git")); err == nil {
			break
		}

		curr = parent
	}

	return "", os.ErrNotExist
}

func FindProjectRoot() string {
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

func loadFromDir(dir string, config *Config) error {
	path := filepath.Join(dir, "opencode.json")
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return loadFromFile(path, config)
}

func loadFromString(content string, config *Config) error {
	cleanData := jsoncComments.ReplaceAll([]byte(content), []byte(""))

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
	for _, ignore := range temp.Watcher.Ignore {
		config.Watcher.Ignore = append(config.Watcher.Ignore, ignore)
	}
	for k, v := range temp.Permission {
		config.Permission[k] = v
	}
	for k, v := range temp.Provider {
		config.Provider[k] = v
	}

	return nil
}

func Save(cfg *Config, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write config tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename config file: %w", err)
	}
	return nil
}

func (c *Config) ActiveConfigPath() (string, error) {
	projectPath, err := getProjectConfigPath()
	if err == nil {
		if _, statErr := os.Stat(projectPath); statErr == nil {
			return projectPath, nil
		}
	}
	globalPath, err := getGlobalConfigPath()
	if err != nil {
		return "", fmt.Errorf("resolve global config path: %w", err)
	}
	return globalPath, nil
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
	for _, ignore := range temp.Watcher.Ignore {
		config.Watcher.Ignore = append(config.Watcher.Ignore, ignore)
	}
	for k, v := range temp.Permission {
		config.Permission[k] = v
	}
	for k, v := range temp.Provider {
		config.Provider[k] = v
	}

	return nil
}
