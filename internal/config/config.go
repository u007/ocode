package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
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

func (c *MCPConfig) UnmarshalJSON(data []byte) error {
	tmp := struct {
		Type        string            `json:"type"`
		Command     json.RawMessage   `json:"command"`
		URL         string            `json:"url"`
		Environment map[string]string `json:"environment"`
		Headers     map[string]string `json:"headers"`
		Enabled     *bool             `json:"enabled"`
		Timeout     int               `json:"timeout"`
		OAuth       *MCPOAuthConfig   `json:"oauth"`
	}{}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	c.Type = tmp.Type
	c.URL = tmp.URL
	c.Environment = tmp.Environment
	c.Headers = tmp.Headers
	c.Timeout = tmp.Timeout
	c.OAuth = tmp.OAuth
	if len(tmp.Command) > 0 && string(tmp.Command) != "null" {
		var parts []string
		if err := json.Unmarshal(tmp.Command, &parts); err == nil {
			c.Command = parts
		} else {
			var command string
			if err := json.Unmarshal(tmp.Command, &command); err != nil {
				return fmt.Errorf("parse mcp command: %w", err)
			}
			c.Command = strings.Fields(command)
		}
	}
	if tmp.Enabled == nil {
		c.Enabled = true
	} else {
		c.Enabled = *tmp.Enabled
	}
	if c.Timeout == 0 {
		c.Timeout = 5000
	}
	if c.Type == "" {
		if c.URL != "" {
			c.Type = "remote"
		} else {
			c.Type = "local"
		}
	}
	return nil
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

type HookConfig struct {
	Pre  []string `json:"pre"`
	Post []string `json:"post"`
}

type FormatterConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Files   []string `json:"files"`
}

type Config struct {
	Model          string                     `json:"model"`
	ThinkingBudget int                        `json:"-"` // runtime-only: extended thinking token budget
	Provider       map[string]interface{}     `json:"provider"`
	Tools          map[string]bool            `json:"tools"`
	Permission     map[string]interface{}     `json:"permission"`
	Agent          map[string]interface{}     `json:"agent"`
	DefaultAgent   string                     `json:"default_agent"`
	MCP            map[string]MCPConfig       `json:"mcp"`
	Watcher        WatcherConfig              `json:"watcher"`
	Hooks          map[string]HookConfig      `json:"hooks"`
	Formatters     map[string]FormatterConfig `json:"formatters"`
	RemoteConfig   string                     `json:"remote_config"`
	Ocode          OcodeConfig                `json:"-"`
}

func Load() (*Config, error) {
	config := &Config{
		Tools:      make(map[string]bool),
		Permission: make(map[string]interface{}),
		Provider:   make(map[string]interface{}),
		Hooks:      make(map[string]HookConfig),
		Formatters: make(map[string]FormatterConfig),
	}

	globalPath, err := getGlobalConfigPath()
	if err == nil {
		if err := loadFromFile(globalPath, config); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load global config: %w", err)
		}
	}

	if customDir := os.Getenv("OPENCODE_CONFIG_DIR"); customDir != "" {
		if err := loadFromDir(customDir, config); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load custom config dir: %w", err)
		}
	}

	projectPath, err := getProjectConfigPath()
	if err == nil {
		if err := loadFromFile(projectPath, config); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load project config: %w", err)
		}
	}

	projectRoot := FindProjectRoot()
	if projectRoot != "" {
		for _, dirName := range []string{".opencode", ".opencodes"} {
			dir := filepath.Join(projectRoot, dirName)
			if err := loadFromDir(dir, config); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to load %s config: %w", dirName, err)
			}
		}
	}

	if model := os.Getenv("OPENCODE_MODEL"); model != "" {
		config.Model = model
	} else if recent := LoadRecentModels(); len(recent) > 0 {
		config.Model = recent[0]
	}

	if err := LoadOcodeConfig(config); err != nil {
		return nil, fmt.Errorf("failed to load ocode config: %w", err)
	}

	// The TUI's own last_model records every model string, including shorthand
	// names that cannot be represented in opencode's provider/model recent list.
	if os.Getenv("OPENCODE_MODEL") == "" {
		if lastModel := GetLastModel(); lastModel != "" {
			config.Model = lastModel
		}
	}

	config.ThinkingBudget = GetLastThinkingBudget()

	if content := os.Getenv("OPENCODE_CONFIG_CONTENT"); content != "" {
		if err := loadFromString(content, config); err != nil {
			return nil, fmt.Errorf("failed to parse OPENCODE_CONFIG_CONTENT: %w", err)
		}
	}

	if config.RemoteConfig != "" {
		if err := mergeRemoteConfig(config); err != nil {
			fmt.Fprintf(os.Stderr, "warning: remote config failed: %v\n", err)
		}
	}

	return config, nil
}

func SaveMCPEnabled(name string, enabled bool) error {
	configPath, err := (&Config{}).ActiveConfigPath()
	if err != nil {
		return err
	}

	existing, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config: %w", err)
	}
	m := map[string]any{}
	if len(existing) > 0 {
		jsoncData := stripJSONCComments(existing)
		if err := json.Unmarshal(jsoncData, &m); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
	}
	mcpRaw, ok := m["mcp"].(map[string]any)
	if !ok {
		return fmt.Errorf("mcp server %q not found in opencode config", name)
	}
	serverRaw, ok := mcpRaw[name].(map[string]any)
	if !ok {
		return fmt.Errorf("mcp server %q not found in opencode config", name)
	}
	serverRaw["enabled"] = enabled
	mcpRaw[name] = serverRaw
	m["mcp"] = mcpRaw

	return saveJSONFile(configPath, m)
}

func SaveMCPServer(name string, server MCPConfig) error {
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
		mcpRaw = map[string]any{}
	}
	serverData, err := json.Marshal(server)
	if err != nil {
		return fmt.Errorf("marshal mcp server: %w", err)
	}
	var serverRaw map[string]any
	if err := json.Unmarshal(serverData, &serverRaw); err != nil {
		return fmt.Errorf("parse mcp server: %w", err)
	}
	mcpRaw[name] = serverRaw
	m["mcp"] = mcpRaw

	return saveJSONFile(configPath, m)
}

func ClearMCPAuthorization(name string) (bool, error) {
	configPath, err := (&Config{}).ActiveConfigPath()
	if err != nil {
		return false, err
	}

	m, err := loadConfigMap(configPath)
	if err != nil {
		return false, err
	}
	mcpRaw, ok := m["mcp"].(map[string]any)
	if !ok {
		return false, fmt.Errorf("mcp server %q not found in opencode config", name)
	}
	serverRaw, ok := mcpRaw[name].(map[string]any)
	if !ok {
		return false, fmt.Errorf("mcp server %q not found in opencode config", name)
	}
	headersRaw, ok := serverRaw["headers"].(map[string]any)
	if !ok || headersRaw["Authorization"] == nil {
		return false, nil
	}
	delete(headersRaw, "Authorization")
	if len(headersRaw) == 0 {
		delete(serverRaw, "headers")
	} else {
		serverRaw["headers"] = headersRaw
	}
	mcpRaw[name] = serverRaw
	m["mcp"] = mcpRaw

	return true, saveJSONFile(configPath, m)
}

func loadConfigMap(path string) (map[string]any, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read config: %w", err)
	}
	m := map[string]any{}
	if len(existing) > 0 {
		jsoncData := stripJSONCComments(existing)
		if err := json.Unmarshal(jsoncData, &m); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}
	return m, nil
}

func saveJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return os.Rename(tmp, path)
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
	cleanData := stripJSONCComments([]byte(content))

	var temp Config
	if err := json.Unmarshal(cleanData, &temp); err != nil {
		return err
	}

	if temp.Model != "" {
		config.Model = temp.Model
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
	for k, v := range temp.Formatters {
		config.Formatters[k] = v
	}
	for k, v := range temp.Hooks {
		config.Hooks[k] = v
	}
	if temp.RemoteConfig != "" {
		config.RemoteConfig = temp.RemoteConfig
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

// jsoncComments matches either a quoted string (to skip it) or a comment (to remove it).
var jsoncComments = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|//[^\n]*|/\*[\s\S]*?\*/`)

// stripJSONCComments removes // and /* */ comments from JSONC content while
// preserving URLs and other // sequences inside quoted strings.
func stripJSONCComments(data []byte) []byte {
	return jsoncComments.ReplaceAllFunc(data, func(match []byte) []byte {
		if match[0] == '"' {
			return match // preserve quoted strings
		}
		return nil // remove comments
	})
}

func mergeRemoteConfig(config *Config) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(config.RemoteConfig)
	if err != nil {
		return fmt.Errorf("fetch remote config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote config returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read remote config body: %w", err)
	}

	var remote Config
	if err := json.Unmarshal(body, &remote); err != nil {
		return fmt.Errorf("parse remote config: %w", err)
	}

	if remote.Model != "" && config.Model == "" {
		config.Model = remote.Model
	}
	if remote.DefaultAgent != "" && config.DefaultAgent == "" {
		config.DefaultAgent = remote.DefaultAgent
	}
	if config.MCP == nil {
		config.MCP = make(map[string]MCPConfig)
	}
	for k, v := range remote.MCP {
		if _, exists := config.MCP[k]; !exists {
			config.MCP[k] = v
		}
	}
	for k, v := range remote.Tools {
		if _, exists := config.Tools[k]; !exists {
			config.Tools[k] = v
		}
	}
	for _, ignore := range remote.Watcher.Ignore {
		config.Watcher.Ignore = append(config.Watcher.Ignore, ignore)
	}
	for k, v := range remote.Permission {
		if _, exists := config.Permission[k]; !exists {
			config.Permission[k] = v
		}
	}
	for k, v := range remote.Provider {
		if _, exists := config.Provider[k]; !exists {
			config.Provider[k] = v
		}
	}
	for k, v := range remote.Hooks {
		if _, exists := config.Hooks[k]; !exists {
			config.Hooks[k] = v
		}
	}
	for k, v := range remote.Formatters {
		if _, exists := config.Formatters[k]; !exists {
			config.Formatters[k] = v
		}
	}

	return nil
}

func loadFromFile(path string, config *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	cleanData := stripJSONCComments(data)

	var temp Config
	if err := json.Unmarshal(cleanData, &temp); err != nil {
		return err
	}

	if temp.Model != "" {
		config.Model = temp.Model
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
	for k, v := range temp.Hooks {
		config.Hooks[k] = v
	}
	if temp.RemoteConfig != "" {
		config.RemoteConfig = temp.RemoteConfig
	}
	for k, v := range temp.Formatters {
		config.Formatters[k] = v
	}

	return nil
}
