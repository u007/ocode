package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type CompactConfig struct {
	Enabled         bool   `json:"enabled"`
	SummaryProvider string `json:"summary_provider"`
	SummaryModel    string `json:"summary_model"`
}

type OcodeConfig struct {
	Compact CompactConfig
	Extra   map[string]json.RawMessage
}

type compactConfigFile struct {
	Enabled         *bool   `json:"enabled"`
	SummaryProvider *string `json:"summary_provider"`
	SummaryModel    *string `json:"summary_model"`
}

type ocodeConfigFile struct {
	Compact compactConfigFile `json:"compact"`
}

func defaultCompactConfig() CompactConfig {
	return CompactConfig{
		Enabled: true,
	}
}

func defaultOcodeConfig() *OcodeConfig {
	return &OcodeConfig{
		Compact: defaultCompactConfig(),
		Extra:   make(map[string]json.RawMessage),
	}
}

func LoadOcodeConfig(cfg *Config) error {
	ocode := defaultOcodeConfig()

	globalPath, err := getGlobalOcodeConfigPath()
	if err == nil {
		if err := loadOcodeConfigFile(globalPath, ocode); err != nil {
			return fmt.Errorf("load global ocode config: %w", err)
		}
	}

	projectPath, err := getProjectOcodeConfigPath()
	if err == nil {
		if err := loadOcodeConfigFile(projectPath, ocode); err != nil {
			return fmt.Errorf("load project ocode config: %w", err)
		}
	}

	cfg.Ocode = ocode
	return nil
}

func loadOcodeConfigFile(path string, cfg *OcodeConfig) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	cleanData := jsoncComments.ReplaceAll(data, []byte(""))
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(cleanData, &raw); err != nil {
		return err
	}

	if _, ok := raw["compact"]; ok {
		var file ocodeConfigFile
		if err := json.Unmarshal(cleanData, &file); err != nil {
			return err
		}
		applyCompactConfig(&cfg.Compact, file.Compact)
		delete(raw, "compact")
	}

	if cfg.Extra == nil {
		cfg.Extra = make(map[string]json.RawMessage)
	}
	for k, v := range raw {
		cfg.Extra[k] = v
	}

	return nil
}

func applyCompactConfig(dst *CompactConfig, src compactConfigFile) {
	if src.Enabled != nil {
		dst.Enabled = *src.Enabled
	}
	if src.SummaryProvider != nil {
		dst.SummaryProvider = *src.SummaryProvider
	}
	if src.SummaryModel != nil {
		dst.SummaryModel = *src.SummaryModel
	}
}

func SaveOcodeConfig(cfg *OcodeConfig) error {
	path, err := getGlobalOcodeConfigPath()
	if err != nil {
		return err
	}
	return writeOcodeConfigFile(path, cfg)
}

func writeOcodeConfigFile(path string, cfg *OcodeConfig) error {
	if cfg == nil {
		cfg = defaultOcodeConfig()
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	payload := map[string]interface{}{
		"compact": cfg.Compact,
	}
	for k, v := range cfg.Extra {
		if k == "compact" {
			continue
		}
		payload[k] = v
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

func getGlobalOcodeConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "opencode", "ocodeconfig.json"), nil
	}
	return filepath.Join(home, ".config", "opencode", "ocodeconfig.json"), nil
}

func getProjectOcodeConfigPath() (string, error) {
	dir, err := findProjectConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ocodeconfig.json"), nil
}
