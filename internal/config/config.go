package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type Config struct {
	Model        string                 `json:"model"`
	SmallModel   string                 `json:"small_model"`
	Provider     map[string]interface{} `json:"provider"`
	Tools        map[string]bool        `json:"tools"`
	Permission   map[string]interface{} `json:"permission"`
	Agent        map[string]interface{} `json:"agent"`
	DefaultAgent string                 `json:"default_agent"`
}

func Load() (*Config, error) {
	config := &Config{
		Tools:      make(map[string]bool),
		Permission: make(map[string]interface{}),
		Provider:   make(map[string]interface{}),
	}

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

	return config, nil
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

func loadFromFile(path string, config *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Simplified merging logic: unmarshal into a temp config and merge
	var temp Config
	if err := json.Unmarshal(data, &temp); err != nil {
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
