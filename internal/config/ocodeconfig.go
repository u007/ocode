package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/jamesmercstudio/ocode/internal/snapshot"
)

const lastModelKey = "last_model"

type CompactConfig struct {
	Enabled               bool    `json:"enabled"`
	SummaryProvider       string  `json:"summary_provider"`
	SummaryModel          string  `json:"summary_model"`
	TokenThreshold        float64 `json:"token_threshold"`
	KeepRecentTurns       int     `json:"keep_recent_turns"`
	MinMessages           int     `json:"min_messages"`
	SummaryTimeoutSeconds int     `json:"summary_timeout_seconds"`
	SummaryMaxRetries     int     `json:"summary_max_retries"`
	MaxSummaryInputTokens int     `json:"max_summary_input_tokens"`
}

const (
	EditorModeExternal   = "external"
	EditorModeTmuxSplit  = "tmux-split"
	EditorModeTmuxWindow = "tmux-window"
)

type OcodeConfig struct {
	Compact     CompactConfig
	Permissions PermissionConfig
	Editor      string
	EditorMode  string
	Extra       map[string]json.RawMessage
}

type PermissionConfig struct {
	Mode  string               `json:"mode,omitempty"`
	Tools map[string]string    `json:"tools,omitempty"`
	Bash  BashPermissionConfig `json:"bash,omitempty"`
}

type BashPermissionConfig struct {
	Prefixes map[string]string `json:"prefixes,omitempty"`
}

type compactConfigFile struct {
	Enabled               *bool    `json:"enabled"`
	SummaryProvider       *string  `json:"summary_provider"`
	SummaryModel          *string  `json:"summary_model"`
	TokenThreshold        *float64 `json:"token_threshold"`
	KeepRecentTurns       *int     `json:"keep_recent_turns"`
	MinMessages           *int     `json:"min_messages"`
	SummaryTimeoutSeconds *int     `json:"summary_timeout_seconds"`
	SummaryMaxRetries     *int     `json:"summary_max_retries"`
	MaxSummaryInputTokens *int     `json:"max_summary_input_tokens"`
}

type ocodeConfigFile struct {
	Compact     compactConfigFile `json:"compact"`
	Permissions PermissionConfig  `json:"permissions"`
	Editor      string            `json:"editor,omitempty"`
	EditorMode  string            `json:"editor_mode,omitempty"`
}

func defaultCompactConfig() CompactConfig {
	return CompactConfig{
		Enabled:               true,
		TokenThreshold:        0.85,
		KeepRecentTurns:       3,
		MinMessages:           8,
		SummaryTimeoutSeconds: 30,
		SummaryMaxRetries:     1,
		MaxSummaryInputTokens: 50000,
	}
}

func defaultOcodeConfig() *OcodeConfig {
	return &OcodeConfig{
		Compact:     defaultCompactConfig(),
		Permissions: defaultPermissionConfig(),
		Extra:       make(map[string]json.RawMessage),
	}
}

func defaultPermissionConfig() PermissionConfig {
	return PermissionConfig{
		Mode: "normal",
		Tools: map[string]string{
			"read":          "allow",
			"glob":          "allow",
			"grep":          "allow",
			"list":          "allow",
			"lsp":           "allow",
			"write":         "allow",
			"edit":          "allow",
			"multi_edit":    "allow",
			"multiedit":     "allow",
			"replace_lines": "allow",
			"patch":         "allow",
			"delete":        "ask",
			"format":        "allow",
			"bash":          "ask",
			"webfetch":      "ask",
			"websearch":     "ask",
			"agent":         "ask",
			"task":          "ask",
			"skill":         "allow",
			"question":      "allow",
		},
		Bash: BashPermissionConfig{Prefixes: map[string]string{}},
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

	if ocode.EditorMode == "" {
		if os.Getenv("TMUX") != "" {
			ocode.EditorMode = EditorModeTmuxSplit
		} else {
			ocode.EditorMode = EditorModeExternal
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

	cleanData := stripJSONCComments(data)
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

	if _, ok := raw["permissions"]; ok {
		var file ocodeConfigFile
		if err := json.Unmarshal(cleanData, &file); err != nil {
			return err
		}
		applyPermissionConfig(&cfg.Permissions, file.Permissions)
		delete(raw, "permissions")
	}

	if _, ok := raw["editor"]; ok {
		var file struct {
			Editor string `json:"editor"`
		}
		if err := json.Unmarshal(cleanData, &file); err != nil {
			return err
		}
		if file.Editor != "" {
			cfg.Editor = file.Editor
		}
		delete(raw, "editor")
	}

	if _, ok := raw["editor_mode"]; ok {
		var file ocodeConfigFile
		if err := json.Unmarshal(cleanData, &file); err != nil {
			return err
		}
		if file.EditorMode != "" {
			cfg.EditorMode = file.EditorMode
		}
		delete(raw, "editor_mode")
	}

	if cfg.Extra == nil {
		cfg.Extra = make(map[string]json.RawMessage)
	}
	for k, v := range raw {
		cfg.Extra[k] = v
	}

	return nil
}

func applyPermissionConfig(dst *PermissionConfig, src PermissionConfig) {
	if src.Mode != "" {
		dst.Mode = src.Mode
	}
	if dst.Tools == nil {
		dst.Tools = make(map[string]string)
	}
	for k, v := range src.Tools {
		dst.Tools[k] = v
	}
	if dst.Bash.Prefixes == nil {
		dst.Bash.Prefixes = make(map[string]string)
	}
	for k, v := range src.Bash.Prefixes {
		dst.Bash.Prefixes[k] = v
	}
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
	if src.TokenThreshold != nil {
		dst.TokenThreshold = *src.TokenThreshold
	}
	if src.KeepRecentTurns != nil {
		dst.KeepRecentTurns = *src.KeepRecentTurns
	}
	if src.MinMessages != nil {
		dst.MinMessages = *src.MinMessages
	}
	if src.SummaryTimeoutSeconds != nil {
		dst.SummaryTimeoutSeconds = *src.SummaryTimeoutSeconds
	}
	if src.SummaryMaxRetries != nil {
		dst.SummaryMaxRetries = *src.SummaryMaxRetries
	}
	if src.MaxSummaryInputTokens != nil {
		dst.MaxSummaryInputTokens = *src.MaxSummaryInputTokens
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
		"compact":     cfg.Compact,
		"permissions": cfg.Permissions,
	}
	if cfg.Editor != "" {
		payload["editor"] = cfg.Editor
	}
	if cfg.EditorMode != "" && cfg.EditorMode != EditorModeExternal {
		payload["editor_mode"] = cfg.EditorMode
	}
	for k, v := range cfg.Extra {
		if k == "compact" || k == "permissions" {
			continue
		}
		payload[k] = v
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := snapshot.Backup(path); err != nil {
		return fmt.Errorf("backup ocode config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func SaveOcodePermissions(permissions PermissionConfig) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.Permissions = permissions
	return SaveOcodeConfig(cfg)
}

func SaveEditor(editor string) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.Editor = editor
	return SaveOcodeConfig(cfg)
}

func SaveEditorMode(mode string) error {
	switch mode {
	case EditorModeExternal, EditorModeTmuxSplit, EditorModeTmuxWindow:
	default:
		return fmt.Errorf("invalid editor_mode: %q (valid: %s, %s, %s)", mode, EditorModeExternal, EditorModeTmuxSplit, EditorModeTmuxWindow)
	}
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.EditorMode = mode
	return SaveOcodeConfig(cfg)
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

// SaveLastModel persists the last used provider/model string into the ocodeconfig.json
// file so it can be restored across sessions.
func SaveLastModel(providerModel string) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}

	raw, _ := json.Marshal(providerModel)
	cfg.Extra[lastModelKey] = json.RawMessage(raw)

	return SaveOcodeConfig(cfg)
}

// GetLastModel retrieves the last saved provider/model string from ocodeconfig.json.
// Returns empty string if not set.
func GetLastModel() string {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return ""
	}
	if raw, ok := cfg.Extra[lastModelKey]; ok {
		var val string
		if err := json.Unmarshal(raw, &val); err == nil && val != "" {
			return val
		}
	}
	return ""
}

// loadFullOcodeConfig loads the full ocode config from the global and project
// ocodeconfig.json files, merging them together.
func loadFullOcodeConfig() (*OcodeConfig, error) {
	ocode := defaultOcodeConfig()

	globalPath, err := getGlobalOcodeConfigPath()
	if err == nil {
		if err := loadOcodeConfigFile(globalPath, ocode); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

	projectPath, err := getProjectOcodeConfigPath()
	if err == nil {
		if err := loadOcodeConfigFile(projectPath, ocode); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

	return ocode, nil
}

// ResolveEditor returns the editor to use for opening files.
// Priority: ocodeconfig.json "editor" field > $VISUAL > $EDITOR > "vi"
func ResolveEditor(cfg *OcodeConfig) string {
	if cfg != nil && cfg.Editor != "" {
		return cfg.Editor
	}
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if v := os.Getenv("EDITOR"); v != "" {
		return v
	}
	return "vi"
}
