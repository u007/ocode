package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/u007/ocode/internal/snapshot"
)

const (
	lastModelKey          = "last_model"
	lastThinkingBudgetKey = "last_thinking_budget"
)

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

	// IDEMode values control the /ide VS Code integration.
	IDEModeOff    = "off"
	IDEModeClaude = "claude"
)

type AdvisorConfig struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// ClaudeCode uses the Claude Code CLI (claude -p) as the advisor backend
	// instead of an LLM API client. The model field holds the Anthropic model
	// name (e.g. "claude-sonnet-4-6") passed to claude --model.
	ClaudeCode bool `json:"claude_code,omitempty"`
}

// PluginsConfig gates opt-in builtin tools that ship disabled by default.
// Toggled at runtime via `/plugin enable|disable <name>` and persisted here.
type PluginsConfig struct {
	// AST enables the LSP-backed semantic code-navigation tool ("ast").
	AST bool `json:"ast"`
}

// SecurityConfig holds security-related settings.
// DiscoveryConfig gates the opt-in discovery-based skill/MCP retrieval
// feature. When Enabled is false, the rest of the block is ignored and
// behavior is byte-identical to a build without discovery support.
type DiscoveryConfig struct {
	Enabled          bool
	EmbeddingModel   string
	EmbeddingBackend string // "http" | "local"
	LocalModelStatus string // none | downloading | ready
	// LocalServerURL, when set, is the OpenAI-compatible base URL of an
	// already-running embed server (LM Studio, user-built llama-server, etc.)
	// that the local backend should adopt instead of downloading and spawning
	// its own. The probe validates the /v1/models response shape. Empty means
	// "use the bundled server on the manifest port and download if needed".
	LocalServerURL string
	PinnedSkills   []string
	// IgnorePaths is a list of path prefixes (relative to the work dir) that the
	// markdown discovery walker will skip. Supports both plain prefixes and glob
	// patterns (matched via filepath.Match against the repo-relative slash path).
	// "skills/" is always included by default.
	IgnorePaths []string
}

type SecurityConfig struct {
	Redaction RedactionConfig `json:"redaction"`
}

// RedactionConfig controls the secret redaction feature.
type RedactionConfig struct {
	Enabled          bool     `json:"enabled"`
	Model            string   `json:"model"`
	BaseURL          string   `json:"base_url"`                    // base URL of the local model server (e.g. http://localhost:11434)
	FailMode         string   `json:"fail_mode"`                   // "block" or "warn"
	Mode             string   `json:"mode"`                        // "lenient" (default when enabled) or "full"; governs typed-user-message LLM aggressiveness
	AllowRemoteTier2 bool     `json:"allow_remote_tier2"`          // allow non-local endpoints for tier-2 scanner
	SkipLLMIfClean   *bool    `json:"skip_llm_if_clean,omitempty"` // DEPRECATED: use Mode; nil = derive from Mode
	CustomWords      []string `json:"custom_words"`
}

type OcodeConfig struct {
	Compact     CompactConfig
	Advisor     AdvisorConfig
	Permissions PermissionConfig
	Plugins     PluginsConfig
	Security    SecurityConfig
	Discovery   DiscoveryConfig
	// MemoryEnabled toggles injection of the ocode-mem skill and memory files
	// into the agent prompt.
	MemoryEnabled bool
	// DocPromptEnabled toggles injection of a documentation-first development
	// prompt into the agent's system prompt so it reads existing docs before
	// implementing and updates them afterward.
	DocPromptEnabled  bool
	ExtraAllowedPaths []string
	Editor            string
	EditorMode        string
	IDEMode           string
	SmallModel        string
	SmallModelEnabled bool
	CommitMsgModel    string
	CommitMsgPrompt   string
	TUI               TUIConfig
	MaxSteps          int `json:"max_steps,omitempty"`
	// MaxImageDim caps the longest edge (px) of an embedded image; larger
	// images are downscaled to fit, preserving aspect ratio. 0 means use the
	// agent package default (2000).
	MaxImageDim int `json:"image_max_dim,omitempty"`
	// RecapTimeoutSeconds controls the timeout for /recap summary generation.
	// Defaults to 120 if not configured.
	RecapTimeoutSeconds int `json:"recap_timeout_seconds,omitempty"`

	// UploadDir overrides the default directory used by the /upload and
	// /api/uploads endpoints. When empty, files are stored under
	// <workDir>/.ocode/uploads.
	UploadDir string `json:"upload_dir,omitempty"`
	Extra     map[string]json.RawMessage
}

type PermissionConfig struct {
	Mode  string                `json:"mode,omitempty"`
	Tools map[string]string     `json:"tools,omitempty"`
	Bash  BashPermissionConfig  `json:"bash,omitempty"`
	Auto  *AutoPermissionConfig `json:"auto,omitempty"`
}

// AutoPermissionConfig holds the LLM auto-permission layer state, described in
// docs/superpowers/specs/2026-06-01-llm-auto-permission-design.md. The
// auto-permission layer is OFF by default; when enabled, the agent consults a
// configured small model to auto-grant or fall through to human Ask. The
// permission model can only `allow` or `ask`; it cannot emit a deny-override,
// cannot escalate the permission mode, and cannot widen past the static
// guardrails (hard-blocks remain deterministic and final).
type AutoPermissionConfig struct {
	Enabled                  bool   `json:"enabled,omitempty"`
	Model                    string `json:"model,omitempty"`
	AllowDestructive         bool   `json:"allow_destructive,omitempty"`
	Prompt                   string `json:"prompt,omitempty"`
	MaxContextBytes          int    `json:"max_context_bytes,omitempty"`
	MaxContextSources        int    `json:"max_context_sources,omitempty"`
	MaxContextLinesPerSource int    `json:"max_context_lines_per_source,omitempty"`
	// MinConfidence is the strict threshold an interpreter-execution effect
	// summary must meet for Go to auto-approve it (see the 2026-06-02 follow-up).
	MinConfidence float64     `json:"min_confidence,omitempty"`
	Grants        []AutoGrant `json:"grants,omitempty"`
}

type autoPermissionConfigFile struct {
	Enabled                  *bool       `json:"enabled"`
	Model                    *string     `json:"model"`
	AllowDestructive         *bool       `json:"allow_destructive"`
	Prompt                   *string     `json:"prompt"`
	MaxContextBytes          *int        `json:"max_context_bytes"`
	MaxContextSources        *int        `json:"max_context_sources"`
	MaxContextLinesPerSource *int        `json:"max_context_lines_per_source"`
	MinConfidence            *float64    `json:"min_confidence"`
	Grants                   []AutoGrant `json:"grants"`
}

type permissionConfigFile struct {
	Mode  string                    `json:"mode,omitempty"`
	Tools map[string]string         `json:"tools,omitempty"`
	Bash  BashPermissionConfig      `json:"bash,omitempty"`
	Auto  *autoPermissionConfigFile `json:"auto,omitempty"`
}

// AutoGrant is a typed, narrow, durable rule derived from a single tool/bash
// invocation. Auto-permission does not invent or widen rule scope; the model
// returns only a decision and reason, and Go derives one of these typed
// entries before persisting.
type AutoGrant struct {
	Kind              string          `json:"kind"`
	Tool              string          `json:"tool,omitempty"`
	NormalizedArgs    json.RawMessage `json:"normalized_args,omitempty"`
	NormalizedCommand string          `json:"normalized_command,omitempty"`
	Destructive       bool            `json:"destructive,omitempty"`
	Domain            string          `json:"domain,omitempty"`
	// Interpreter-execution grant fields (kind "interpreter_exact"). The durable
	// grant is keyed by normalized command + resolved entrypoint path + cwd +
	// source hash so path/flag/cwd changes do not silently reuse it.
	Language             string `json:"language,omitempty"`
	SourceMode           string `json:"source_mode,omitempty"`
	EntrypointPath       string `json:"entrypoint_path,omitempty"`
	EntrypointSHA256     string `json:"entrypoint_sha256,omitempty"`
	EmbeddedSourceSHA256 string `json:"embedded_source_sha256,omitempty"`
	CWD                  string `json:"cwd,omitempty"`
}

type BashPermissionConfig struct {
	Prefixes          map[string]string `json:"prefixes,omitempty"`
	AutoAllowPrefixes []string          `json:"auto_allow_prefixes,omitempty"`
	PrefixModes       map[string]string `json:"prefix_modes,omitempty"`
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

type tuiConfigFile struct {
	Theme         string            `json:"theme"`
	Mouse         *bool             `json:"mouse"`
	Scroll        float64           `json:"scroll_speed"`
	Keybinds      map[string]string `json:"keybinds"`
	LeaderTimeout int               `json:"leader_timeout"`
	Branchless    *bool             `json:"branchless"`
}

type advisorConfigFile struct {
	Enabled    *bool  `json:"enabled"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	ClaudeCode *bool  `json:"claude_code,omitempty"`
}

type pluginsConfigFile struct {
	AST    *bool `json:"ast"`
	Memory *bool `json:"memory"`
}

type redactionConfigFile struct {
	Enabled          *bool    `json:"enabled"`
	Model            *string  `json:"model"`
	BaseURL          *string  `json:"base_url"`
	FailMode         *string  `json:"fail_mode"`
	Mode             *string  `json:"mode"`
	AllowRemoteTier2 *bool    `json:"allow_remote_tier2,omitempty"`
	SkipLLMIfClean   *bool    `json:"skip_llm_if_clean,omitempty"`
	CustomWords      []string `json:"custom_words"`
}

type securityConfigFile struct {
	Redaction redactionConfigFile `json:"redaction"`
}

type discoveryConfigFile struct {
	Enabled          *bool    `json:"enabled,omitempty"`
	EmbeddingModel   string   `json:"embedding_model,omitempty"`
	EmbeddingBackend string   `json:"embedding_backend,omitempty"`
	LocalModelStatus string   `json:"local_model_status,omitempty"`
	LocalServerURL   string   `json:"local_server_url,omitempty"`
	PinnedSkills     []string `json:"pinned_skills,omitempty"`
	IgnorePaths      []string `json:"ignore_paths,omitempty"`
}

type ocodeConfigFile struct {
	Compact             compactConfigFile    `json:"compact"`
	Advisor             advisorConfigFile    `json:"advisor"`
	Permissions         permissionConfigFile `json:"permissions"`
	Plugins             pluginsConfigFile    `json:"plugins"`
	Security            securityConfigFile   `json:"security"`
	Discovery           discoveryConfigFile  `json:"discovery"`
	MemoryEnabled       *bool                `json:"memory_enabled,omitempty"`
	DocPromptEnabled    *bool                `json:"doc_prompt_enabled,omitempty"`
	ExtraAllowedPaths   []string             `json:"extra_allowed_paths,omitempty"`
	Editor              string               `json:"editor,omitempty"`
	EditorMode          string               `json:"editor_mode,omitempty"`
	IDEMode             string               `json:"ide_mode,omitempty"`
	SmallModel          string               `json:"small_model,omitempty"`
	SmallModelEnabled   *bool                `json:"small_model_enabled,omitempty"`
	RecapTimeoutSeconds *int                 `json:"recap_timeout_seconds,omitempty"`
	CommitMsgModel      string               `json:"commit_msg_model,omitempty"`
	CommitMsgPrompt     string               `json:"commit_msg_prompt,omitempty"`
	TUI                 tuiConfigFile        `json:"tui"`
	MaxSteps            int                  `json:"max_steps,omitempty"`
	MaxImageDim         int                  `json:"image_max_dim,omitempty"`
	UploadDir           string               `json:"upload_dir,omitempty"`
}

func defaultCompactConfig() CompactConfig {
	return CompactConfig{
		Enabled:               true,
		TokenThreshold:        0.85,
		KeepRecentTurns:       3,
		MinMessages:           8,
		SummaryTimeoutSeconds: 90,
		SummaryMaxRetries:     1,
		MaxSummaryInputTokens: 50000,
	}
}

func defaultTUIConfig() TUIConfig {
	mouseDefault := true
	return TUIConfig{
		Mouse:         &mouseDefault,
		Scroll:        3.0,
		LeaderTimeout: 2000,
	}
}

func defaultSecurityConfig() SecurityConfig {
	return SecurityConfig{
		Redaction: RedactionConfig{
			Enabled:          false,
			Model:            "",
			FailMode:         "block",
			Mode:             "",
			AllowRemoteTier2: false,
			SkipLLMIfClean:   nil,
		},
	}
}

func defaultOcodeConfig() OcodeConfig {
	return OcodeConfig{
		Compact:             defaultCompactConfig(),
		Advisor:             defaultAdvisorConfig(),
		Permissions:         defaultPermissionConfig(),
		MemoryEnabled:       true,
		SmallModelEnabled:   true,
		Security:            defaultSecurityConfig(),
		Discovery:           defaultDiscoveryConfig(),
		RecapTimeoutSeconds: 120,
		TUI:                 defaultTUIConfig(),
		Extra:               make(map[string]json.RawMessage),
	}
}

func defaultDiscoveryConfig() DiscoveryConfig {
	return DiscoveryConfig{
		Enabled:          false,
		EmbeddingModel:   "",
		EmbeddingBackend: "http",
		LocalModelStatus: "none",
		PinnedSkills:     []string{"brainstorming", "using-superpowers"},
		IgnorePaths:      []string{"skills/"},
	}
}

func defaultAdvisorConfig() AdvisorConfig {
	return AdvisorConfig{
		Enabled:  true,
		Provider: "deepseek",
		Model:    "deepseek-v4-pro",
	}
}

func defaultPermissionConfig() PermissionConfig {
	return PermissionConfig{
		Mode: "normal",
		Tools: map[string]string{
			"read":            "allow",
			"glob":            "allow",
			"grep":            "allow",
			"list":            "allow",
			"lsp":             "allow",
			"ast":             "allow",
			"write":           "allow",
			"edit":            "allow",
			"multi_edit":      "allow",
			"multiedit":       "allow",
			"multi_file_edit": "allow",
			"replace_lines":   "allow",
			"apply_patch":     "allow",
			"delete":          "ask",
			"format":          "allow",
			"bash":            "ask",
			"webfetch":        "ask",
			"websearch":       "ask",
			"agent":           "ask",
			"task":            "ask",
			"skill":           "allow",
			"question":        "allow",
		},
		Bash: BashPermissionConfig{Prefixes: map[string]string{}, AutoAllowPrefixes: []string{}, PrefixModes: map[string]string{}},
		Auto: &AutoPermissionConfig{
			Enabled:                  false,
			Model:                    "",
			AllowDestructive:         false,
			Prompt:                   "",
			MaxContextBytes:          4096,
			MaxContextSources:        2,
			MaxContextLinesPerSource: 80,
			MinConfidence:            0.85,
			Grants:                   nil,
		},
	}
}

func LoadOcodeConfig(cfg *Config) error {
	ocode := defaultOcodeConfig()

	globalPath, err := getGlobalOcodeConfigPath()
	if err == nil {
		if err := loadOcodeConfigFile(globalPath, &ocode); err != nil {
			return fmt.Errorf("load global ocode config: %w", err)
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
		return fmt.Errorf("parse ocodeconfig %s: %w", path, err)
	}

	var file ocodeConfigFile
	if err := json.Unmarshal(cleanData, &file); err != nil {
		return fmt.Errorf("decode ocodeconfig %s: %w", path, err)
	}

	if _, ok := raw["compact"]; ok {
		applyCompactConfig(&cfg.Compact, file.Compact)
		delete(raw, "compact")
	}

	if _, ok := raw["advisor"]; ok {
		applyAdvisorConfig(&cfg.Advisor, file.Advisor)
		delete(raw, "advisor")
	}

	if _, ok := raw["permissions"]; ok {
		applyPermissionConfig(&cfg.Permissions, file.Permissions)
		delete(raw, "permissions")
	}

	if _, ok := raw["plugins"]; ok {
		if file.Plugins.AST != nil {
			cfg.Plugins.AST = *file.Plugins.AST
		}
		delete(raw, "plugins")
	}

	if _, ok := raw["security"]; ok {
		applySecurityConfig(&cfg.Security, file.Security)
		delete(raw, "security")
	}

	if _, ok := raw["discovery"]; ok {
		applyDiscoveryConfig(&cfg.Discovery, file.Discovery)
		delete(raw, "discovery")
	}

	if _, ok := raw["extra_allowed_paths"]; ok {
		cfg.ExtraAllowedPaths = append([]string{}, file.ExtraAllowedPaths...)
		delete(raw, "extra_allowed_paths")
	}

	if _, ok := raw["editor"]; ok {
		if file.Editor != "" {
			cfg.Editor = file.Editor
		}
		delete(raw, "editor")
	}

	if _, ok := raw["editor_mode"]; ok {
		if file.EditorMode != "" {
			cfg.EditorMode = file.EditorMode
		}
		delete(raw, "editor_mode")
	}

	if _, ok := raw["ide_mode"]; ok {
		if file.IDEMode != "" {
			cfg.IDEMode = file.IDEMode
		}
		delete(raw, "ide_mode")
	}

	if _, ok := raw["small_model"]; ok {
		if file.SmallModel != "" {
			cfg.SmallModel = file.SmallModel
		}
		delete(raw, "small_model")
	}
	if _, ok := raw["small_model_enabled"]; ok {
		if file.SmallModelEnabled != nil {
			cfg.SmallModelEnabled = *file.SmallModelEnabled
		}
		delete(raw, "small_model_enabled")
	}

	if _, ok := raw["commit_msg_model"]; ok {
		if file.CommitMsgModel != "" {
			cfg.CommitMsgModel = file.CommitMsgModel
		}
		delete(raw, "commit_msg_model")
	}

	if _, ok := raw["commit_msg_prompt"]; ok {
		if file.CommitMsgPrompt != "" {
			cfg.CommitMsgPrompt = file.CommitMsgPrompt
		}
		delete(raw, "commit_msg_prompt")
	}

	if _, ok := raw["recap_timeout_seconds"]; ok {
		if file.RecapTimeoutSeconds != nil {
			cfg.RecapTimeoutSeconds = *file.RecapTimeoutSeconds
		}
		delete(raw, "recap_timeout_seconds")
	}

	if _, ok := raw["memory_enabled"]; ok {
		if file.MemoryEnabled != nil {
			cfg.MemoryEnabled = *file.MemoryEnabled
		}
		delete(raw, "memory_enabled")
	}

	if _, ok := raw["doc_prompt_enabled"]; ok {
		if file.DocPromptEnabled != nil {
			cfg.DocPromptEnabled = *file.DocPromptEnabled
		}
		delete(raw, "doc_prompt_enabled")
	}

	if _, ok := raw["tui"]; ok {
		applyTUIConfig(&cfg.TUI, file.TUI)
		delete(raw, "tui")
	}

	if _, ok := raw["max_steps"]; ok {
		if file.MaxSteps > 0 {
			cfg.MaxSteps = file.MaxSteps
		}
		delete(raw, "max_steps")
	}

	if _, ok := raw["image_max_dim"]; ok {
		if file.MaxImageDim > 0 {
			cfg.MaxImageDim = file.MaxImageDim
		}
		delete(raw, "image_max_dim")
	}

	if _, ok := raw["upload_dir"]; ok {
		cfg.UploadDir = file.UploadDir
		delete(raw, "upload_dir")
	}

	if cfg.Extra == nil {
		cfg.Extra = make(map[string]json.RawMessage)
	}
	for k, v := range raw {
		cfg.Extra[k] = v
	}

	return nil
}

func applyPermissionConfig(dst *PermissionConfig, src permissionConfigFile) {
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
	dst.Bash.AutoAllowPrefixes = append([]string(nil), src.Bash.AutoAllowPrefixes...)
	if dst.Bash.PrefixModes == nil {
		dst.Bash.PrefixModes = make(map[string]string)
	}
	for k, v := range src.Bash.PrefixModes {
		dst.Bash.PrefixModes[k] = v
	}
	// Auto block: when present in src, merge field-by-field so unset fields
	// keep their default values (e.g. MaxContextBytes: 4096). A nil src.Auto
	// means the user did not set the block in the file, so we leave the
	// destination's defaults intact.
	if src.Auto != nil {
		applyAutoPermissionConfig(dst.Auto, src.Auto)
	}
}

func applyAutoPermissionConfig(dst *AutoPermissionConfig, src *autoPermissionConfigFile) {
	if dst == nil {
		return
	}
	if src == nil {
		return
	}
	if src.Enabled != nil {
		dst.Enabled = *src.Enabled
	}
	if src.Model != nil {
		dst.Model = *src.Model
	}
	if src.AllowDestructive != nil {
		dst.AllowDestructive = *src.AllowDestructive
	}
	if src.Prompt != nil {
		dst.Prompt = *src.Prompt
	}
	if src.MaxContextBytes != nil {
		dst.MaxContextBytes = *src.MaxContextBytes
	}
	if src.MaxContextSources != nil {
		dst.MaxContextSources = *src.MaxContextSources
	}
	if src.MaxContextLinesPerSource != nil {
		dst.MaxContextLinesPerSource = *src.MaxContextLinesPerSource
	}
	if src.MinConfidence != nil {
		dst.MinConfidence = *src.MinConfidence
	}
	if src.Grants != nil {
		// Replace (not append) — Grants is the persisted auto-grant list as
		// derived by Go; the file is a complete snapshot of that list.
		dst.Grants = append([]AutoGrant(nil), src.Grants...)
	}
}

func applySecurityConfig(dst *SecurityConfig, src securityConfigFile) {
	if src.Redaction.Enabled != nil {
		dst.Redaction.Enabled = *src.Redaction.Enabled
	}
	if src.Redaction.Model != nil {
		dst.Redaction.Model = *src.Redaction.Model
	}
	if src.Redaction.BaseURL != nil {
		dst.Redaction.BaseURL = *src.Redaction.BaseURL
	}
	if src.Redaction.FailMode != nil {
		dst.Redaction.FailMode = *src.Redaction.FailMode
	}
	if src.Redaction.Mode != nil {
		dst.Redaction.Mode = *src.Redaction.Mode
	}
	if src.Redaction.AllowRemoteTier2 != nil {
		dst.Redaction.AllowRemoteTier2 = *src.Redaction.AllowRemoteTier2
	}
	if src.Redaction.SkipLLMIfClean != nil {
		dst.Redaction.SkipLLMIfClean = src.Redaction.SkipLLMIfClean
	}
	if src.Redaction.CustomWords != nil {
		dst.Redaction.CustomWords = append([]string(nil), src.Redaction.CustomWords...)
	}
}

func applyTUIConfig(dst *TUIConfig, src tuiConfigFile) {
	if src.Theme != "" {
		dst.Theme = src.Theme
	}
	if src.Mouse != nil {
		dst.Mouse = src.Mouse
	}
	if src.Scroll != 0 {
		dst.Scroll = src.Scroll
	}
	if src.LeaderTimeout != 0 {
		dst.LeaderTimeout = src.LeaderTimeout
	}
	if src.Branchless != nil {
		dst.Branchless = *src.Branchless
	}
	if dst.Keybinds == nil {
		dst.Keybinds = make(map[string]string)
	}
	for k, v := range src.Keybinds {
		dst.Keybinds[k] = v
	}
}

func applyAdvisorConfig(dst *AdvisorConfig, src advisorConfigFile) {
	if src.Provider != "" {
		dst.Provider = src.Provider
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.Enabled != nil {
		dst.Enabled = *src.Enabled
	}
	if src.ClaudeCode != nil {
		dst.ClaudeCode = *src.ClaudeCode
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

func applyDiscoveryConfig(dst *DiscoveryConfig, src discoveryConfigFile) {
	if src.Enabled != nil {
		dst.Enabled = *src.Enabled
	}
	if src.EmbeddingModel != "" {
		dst.EmbeddingModel = src.EmbeddingModel
	}
	if src.EmbeddingBackend != "" {
		dst.EmbeddingBackend = src.EmbeddingBackend
	}
	if src.LocalModelStatus != "" {
		dst.LocalModelStatus = src.LocalModelStatus
	}
	if src.LocalServerURL != "" {
		dst.LocalServerURL = src.LocalServerURL
	}
	if src.PinnedSkills != nil {
		dst.PinnedSkills = append([]string{}, src.PinnedSkills...)
	}
	if src.IgnorePaths != nil {
		dst.IgnorePaths = append([]string{}, src.IgnorePaths...)
	}
}

func SaveOcodeConfig(cfg *OcodeConfig) error {
	path, err := ActiveOcodeConfigPath()
	if err != nil {
		return err
	}
	return writeOcodeConfigFile(path, cfg)
}

func writeOcodeConfigFile(path string, cfg *OcodeConfig) error {
	if cfg == nil {
		d := defaultOcodeConfig()
		cfg = &d
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	discoveryMap := map[string]interface{}{
		"enabled":            cfg.Discovery.Enabled,
		"embedding_model":    cfg.Discovery.EmbeddingModel,
		"embedding_backend":  cfg.Discovery.EmbeddingBackend,
		"local_model_status": cfg.Discovery.LocalModelStatus,
		"pinned_skills":      cfg.Discovery.PinnedSkills,
		"ignore_paths":       cfg.Discovery.IgnorePaths,
	}
	// local_server_url is opt-in: empty stays the default (use the bundled
	// server), a non-empty value is persisted so a user pointing at LM
	// Studio (or a custom llama-server) doesn't have to re-set it on every
	// run.
	if cfg.Discovery.LocalServerURL != "" {
		discoveryMap["local_server_url"] = cfg.Discovery.LocalServerURL
	}
	payload := map[string]interface{}{
		"compact":     cfg.Compact,
		"advisor":     cfg.Advisor,
		"permissions": cfg.Permissions,
		"security":    cfg.Security,
		"discovery":   discoveryMap,
	}
	if cfg.Plugins.AST {
		payload["plugins"] = cfg.Plugins
	}
	if len(cfg.ExtraAllowedPaths) > 0 {
		payload["extra_allowed_paths"] = cfg.ExtraAllowedPaths
	}
	if cfg.Editor != "" {
		payload["editor"] = cfg.Editor
	}
	if cfg.EditorMode != "" && cfg.EditorMode != EditorModeExternal {
		payload["editor_mode"] = cfg.EditorMode
	}
	if cfg.IDEMode != "" {
		payload["ide_mode"] = cfg.IDEMode
	}
	if cfg.SmallModel != "" {
		payload["small_model"] = cfg.SmallModel
	}
	payload["small_model_enabled"] = cfg.SmallModelEnabled
	if cfg.CommitMsgModel != "" {
		payload["commit_msg_model"] = cfg.CommitMsgModel
	}
	if cfg.CommitMsgPrompt != "" {
		payload["commit_msg_prompt"] = cfg.CommitMsgPrompt
	}
	payload["memory_enabled"] = cfg.MemoryEnabled
	payload["doc_prompt_enabled"] = cfg.DocPromptEnabled
	if cfg.MaxSteps > 0 {
		payload["max_steps"] = cfg.MaxSteps
	}
	if cfg.MaxImageDim > 0 {
		payload["image_max_dim"] = cfg.MaxImageDim
	}
	if cfg.UploadDir != "" {
		payload["upload_dir"] = cfg.UploadDir
	}
	if cfg.TUI.Theme != "" || cfg.TUI.Mouse != nil || cfg.TUI.Scroll != 0 || cfg.TUI.LeaderTimeout != 0 || len(cfg.TUI.Keybinds) > 0 {
		payload["tui"] = cfg.TUI
	}
	for k, v := range cfg.Extra {
		if k == "compact" || k == "advisor" || k == "permissions" || k == "plugins" || k == "extra_allowed_paths" || k == "max_steps" || k == "discovery" {
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
	// Atomic write: a crash mid-write must not truncate the live config (every
	// saver re-reads this file, so a corrupt write would cascade to defaults).
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write ocode config tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename ocode config: %w", err)
	}
	return nil
}

func SaveOcodePermissions(permissions PermissionConfig) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	// Preserve the on-disk auto-permission block (model, grants, context
	// limits) when persisting permissions. ExportConfig only carries
	// `enabled`; model/grants/limits are owned elsewhere and would otherwise
	// be erased here by a session whose in-memory snapshot predates them (or a
	// concurrent session). The caller is authoritative only for `enabled` and
	// `grants`. The auto-permission `model` is owned EXCLUSIVELY by
	// SavePermissionModel (the /permissions model command): a permissions write
	// must never set or clear it, or a session that merely toggled a tool rule
	// would clobber a model another concurrent session selected on disk.
	if permissions.Auto != nil {
		merged := AutoPermissionConfig{}
		if cfg.Permissions.Auto != nil {
			merged = *cfg.Permissions.Auto // start from disk: preserves model + limits
		}
		merged.Enabled = permissions.Auto.Enabled
		if permissions.Auto.Grants != nil {
			merged.Grants = permissions.Auto.Grants
		}
		permissions.Auto = &merged
	} else if cfg.Permissions.Auto != nil {
		// This session never had an auto block but disk gained one (a concurrent
		// session wrote it). We hold no authoritative opinion on it — not even
		// `enabled` — so preserve the disk block verbatim.
		permissions.Auto = cfg.Permissions.Auto
	}
	cfg.Permissions = permissions
	return SaveOcodeConfig(cfg)
}

// SaveMaxSteps persists the max steps setting to the ocode config.
// 0 or negative clears the override (unlimited, default cap of 100 applies).
func SaveMaxSteps(n int) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.MaxSteps = n
	return SaveOcodeConfig(cfg)
}

// SaveAutoPermissionEnabled persists only the auto-permission `enabled` flag
// using load-modify-write, so it cannot clobber a concurrent session's
// model/grants/tool rules the way a wholesale config write would.
func SaveAutoPermissionEnabled(enabled bool) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	if cfg.Permissions.Auto == nil {
		cfg.Permissions.Auto = &AutoPermissionConfig{}
	}
	cfg.Permissions.Auto.Enabled = enabled
	return SaveOcodeConfig(cfg)
}

// SaveExtraAllowedPath appends one cleaned path to extra_allowed_paths using
// load-modify-write (no-op if already present), avoiding a wholesale config
// write that would drop concurrent changes to other fields.
func SaveExtraAllowedPath(path string) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cleaned := filepath.Clean(path)
	for _, existing := range cfg.ExtraAllowedPaths {
		if filepath.Clean(existing) == cleaned {
			return nil
		}
	}
	cfg.ExtraAllowedPaths = append(cfg.ExtraAllowedPaths, cleaned)
	return SaveOcodeConfig(cfg)
}

// autoGrantKey returns the identity used to de-duplicate auto-grants. Interpreter
// grants are keyed by the same exact source identity used by MatchInterpreterGrant:
// kind, language, source mode, command identity, path identity, and the relevant
// source hash.
func autoGrantKey(g AutoGrant) string {
	return strings.Join([]string{
		g.Kind, g.Language, g.SourceMode,
		g.NormalizedCommand, g.EntrypointPath,
		g.EntrypointSHA256, g.EmbeddedSourceSHA256,
		g.CWD,
	}, "\x00")
}

// SaveAutoGrant appends one narrow auto-grant to permissions.auto.grants using
// load-modify-write (no-op if an identical grant already exists), avoiding a
// wholesale config write that would drop concurrent changes to other fields.
func SaveAutoGrant(grant AutoGrant) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	if cfg.Permissions.Auto == nil {
		cfg.Permissions.Auto = &AutoPermissionConfig{}
	}
	key := autoGrantKey(grant)
	for _, existing := range cfg.Permissions.Auto.Grants {
		if autoGrantKey(existing) == key {
			return nil
		}
	}
	cfg.Permissions.Auto.Grants = append(cfg.Permissions.Auto.Grants, grant)
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

// SaveUploadDir persists only the upload_dir field using load-modify-write so
// it cannot clobber a concurrent session's other config.
func SaveUploadDir(dir string) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.UploadDir = dir
	return SaveOcodeConfig(cfg)
}

// SaveDiscoveryEnabled persists only the discovery.enabled flag using
// load-modify-write so it cannot clobber a concurrent session's other config.
func SaveDiscoveryEnabled(enabled bool) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.Discovery.Enabled = enabled
	return SaveOcodeConfig(cfg)
}

// SaveQueryEmbeddingModel persists the discovery embedding model + backend.
// An empty backend preserves the existing on-disk value.
func SaveQueryEmbeddingModel(modelID, backend string) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.Discovery.EmbeddingModel = modelID
	if backend != "" {
		cfg.Discovery.EmbeddingBackend = backend
	}
	return SaveOcodeConfig(cfg)
}

// SaveDiscoveryIgnorePaths persists the discovery ignore-paths list.
func SaveDiscoveryIgnorePaths(paths []string) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.Discovery.IgnorePaths = paths
	return SaveOcodeConfig(cfg)
}

// SaveLocalModelStatus persists the local model download status.
func SaveLocalModelStatus(status string) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.Discovery.LocalModelStatus = status
	return SaveOcodeConfig(cfg)
}

// SaveOcodeASTPlugin persists the enabled state of the opt-in "ast" tool.
func SaveOcodeASTPlugin(enabled bool) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.Plugins.AST = enabled
	return SaveOcodeConfig(cfg)
}

// SaveIDEMode persists only the ide_mode field using load-modify-write so it
// cannot clobber a concurrent session's other config.
func SaveIDEMode(mode string) error {
	switch mode {
	case IDEModeOff, IDEModeClaude:
	default:
		return fmt.Errorf("invalid ide_mode: %q (valid: %s, %s)", mode, IDEModeOff, IDEModeClaude)
	}
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.IDEMode = mode
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

func ActiveOcodeConfigPath() (string, error) {
	return getGlobalOcodeConfigPath()
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

// SaveLastThinkingBudget persists the last used thinking budget into ocodeconfig.json
// so it can be restored across sessions.
func SaveLastThinkingBudget(budget int) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}

	raw, _ := json.Marshal(budget)
	cfg.Extra[lastThinkingBudgetKey] = json.RawMessage(raw)

	return SaveOcodeConfig(cfg)
}

// GetLastThinkingBudget retrieves the last saved thinking budget from
// ocodeconfig.json. Returns 0 if not set.
func GetLastThinkingBudget() int {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return 0
	}
	if raw, ok := cfg.Extra[lastThinkingBudgetKey]; ok {
		var val int
		if err := json.Unmarshal(raw, &val); err == nil && val >= 0 {
			return val
		}
	}
	return 0
}

// loadFullOcodeConfig loads the full ocode config from the global and project
// ocodeconfig.json files, merging them together.
func loadFullOcodeConfig() (*OcodeConfig, error) {
	ocode := defaultOcodeConfig()

	globalPath, err := getGlobalOcodeConfigPath()
	if err == nil {
		if err := loadOcodeConfigFile(globalPath, &ocode); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

	return &ocode, nil
}

func SaveTUITheme(theme string) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.TUI.Theme = theme
	return SaveOcodeConfig(cfg)
}

func SaveAdvisorModel(providerModel string) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	if providerModel == "" {
		// Reset to defaults.
		cfg.Advisor = defaultAdvisorConfig()
		return SaveOcodeConfig(cfg)
	}
	provider, model := SplitProviderModel(providerModel)
	if provider == "" || model == "" {
		return fmt.Errorf("advisor model must be in provider/model format")
	}
	cfg.Advisor.Provider = provider
	cfg.Advisor.Model = model
	cfg.Advisor.ClaudeCode = (provider == "claude-code")
	return SaveOcodeConfig(cfg)
}

// SaveDocPromptEnabled persists the doc-prompt toggle to config.
func SaveDocPromptEnabled(enabled bool) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.DocPromptEnabled = enabled
	return SaveOcodeConfig(cfg)
}

// SaveAdvisorEnabled persists the advisor enabled/disabled state to config.
func SaveAdvisorEnabled(enabled bool) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.Advisor.Enabled = enabled
	return SaveOcodeConfig(cfg)
}

// SaveMemoryEnabled persists the memory prompt-injection toggle to config.
func SaveMemoryEnabled(enabled bool) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.MemoryEnabled = enabled
	return SaveOcodeConfig(cfg)
}

// ResolveRedactionMode returns the effective redaction mode for a RedactionConfig.
// When Mode is set it wins; when empty the legacy SkipLLMIfClean is consulted
// (false → "full", true/nil → "lenient"). Returns "lenient" as the ultimate default.
func ResolveRedactionMode(rc RedactionConfig) string {
	if rc.Mode != "" {
		return rc.Mode
	}
	// Legacy back-compat: skip_llm_if_clean=false → "full"
	if rc.SkipLLMIfClean != nil && !*rc.SkipLLMIfClean {
		return "full"
	}
	return "lenient"
}

// SaveSecurityRedaction persists the security.redaction config via a targeted load-modify-save.
func SaveSecurityRedaction(mutate func(*RedactionConfig)) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	mutate(&cfg.Security.Redaction)
	return SaveOcodeConfig(cfg)
}

// DefaultAdvisorConfig returns the default advisor configuration.
func DefaultAdvisorConfig() AdvisorConfig {
	return defaultAdvisorConfig()
}

// DefaultAdvisorProvider returns the default advisor provider name.
func DefaultAdvisorProvider() string {
	return defaultAdvisorConfig().Provider
}

// DefaultAdvisorModelName returns the default advisor model name (without provider prefix).
func DefaultAdvisorModelName() string {
	return defaultAdvisorConfig().Model
}

// SplitProviderModel splits "provider/model" into (provider, model).
// If no "/" separator is present, provider is empty.
func SplitProviderModel(s string) (string, string) {
	if parts := strings.SplitN(s, "/", 2); len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", s
}

func SaveSmallModel(model string) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.SmallModel = model
	return SaveOcodeConfig(cfg)
}

// SaveSmallModelEnabled persists the small model enabled/disabled state to config.
func SaveSmallModelEnabled(enabled bool) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.SmallModelEnabled = enabled
	return SaveOcodeConfig(cfg)
}

// SavePermissionModel persists the auto-permission model override.
// Set to empty string to clear the override and fall back to the small model.
func SavePermissionModel(providerModel string) error {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	if cfg.Permissions.Auto == nil {
		cfg.Permissions.Auto = &AutoPermissionConfig{Enabled: false}
	}
	cfg.Permissions.Auto.Model = providerModel
	return SaveOcodeConfig(cfg)
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
