package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/u007/ocode/internal/hook"
	"github.com/u007/ocode/internal/ocr"
	"github.com/u007/ocode/internal/snapshot"
)

const (
	lastModelKey          = "last_model"
	lastThinkingBudgetKey = "last_thinking_budget"
)

var profileNameRe = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// ProfileDelta is a sparse overlay for OcodeConfig and Config-level fields.
// Only present fields override the base; absent fields inherit.
type ProfileDelta struct {
	DisplayName             *string                    `json:"display_name,omitempty"`
	Model                   *string                    `json:"model,omitempty"`
	Provider                map[string]interface{}     `json:"provider,omitempty"`
	MCP                     map[string]MCPConfig       `json:"mcp,omitempty"`
	Permission              map[string]interface{}     `json:"permission,omitempty"`
	SmallModel              *string                    `json:"small_model,omitempty"`
	SmallModelEnabled       *bool                      `json:"small_model_enabled,omitempty"`
	RecapModel              *string                    `json:"recap_model,omitempty"`
	RecapModelEnabled       *bool                      `json:"recap_model_enabled,omitempty"`
	ExplorerModel           *string                    `json:"explorer_model,omitempty"`
	ExplorerModelEnabled    *bool                      `json:"explorer_model_enabled,omitempty"`
	ContextModel            *string                    `json:"context_model,omitempty"`
	ContextModelEnabled     *bool                      `json:"context_model_enabled,omitempty"`
	Editor                  *string                    `json:"editor,omitempty"`
	EditorMode              *string                    `json:"editor_mode,omitempty"`
	IDEMode                 *string                    `json:"ide_mode,omitempty"`
	MaxSteps                *int                       `json:"max_steps,omitempty"`
	MaxImageDim             *int                       `json:"image_max_dim,omitempty"`
	MemoryEnabled           *bool                      `json:"memory_enabled,omitempty"`
	DocPromptEnabled        *bool                      `json:"doc_prompt_enabled,omitempty"`
	TerminalShell           *string                    `json:"terminal_shell,omitempty"`
	TerminalFontFamily      *string                    `json:"terminal_font_family,omitempty"`
	TerminalFontSize        *int                       `json:"terminal_font_size,omitempty"`
	TerminalScrollbackLines *int                       `json:"terminal_scrollback_lines,omitempty"`
	TUI                     *TUIConfig                 `json:"tui,omitempty"`
	Extra                   map[string]json.RawMessage `json:"-"`
}

func ValidateProfileName(name string) error {
	if !profileNameRe.MatchString(name) {
		return fmt.Errorf("invalid profile name %q: must match [a-z0-9_-]{1,32}", name)
	}
	return nil
}

func ProfileOverrideCount(delta ProfileDelta) int {
	n := 0
	if delta.DisplayName != nil {
		n++
	}
	if delta.Model != nil {
		n++
	}
	if len(delta.Provider) > 0 {
		n += len(delta.Provider)
	}
	if len(delta.MCP) > 0 {
		n += len(delta.MCP)
	}
	if len(delta.Permission) > 0 {
		n += len(delta.Permission)
	}
	if delta.SmallModel != nil {
		n++
	}
	if delta.SmallModelEnabled != nil {
		n++
	}
	if delta.RecapModel != nil {
		n++
	}
	if delta.RecapModelEnabled != nil {
		n++
	}
	if delta.ExplorerModel != nil {
		n++
	}
	if delta.ExplorerModelEnabled != nil {
		n++
	}
	if delta.ContextModel != nil {
		n++
	}
	if delta.ContextModelEnabled != nil {
		n++
	}
	if delta.Editor != nil {
		n++
	}
	if delta.EditorMode != nil {
		n++
	}
	if delta.IDEMode != nil {
		n++
	}
	if delta.MaxSteps != nil {
		n++
	}
	if delta.MaxImageDim != nil {
		n++
	}
	if delta.MemoryEnabled != nil {
		n++
	}
	if delta.DocPromptEnabled != nil {
		n++
	}
	if delta.TerminalShell != nil {
		n++
	}
	if delta.TerminalFontFamily != nil {
		n++
	}
	if delta.TerminalFontSize != nil {
		n++
	}
	if delta.TerminalScrollbackLines != nil {
		n++
	}
	if delta.TUI != nil {
		n++
	}
	return n
}

// EffectiveOcodeConfig returns a copy of base merged with the named profile delta.
func EffectiveOcodeConfig(base *OcodeConfig, profile string) *OcodeConfig {
	if base == nil {
		d := defaultOcodeConfig()
		base = &d
	}
	if profile == "" {
		clone := *base
		return &clone
	}
	delta, ok := base.Profiles[profile]
	if !ok {
		clone := *base
		return &clone
	}
	clone := *base
	clone.Profiles = nil
	if delta.SmallModel != nil {
		clone.SmallModel = *delta.SmallModel
	}
	if delta.SmallModelEnabled != nil {
		clone.SmallModelEnabled = *delta.SmallModelEnabled
	}
	if delta.RecapModel != nil {
		clone.RecapModel = *delta.RecapModel
	}
	if delta.RecapModelEnabled != nil {
		clone.RecapModelEnabled = *delta.RecapModelEnabled
	}
	if delta.ExplorerModel != nil {
		clone.ExplorerModel = *delta.ExplorerModel
	}
	if delta.ExplorerModelEnabled != nil {
		clone.ExplorerModelEnabled = *delta.ExplorerModelEnabled
	}
	if delta.ContextModel != nil {
		clone.ContextModel = *delta.ContextModel
	}
	if delta.ContextModelEnabled != nil {
		clone.ContextModelEnabled = *delta.ContextModelEnabled
	}
	if delta.Editor != nil {
		clone.Editor = *delta.Editor
	}
	if delta.EditorMode != nil {
		clone.EditorMode = *delta.EditorMode
	}
	if delta.IDEMode != nil {
		clone.IDEMode = *delta.IDEMode
	}
	if delta.MaxSteps != nil {
		clone.MaxSteps = *delta.MaxSteps
	}
	if delta.MaxImageDim != nil {
		clone.MaxImageDim = *delta.MaxImageDim
	}
	if delta.MemoryEnabled != nil {
		clone.MemoryEnabled = *delta.MemoryEnabled
	}
	if delta.DocPromptEnabled != nil {
		clone.DocPromptEnabled = *delta.DocPromptEnabled
	}
	if delta.TerminalShell != nil {
		clone.TerminalShell = *delta.TerminalShell
	}
	if delta.TerminalFontFamily != nil {
		clone.TerminalFontFamily = *delta.TerminalFontFamily
	}
	if delta.TerminalFontSize != nil {
		clone.TerminalFontSize = *delta.TerminalFontSize
	}
	if delta.TerminalScrollbackLines != nil {
		clone.TerminalScrollbackLines = *delta.TerminalScrollbackLines
	}
	if delta.TUI != nil {
		clone.TUI = *delta.TUI
	}
	return &clone
}

// EffectiveConfig applies profile provider/mcp/model overrides to a Config copy.
func EffectiveConfig(base *Config, profile string, profiles map[string]ProfileDelta) *Config {
	if base == nil {
		return nil
	}
	if profile == "" {
		clone := *base
		return &clone
	}
	delta, ok := profiles[profile]
	if !ok {
		clone := *base
		return &clone
	}
	clone := *base
	if delta.Model != nil {
		clone.Model = *delta.Model
	}
	if len(delta.Provider) > 0 {
		if clone.Provider == nil {
			clone.Provider = make(map[string]interface{}, len(delta.Provider))
		} else {
			clone.Provider = cloneProviderMap(clone.Provider)
		}
		for k, v := range delta.Provider {
			clone.Provider[k] = v
		}
	}
	if len(delta.MCP) > 0 {
		if clone.MCP == nil {
			clone.MCP = make(map[string]MCPConfig, len(delta.MCP))
		} else {
			clone.MCP = cloneMCPMap(clone.MCP)
		}
		for k, v := range delta.MCP {
			clone.MCP[k] = v
		}
	}
	if len(delta.Permission) > 0 {
		if clone.Permission == nil {
			clone.Permission = make(map[string]interface{}, len(delta.Permission))
		} else {
			clone.Permission = cloneProviderMap(clone.Permission)
		}
		for k, v := range delta.Permission {
			clone.Permission[k] = v
		}
	}
	return &clone
}

func cloneProviderMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneMCPMap(m map[string]MCPConfig) map[string]MCPConfig {
	out := make(map[string]MCPConfig, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

type CompactConfig struct {
	Enabled               bool    `json:"enabled"`
	SummaryProvider       string  `json:"summary_provider"`
	SummaryModel          string  `json:"summary_model"`
	TokenThreshold        float64 `json:"token_threshold"`
	KeepRecentTurns       int     `json:"keep_recent_turns"`
	KeepRecentTokens      int     `json:"keep_recent_tokens"`
	MinMessages           int     `json:"min_messages"`
	SummaryTimeoutSeconds int     `json:"summary_timeout_seconds"`
	SummaryMaxRetries     int     `json:"summary_max_retries"`
	MaxSummaryInputTokens int     `json:"max_summary_input_tokens"`
}

// BrowserConfig configures the embedded headless-Chrome browser mode
// (internal/browse/cdp). ChromePath is an optional explicit path to the Chrome
// binary; IdleTimeoutMinutes is how long the shared Chrome process stays
// alive with zero active targets before it is reaped and relaunched lazily.
type BrowserConfig struct {
	ChromePath         string `json:"chrome_path"`
	IdleTimeoutMinutes int    `json:"idle_timeout_minutes"`
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
	// Checkpoints lists loop-enforced advisor consultations, applied by the
	// agent loop (not left to the model's discretion). Supported values:
	//   "done" — before the agent's final answer on a non-trivial turn, the
	//            loop injects an advisor verification of the completion claim.
	//   "plan" — the first write-tool batch of a turn is deferred until the
	//            advisor has reviewed the proposed changes.
	// Checkpoints only fire when the advisor is enabled and the checkpoint
	// is listed. The advisor model is resolved with a built-in default
	// fallback, so absence of an explicit model does not disable checkpoints.
	Checkpoints []string `json:"checkpoints"`
}

// PluginsConfig gates opt-in builtin tools that ship disabled by default.
// Toggled at runtime via `/plugin enable|disable <name>` and persisted here.
type PluginsConfig struct {
	// AST enables the opt-in "ast_grep" structural search/rewrite tool (which
	// shells out to the ast-grep CLI). The LSP-backed semantic "ast" tool is
	// registered separately and is always on when a language server is present.
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
	// The built-in defaults always include skills/, .opencode/, .claude/, .qwen/,
	// .agent/, .pnpm/, node_modules/, vendor/, .git/, dist/, build/, and target/.
	IgnorePaths []string
}

// LocalModelConfig is one user-registered local chat/completion model
// instance (see internal/discovery/instances.go). MaxParallel is the number
// of concurrent request slots (1 or 2) the running server process is
// launched with. ContextSize is the llama-server --ctx-size passed at
// launch, in tokens; 0 means "loaded from model" (llama.cpp's own
// convention), i.e. no cap — the model's full native trained context.
// Uncapped defaults are what let a long-context GGUF (e.g. a 256k-context
// model) balloon its KV cache to tens of GB on startup with a single slot.
type LocalModelConfig struct {
	Enabled     bool `json:"enabled"`
	MaxParallel int  `json:"max_parallel"`
	ContextSize int  `json:"context_size"`
}

// DefaultLocalChatContextSize is the --ctx-size a newly-registered local
// chat model gets, bounding KV cache instead of inheriting the model's full
// native trained context (see LocalModelConfig.ContextSize).
const DefaultLocalChatContextSize = 20000

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
	Browser     BrowserConfig
	// ExternalPlugins holds installable/loadable plugin packages (source,
	// dir, ref, enabled) such as the "orchestrator" plugin. Distinct from
	// PluginsConfig, which only gates built-in opt-in tools. Persisted under
	// ocodeconfig.json's "external_plugins" key — this is the source of
	// truth for external plugins; opencode.json's legacy "plugins" key is
	// only consulted as a one-time migration fallback.
	ExternalPlugins map[string]PluginConfig
	// LocalModels holds registered local chat/completion model instances
	// (see /localmodel), keyed by model id (e.g. "local/bonsai-8b-1bit").
	// Distinct from Discovery, which only covers the embedding model.
	LocalModels map[string]LocalModelConfig
	Security    SecurityConfig
	Discovery   DiscoveryConfig
	// MemoryEnabled toggles injection of the ocode-mem skill and memory files
	// into the agent prompt.
	MemoryEnabled bool
	// DocPromptEnabled toggles injection of a documentation-first development
	// prompt into the agent's system prompt so it reads existing docs before
	// implementing and updates them afterward.
	DocPromptEnabled bool
	// ProfileDebug toggles verbose profile debugging to the log tab, emitting
	// the active profile and its effective overrides (model, provider, creds,
	// mcp, etc.) when a session is built or the profile switches. Default
	// off.
	ProfileDebug bool
	// TerminalScrollbackLines controls how many lines xterm.js retains for the
	// interactive terminal. Values are normalized to the supported range when
	// config is loaded; zero means use DefaultTerminalScrollbackLines.
	TerminalScrollbackLines int
	// TerminalFontFamily overrides the interactive terminal's font stack.
	// Empty means use the frontend's built-in default monospace stack.
	TerminalFontFamily string
	// TerminalFontSize overrides the interactive terminal's font size in px.
	// Zero means use DefaultTerminalFontSize.
	TerminalFontSize int
	// TerminalShell overrides which shell binary the interactive terminal
	// starts. Empty means use $SHELL, falling back to /bin/sh.
	TerminalShell        string
	ExtraAllowedPaths    []string
	Editor               string
	EditorMode           string
	IDEMode              string
	SmallModel           string
	SmallModelEnabled    bool
	RecapModel           string
	RecapModelEnabled    bool
	ExplorerModel        string
	ExplorerModelEnabled bool
	ContextModel         string
	ContextModelEnabled  bool
	AutoContinueEnabled  bool
	AutoContinueModel    string
	CommitMsgModel       string
	CommitMsgPrompt      string
	TUI                  TUIConfig
	MaxSteps             int `json:"max_steps,omitempty"`
	// MaxImageDim caps the longest edge (px) of an embedded image; larger
	// images are downscaled to fit, preserving aspect ratio. 0 means use the
	// agent package default (2000).
	MaxImageDim int `json:"image_max_dim,omitempty"`
	// RecapTimeoutSeconds controls the timeout for /recap summary generation.
	// Defaults to 120 if not configured.
	RecapTimeoutSeconds int `json:"recap_timeout_seconds,omitempty"`
	// UndoMaxAgeDelta is the number of agent step increments after which a
	// snapshot can no longer be undone by undo_file_change. Defaults to 4.
	UndoMaxAgeDelta int `json:"undo_max_age_delta,omitempty"`
	// MaxConcurrentAgents caps how many subagents (task tool dispatches) may
	// run at once; excess dispatches queue until a slot frees up. Defaults to
	// 2. 0 means unlimited.
	//
	// Access discipline (keep this invariant): the live limit is enforced by
	// AgentRunRegistry.SetMaxConcurrent / MaxConcurrent, which are
	// mutex-protected — that is the single source of truth at runtime. This
	// field is a plain int on purpose (JSON-serialized; atomic types would
	// break encoding/json), and its in-memory value is written ONLY from the
	// TUI event-loop goroutine in handleAgentsLimitCmd, which is queue-gated
	// while the agent is streaming — so a subagent's construction-time read of
	// this field (internal/agent/agent.go, SetMaxConcurrent) can never overlap
	// a write. Never read or write this field from a background goroutine;
	// route runtime limit changes through the registry instead.
	MaxConcurrentAgents int `json:"max_concurrent_agents"`

	// UploadDir overrides the default directory used by the /upload and
	// /api/uploads endpoints. When empty, files are stored under
	// <workDir>/.ocode/uploads.
	UploadDir string `json:"upload_dir,omitempty"`
	// BackendURL overrides the API backend the web frontend connects to.
	// Empty means same-origin (existing behavior). Allowed values are
	// empty, http://localhost[:port], or http://127.0.0.1[:port]
	// (optional trailing slash normalized away). Local dev origins only —
	// config/auth sync goes through the separate /api/sync/* flow instead.
	BackendURL string `json:"backend_url,omitempty"`
	// SyncURL overrides the config/auth sync server (kakiit) this machine
	// talks to for the /api/sync/* device-login + push/pull flow. Empty
	// falls back to OCODE_SYNC_URL, then the production default
	// (https://hub.mercstudio.com). Unlike BackendURL, this never affects
	// general API routing — only internal/sync's client.
	SyncURL string `json:"sync_url,omitempty"`
	// Ocr holds the OCR tool configuration (backend, model, endpoint).
	// Backend accepts openai-compat, paddle, and the lmstudio alias.
	Ocr      ocr.OcrConfig           `json:"ocr"`
	ImageGen ImageGenConfig          `json:"imagegen"`
	Profiles map[string]ProfileDelta `json:"profiles,omitempty"`
	Extra    map[string]json.RawMessage
}

const (
	DefaultTerminalScrollbackLines = 9999
	MinTerminalScrollbackLines     = 100
	MaxTerminalScrollbackLines     = 100000

	DefaultTerminalFontSize = 13
	MinTerminalFontSize     = 8
	MaxTerminalFontSize     = 32
)

// NormalizeTerminalFontSize applies the terminal font size default and bounds
// to values loaded from config or supplied by an API client.
func NormalizeTerminalFontSize(size int) int {
	if size <= 0 {
		return DefaultTerminalFontSize
	}
	if size < MinTerminalFontSize {
		return MinTerminalFontSize
	}
	if size > MaxTerminalFontSize {
		return MaxTerminalFontSize
	}
	return size
}

// NormalizeTerminalScrollbackLines applies the terminal scrollback default and
// bounds to values loaded from config or supplied by an API client.
func NormalizeTerminalScrollbackLines(lines int) int {
	if lines <= 0 {
		return DefaultTerminalScrollbackLines
	}
	if lines < MinTerminalScrollbackLines {
		return MinTerminalScrollbackLines
	}
	if lines > MaxTerminalScrollbackLines {
		return MaxTerminalScrollbackLines
	}
	return lines
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
	KeepRecentTokens      *int     `json:"keep_recent_tokens"`
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
	Enabled     *bool    `json:"enabled"`
	Provider    string   `json:"provider"`
	Model       string   `json:"model"`
	ClaudeCode  *bool    `json:"claude_code,omitempty"`
	Checkpoints []string `json:"checkpoints"`
}

type pluginsConfigFile struct {
	AST *bool `json:"ast"`
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

// browserConfigFile is the on-disk mirror of BrowserConfig. Extensions is read
// purely to detect (and reject) the reserved-but-unimplemented key.
type browserConfigFile struct {
	ChromePath         string          `json:"chrome_path,omitempty"`
	IdleTimeoutMinutes *int            `json:"idle_timeout_minutes,omitempty"`
	Extensions         json.RawMessage `json:"extensions,omitempty"`
}

type ocodeConfigFile struct {
	Compact                 compactConfigFile           `json:"compact"`
	Advisor                 advisorConfigFile           `json:"advisor"`
	Permissions             permissionConfigFile        `json:"permissions"`
	Plugins                 pluginsConfigFile           `json:"plugins"`
	Browser                 browserConfigFile           `json:"browser"`
	ExternalPlugins         map[string]PluginConfig     `json:"external_plugins,omitempty"`
	LocalModels             map[string]LocalModelConfig `json:"local_models,omitempty"`
	Security                securityConfigFile          `json:"security"`
	Discovery               discoveryConfigFile         `json:"discovery"`
	MemoryEnabled           *bool                       `json:"memory_enabled,omitempty"`
	DocPromptEnabled        *bool                       `json:"doc_prompt_enabled,omitempty"`
	ProfileDebug            *bool                       `json:"profile_debug,omitempty"`
	TerminalScrollbackLines *int                        `json:"terminal_scrollback_lines,omitempty"`
	TerminalFontFamily      string                      `json:"terminal_font_family,omitempty"`
	TerminalFontSize        *int                        `json:"terminal_font_size,omitempty"`
	TerminalShell           string                      `json:"terminal_shell,omitempty"`
	ExtraAllowedPaths       []string                    `json:"extra_allowed_paths,omitempty"`
	Editor                  string                      `json:"editor,omitempty"`
	EditorMode              string                      `json:"editor_mode,omitempty"`
	IDEMode                 string                      `json:"ide_mode,omitempty"`
	SmallModel              string                      `json:"small_model,omitempty"`
	SmallModelEnabled       *bool                       `json:"small_model_enabled,omitempty"`
	RecapModel              string                      `json:"recap_model,omitempty"`
	RecapModelEnabled       *bool                       `json:"recap_model_enabled,omitempty"`
	ExplorerModel           string                      `json:"explorer_model,omitempty"`
	ExplorerModelEnabled    *bool                       `json:"explorer_model_enabled,omitempty"`
	ContextModel            string                      `json:"context_model,omitempty"`
	ContextModelEnabled     *bool                       `json:"context_model_enabled,omitempty"`
	AutoContinueEnabled     *bool                       `json:"auto_continue_enabled,omitempty"`
	AutoContinueModel       string                      `json:"auto_continue_model,omitempty"`
	RecapTimeoutSeconds     *int                        `json:"recap_timeout_seconds,omitempty"`
	UndoMaxAgeDelta         *int                        `json:"undo_max_age_delta,omitempty"`
	MaxConcurrentAgents     *int                        `json:"max_concurrent_agents,omitempty"`
	CommitMsgModel          string                      `json:"commit_msg_model,omitempty"`
	CommitMsgPrompt         string                      `json:"commit_msg_prompt,omitempty"`
	TUI                     tuiConfigFile               `json:"tui"`
	MaxSteps                int                         `json:"max_steps,omitempty"`
	MaxImageDim             int                         `json:"image_max_dim,omitempty"`
	UploadDir               string                      `json:"upload_dir,omitempty"`
	BackendURL              string                      `json:"backend_url,omitempty"`
	SyncURL                 string                      `json:"sync_url,omitempty"`
	Ocr                     *ocr.OcrConfig              `json:"ocr,omitempty"`
	ImageGen                *ImageGenConfig             `json:"imagegen,omitempty"`
	Profiles                map[string]ProfileDelta     `json:"profiles,omitempty"`
	// Legacy fields (read from old configs for migration)
	OcrModel   string `json:"ocr_model,omitempty"`
	OcrEnabled *bool  `json:"ocr_enabled,omitempty"`
}

func defaultCompactConfig() CompactConfig {
	return CompactConfig{
		Enabled:               true,
		TokenThreshold:        0.85,
		KeepRecentTurns:       3,
		MinMessages:           8,
		SummaryTimeoutSeconds: 600,
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
		Compact:                 defaultCompactConfig(),
		Advisor:                 defaultAdvisorConfig(),
		Permissions:             defaultPermissionConfig(),
		Browser:                 BrowserConfig{IdleTimeoutMinutes: 10},
		MemoryEnabled:           true,
		SmallModelEnabled:       true,
		RecapModelEnabled:       false,
		ExplorerModelEnabled:    false,
		ContextModelEnabled:     false,
		Security:                defaultSecurityConfig(),
		Discovery:               defaultDiscoveryConfig(),
		RecapTimeoutSeconds:     120,
		UndoMaxAgeDelta:         10,
		MaxConcurrentAgents:     2,
		TerminalScrollbackLines: DefaultTerminalScrollbackLines,
		TUI:                     defaultTUIConfig(),
		Ocr:                     ocr.DefaultOcrConfig(),
		Extra:                   make(map[string]json.RawMessage),
		ImageGen:                DefaultImageGenConfig(),
	}
}

func defaultDiscoveryConfig() DiscoveryConfig {
	return DiscoveryConfig{
		Enabled:          false,
		EmbeddingModel:   "",
		EmbeddingBackend: "local",
		LocalModelStatus: "none",
		PinnedSkills:     []string{"brainstorming", "using-superpowers"},
		IgnorePaths:      DefaultDiscoveryIgnorePaths(),
	}
}

// DefaultDiscoveryIgnorePaths returns the built-in discovery ignore list.
// Callers receive a copy and may mutate it freely.
func DefaultDiscoveryIgnorePaths() []string {
	return []string{
		"skills/",
		".opencode/",
		".claude/",
		".qwen/",
		".agent/",
		".pnpm/",
		"node_modules/",
		"vendor/",
		".git/",
		"dist/",
		"build/",
		"target/",
	}
}

func defaultAdvisorConfig() AdvisorConfig {
	return AdvisorConfig{
		Enabled:  true,
		Provider: "deepseek",
		Model:    "deepseek-v4-pro",
		// Both on by default: "plan" vets the first write batch before any
		// mutation, "done" verifies completion claims. Set "checkpoints": []
		// in ocode.json to disable.
		Checkpoints: []string{"plan", "done"},
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

	// Load project-level settings from .ocode/settings.json.
	// These supplement (not replace) the global config; project settings are
	// optional — the file may not exist, and that is not an error.
	if settingsPath := getProjectSettingsPath(); settingsPath != "" {
		extraPaths, pErr := loadProjectSettings(settingsPath)
		if pErr != nil {
			return fmt.Errorf("load project settings: %w", pErr)
		}
		if len(extraPaths) > 0 {
			ocode.ExtraAllowedPaths = append(ocode.ExtraAllowedPaths, extraPaths...)
		}
	}

	if ocode.EditorMode == "" {
		if os.Getenv("TMUX") != "" {
			ocode.EditorMode = EditorModeTmuxSplit
		} else {
			ocode.EditorMode = EditorModeExternal
		}
	}

	// External plugins (source/dir/ref/enabled packages like "orchestrator")
	// are owned by ocodeconfig.json, not opencode.json — cfg.Plugins is
	// populated exclusively from here (see loadFromFile in config.go, which
	// intentionally skips opencode.json's "plugins" key).
	cfg.Plugins = ocode.ExternalPlugins
	if cfg.Plugins == nil {
		cfg.Plugins = make(map[string]PluginConfig)
	}

	cfg.Ocode = ocode
	return nil
}

// loadProjectSettings loads extra_allowed_paths from .ocode/settings.json.
// Returns an empty slice if the file does not exist.
// Uses map[string]json.RawMessage to preserve unknown fields.
func loadProjectSettings(path string) ([]string, error) {
	if path == "" {
		return []string{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil // File doesn't exist, not an error
		}
		return nil, fmt.Errorf("read project settings: %w", err)
	}

	// Decode into generic map to preserve unknown keys
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse project settings: %w", err)
	}

	// Extract extra_allowed_paths if present
	var paths []string
	if pathsRaw, ok := raw["extra_allowed_paths"]; ok {
		if err := json.Unmarshal(pathsRaw, &paths); err != nil {
			return nil, fmt.Errorf("parse extra_allowed_paths: %w", err)
		}
	}

	return paths, nil
}

// saveProjectSettings persists extra_allowed_paths to .ocode/settings.json.
// Uses key-preserving merge (map[string]json.RawMessage) to avoid data loss.
// Creates .ocode directory and file if they don't exist.
func saveProjectSettings(path string, paths []string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}

	// Ensure .ocode directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create .ocode directory: %w", err)
	}

	// Load existing file (if it exists) to preserve unknown fields
	var raw map[string]json.RawMessage
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parse existing settings: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read settings file: %w", err)
	} else {
		raw = make(map[string]json.RawMessage)
	}

	// Update only extra_allowed_paths
	pathsData, err := json.Marshal(paths)
	if err != nil {
		return fmt.Errorf("marshal paths: %w", err)
	}
	raw["extra_allowed_paths"] = pathsData

	// Write back
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write settings file: %w", err)
	}

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

	if _, ok := raw["external_plugins"]; ok {
		if cfg.ExternalPlugins == nil {
			cfg.ExternalPlugins = make(map[string]PluginConfig, len(file.ExternalPlugins))
		}
		for name, p := range file.ExternalPlugins {
			cfg.ExternalPlugins[name] = p
		}
		delete(raw, "external_plugins")
	}

	if _, ok := raw["local_models"]; ok {
		if cfg.LocalModels == nil {
			cfg.LocalModels = make(map[string]LocalModelConfig, len(file.LocalModels))
		}
		for id, lm := range file.LocalModels {
			cfg.LocalModels[id] = lm
		}
		delete(raw, "local_models")
	}

	if _, ok := raw["security"]; ok {
		applySecurityConfig(&cfg.Security, file.Security)
		delete(raw, "security")
	}

	if _, ok := raw["discovery"]; ok {
		applyDiscoveryConfig(&cfg.Discovery, file.Discovery)
		delete(raw, "discovery")
	}

	if _, ok := raw["browser"]; ok {
		if err := applyBrowserConfig(&cfg.Browser, file.Browser); err != nil {
			return fmt.Errorf("browser: %w", err)
		}
		delete(raw, "browser")
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

	if _, ok := raw["recap_model"]; ok {
		if file.RecapModel != "" {
			cfg.RecapModel = file.RecapModel
		}
		delete(raw, "recap_model")
	}
	if _, ok := raw["recap_model_enabled"]; ok {
		if file.RecapModelEnabled != nil {
			cfg.RecapModelEnabled = *file.RecapModelEnabled
		}
		delete(raw, "recap_model_enabled")
	}

	if _, ok := raw["explorer_model"]; ok {
		if file.ExplorerModel != "" {
			cfg.ExplorerModel = file.ExplorerModel
		}
		delete(raw, "explorer_model")
	}
	if _, ok := raw["explorer_model_enabled"]; ok {
		if file.ExplorerModelEnabled != nil {
			cfg.ExplorerModelEnabled = *file.ExplorerModelEnabled
		}
		delete(raw, "explorer_model_enabled")
	}

	if _, ok := raw["context_model"]; ok {
		if file.ContextModel != "" {
			cfg.ContextModel = file.ContextModel
		}
		delete(raw, "context_model")
	}
	if _, ok := raw["context_model_enabled"]; ok {
		if file.ContextModelEnabled != nil {
			cfg.ContextModelEnabled = *file.ContextModelEnabled
		}
		delete(raw, "context_model_enabled")
	}

	if _, ok := raw["auto_continue_enabled"]; ok {
		if file.AutoContinueEnabled != nil {
			cfg.AutoContinueEnabled = *file.AutoContinueEnabled
		}
		delete(raw, "auto_continue_enabled")
	}
	if _, ok := raw["auto_continue_model"]; ok {
		if file.AutoContinueModel != "" {
			cfg.AutoContinueModel = file.AutoContinueModel
		}
		delete(raw, "auto_continue_model")
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

	if _, ok := raw["undo_max_age_delta"]; ok {
		if file.UndoMaxAgeDelta != nil && *file.UndoMaxAgeDelta > 0 {
			cfg.UndoMaxAgeDelta = *file.UndoMaxAgeDelta
		}
		delete(raw, "undo_max_age_delta")
	}

	if _, ok := raw["max_concurrent_agents"]; ok {
		// Unlike most int settings, 0 is a valid explicit value here (means
		// unlimited), so apply whenever the key is present rather than
		// gating on > 0.
		if file.MaxConcurrentAgents != nil {
			cfg.MaxConcurrentAgents = *file.MaxConcurrentAgents
		}
		delete(raw, "max_concurrent_agents")
	}

	if _, ok := raw["memory_enabled"]; ok {
		if file.MemoryEnabled != nil {
			cfg.MemoryEnabled = *file.MemoryEnabled
		}
		delete(raw, "memory_enabled")
	}

	if _, ok := raw["terminal_scrollback_lines"]; ok {
		if file.TerminalScrollbackLines != nil {
			cfg.TerminalScrollbackLines = NormalizeTerminalScrollbackLines(*file.TerminalScrollbackLines)
		}
		delete(raw, "terminal_scrollback_lines")
	}

	if _, ok := raw["terminal_font_family"]; ok {
		cfg.TerminalFontFamily = file.TerminalFontFamily
		delete(raw, "terminal_font_family")
	}

	if _, ok := raw["terminal_font_size"]; ok {
		if file.TerminalFontSize != nil {
			cfg.TerminalFontSize = NormalizeTerminalFontSize(*file.TerminalFontSize)
		}
		delete(raw, "terminal_font_size")
	}

	if _, ok := raw["terminal_shell"]; ok {
		cfg.TerminalShell = file.TerminalShell
		delete(raw, "terminal_shell")
	}

	if _, ok := raw["doc_prompt_enabled"]; ok {
		if file.DocPromptEnabled != nil {
			cfg.DocPromptEnabled = *file.DocPromptEnabled
		}
		delete(raw, "doc_prompt_enabled")
	}

	if _, ok := raw["profile_debug"]; ok {
		if file.ProfileDebug != nil {
			cfg.ProfileDebug = *file.ProfileDebug
		}
		delete(raw, "profile_debug")
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

	if _, ok := raw["backend_url"]; ok {
		if normalized, err := NormalizeBackendURL(file.BackendURL); err == nil {
			cfg.BackendURL = normalized
			delete(raw, "backend_url")
		} else {
			// Normalization failed: leave the raw json.RawMessage in `raw`
			// so it flows through to cfg.Extra and a later save round-trips
			// it intact instead of silently dropping the user's value.
			// Surface the failure via log for field diagnosis.
			log.Printf("config: invalid backend_url %q on disk, preserving raw value: %v", file.BackendURL, err)
		}
	}

	if _, ok := raw["sync_url"]; ok {
		if normalized, err := NormalizeSyncURL(file.SyncURL); err == nil {
			cfg.SyncURL = normalized
			delete(raw, "sync_url")
		} else {
			// Leave raw in place so the next save round-trips it.
			log.Printf("config: invalid sync_url %q on disk, preserving raw value: %v", file.SyncURL, err)
		}
	}

	if rawOcr, ok := raw["ocr"]; ok && rawOcr != nil {
		var ocrCfg ocr.OcrConfig
		if data, err := json.Marshal(rawOcr); err == nil {
			if json.Unmarshal(data, &ocrCfg) == nil {
				cfg.Ocr = ocrCfg
			}
		}
		delete(raw, "ocr")
	} else {
		// Legacy migration: ocr_model + ocr_enabled
		if _, ok := raw["ocr_model"]; ok {
			if file.Ocr != nil {
				cfg.Ocr.OpenAI.Model = file.Ocr.OpenAI.Model
			} else if file.OcrModel != "" {
				cfg.Ocr.OpenAI.Model = file.OcrModel
			}
			delete(raw, "ocr_model")
		}
		if _, ok := raw["ocr_enabled"]; ok {
			if file.Ocr != nil {
				cfg.Ocr.Enabled = file.Ocr.Enabled
			} else if file.OcrEnabled != nil {
				cfg.Ocr.Enabled = *file.OcrEnabled
			}
			delete(raw, "ocr_enabled")
		}
	}

	if rawImg, ok := raw["imagegen"]; ok && rawImg != nil {
		var imgCfg ImageGenConfig
		if data, err := json.Marshal(rawImg); err == nil {
			if json.Unmarshal(data, &imgCfg) == nil {
				cfg.ImageGen = imgCfg
			}
		}
		delete(raw, "imagegen")
	}

	if _, ok := raw["profiles"]; ok {
		if len(file.Profiles) > 0 {
			if cfg.Profiles == nil {
				cfg.Profiles = make(map[string]ProfileDelta, len(file.Profiles))
			}
			for name, delta := range file.Profiles {
				if err := ValidateProfileName(name); err != nil {
					continue
				}
				cfg.Profiles[name] = delta
			}
		} else {
			// Preserve empty map if explicitly set to {} to distinguish from absent.
			if rawProfiles, err := json.Marshal(raw["profiles"]); err == nil && string(rawProfiles) == "{}" {
				if cfg.Profiles == nil {
					cfg.Profiles = make(map[string]ProfileDelta)
				}
			}
		}
		delete(raw, "profiles")
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
	// nil = key absent, keep default; [] = explicitly disable all checkpoints.
	if src.Checkpoints != nil {
		dst.Checkpoints = src.Checkpoints
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
	if src.KeepRecentTokens != nil {
		dst.KeepRecentTokens = *src.KeepRecentTokens
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
		dst.IgnorePaths = mergeDiscoveryIgnorePaths(DefaultDiscoveryIgnorePaths(), src.IgnorePaths)
	}
}

func applyBrowserConfig(dst *BrowserConfig, src browserConfigFile) error {
	if len(src.Extensions) != 0 {
		return fmt.Errorf("browser.extensions is not supported yet")
	}
	if src.ChromePath != "" {
		dst.ChromePath = src.ChromePath
	}
	if src.IdleTimeoutMinutes != nil {
		if *src.IdleTimeoutMinutes < 0 {
			return fmt.Errorf("browser.idle_timeout_minutes must be >= 0, got %d", *src.IdleTimeoutMinutes)
		}
		// Explicit 0 keeps the default (10) in dst — it does not mean "reap
		// immediately".
		if *src.IdleTimeoutMinutes > 0 {
			dst.IdleTimeoutMinutes = *src.IdleTimeoutMinutes
		}
	}
	return nil
}

func mergeDiscoveryIgnorePaths(base, extra []string) []string {
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	add := func(paths []string) {
		for _, p := range paths {
			if p == "" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	add(base)
	add(extra)
	return out
}

func SaveOcodeConfig(cfg *OcodeConfig) error {
	path, err := ActiveOcodeConfigPath()
	if err != nil {
		return err
	}
	return writeOcodeConfigFile(path, cfg)
}

// lockOcodeConfig acquires a cross-process advisory lock on ocodeconfig.json
// via an exclusive-create lock file, so a read-modify-write from one ocode
// session can't interleave with another's and silently drop its change.
// Returns an unlock func; on timeout it gives up waiting and returns a no-op
// unlock rather than hanging the caller indefinitely.
func lockOcodeConfig() (func(), error) {
	path, err := ActiveOcodeConfigPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	lockPath := path + ".lock"
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			f.Close()
			return func() { os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		// A crashed process can leave the lock file behind forever; steal it
		// once it's clearly stale rather than deadlocking every future save.
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 10*time.Second {
			os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return func() {}, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// withOcodeConfigLock loads the current config under lockOcodeConfig, lets fn
// mutate it, and writes it back before releasing — the standard shape for
// every Save* setter below. This closes the load-modify-write race that a
// OnConfigSaved is invoked after every successful ocodeconfig.json file
// write. Clients register callbacks with OnConfigSaved.Add; the zero value
// is ready to use. Used by internal/sync to debounce background push.
var OnConfigSaved hook.Hooks

// bare loadFullOcodeConfig()+SaveOcodeConfig() pair leaves open between two
// concurrent ocode sessions.
func withOcodeConfigLock(fn func(cfg *OcodeConfig) error) error {
	unlock, err := lockOcodeConfig()
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	if err := fn(cfg); err != nil {
		return err
	}
	if err := SaveOcodeConfig(cfg); err != nil {
		return err
	}
	OnConfigSaved.Fire()
	return nil
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
		"browser":     cfg.Browser,
	}
	if cfg.Plugins.AST {
		payload["plugins"] = cfg.Plugins
	}
	if len(cfg.ExternalPlugins) > 0 {
		payload["external_plugins"] = cfg.ExternalPlugins
	}
	if len(cfg.LocalModels) > 0 {
		payload["local_models"] = cfg.LocalModels
	}
	if len(cfg.ExtraAllowedPaths) > 0 {
		seen := make(map[string]struct{}, len(cfg.ExtraAllowedPaths))
		deduped := cfg.ExtraAllowedPaths[:0:0]
		for _, p := range cfg.ExtraAllowedPaths {
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				deduped = append(deduped, p)
			}
		}
		payload["extra_allowed_paths"] = deduped
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
	if cfg.RecapModel != "" {
		payload["recap_model"] = cfg.RecapModel
	}
	payload["recap_model_enabled"] = cfg.RecapModelEnabled
	if cfg.ExplorerModel != "" {
		payload["explorer_model"] = cfg.ExplorerModel
	}
	payload["explorer_model_enabled"] = cfg.ExplorerModelEnabled
	if cfg.ContextModel != "" {
		payload["context_model"] = cfg.ContextModel
	}
	payload["context_model_enabled"] = cfg.ContextModelEnabled
	payload["auto_continue_enabled"] = cfg.AutoContinueEnabled
	if cfg.AutoContinueModel != "" {
		payload["auto_continue_model"] = cfg.AutoContinueModel
	}
	if cfg.RecapTimeoutSeconds > 0 {
		payload["recap_timeout_seconds"] = cfg.RecapTimeoutSeconds
	}
	if cfg.CommitMsgModel != "" {
		payload["commit_msg_model"] = cfg.CommitMsgModel
	}
	if cfg.CommitMsgPrompt != "" {
		payload["commit_msg_prompt"] = cfg.CommitMsgPrompt
	}
	payload["memory_enabled"] = cfg.MemoryEnabled
	payload["doc_prompt_enabled"] = cfg.DocPromptEnabled
	payload["profile_debug"] = cfg.ProfileDebug
	payload["terminal_scrollback_lines"] = NormalizeTerminalScrollbackLines(cfg.TerminalScrollbackLines)
	if cfg.TerminalFontFamily != "" {
		payload["terminal_font_family"] = cfg.TerminalFontFamily
	}
	if cfg.TerminalFontSize > 0 {
		payload["terminal_font_size"] = NormalizeTerminalFontSize(cfg.TerminalFontSize)
	}
	if cfg.TerminalShell != "" {
		payload["terminal_shell"] = cfg.TerminalShell
	}
	if cfg.MaxSteps > 0 {
		payload["max_steps"] = cfg.MaxSteps
	}
	if cfg.MaxImageDim > 0 {
		payload["image_max_dim"] = cfg.MaxImageDim
	}
	if cfg.UndoMaxAgeDelta > 0 {
		payload["undo_max_age_delta"] = cfg.UndoMaxAgeDelta
	}
	// Always written (unlike the other max_* settings): 0 is a meaningful
	// explicit value (unlimited), not "unset", so it must round-trip.
	payload["max_concurrent_agents"] = cfg.MaxConcurrentAgents
	if cfg.UploadDir != "" {
		payload["upload_dir"] = cfg.UploadDir
	}
	if len(cfg.Profiles) > 0 {
		payload["profiles"] = cfg.Profiles
	}
	payload["ocr"] = cfg.Ocr
	payload["imagegen"] = cfg.ImageGen
	if cfg.TUI.Theme != "" || cfg.TUI.Mouse != nil || cfg.TUI.Scroll != 0 || cfg.TUI.LeaderTimeout != 0 || len(cfg.TUI.Keybinds) > 0 {
		payload["tui"] = cfg.TUI
	}
	for k, v := range cfg.Extra {
		// Canonical keys are set either by the Extra loop (preserving raw
		// on-disk values that failed normalization) or overridden afterward
		// by the canonical setters below when a valid normalized value exists.
		if k == "compact" || k == "advisor" || k == "permissions" || k == "plugins" || k == "external_plugins" || k == "local_models" || k == "extra_allowed_paths" || k == "max_steps" || k == "discovery" || k == "recap_model" || k == "recap_model_enabled" || k == "auto_continue_enabled" || k == "auto_continue_model" || k == "ocr" || k == "terminal_enabled" || k == "terminal_scrollback_lines" || k == "terminal_font_family" || k == "terminal_font_size" || k == "terminal_shell" || k == "profiles" || k == "profile_debug" {
			continue
		}
		payload[k] = v
	}
	// Canonical backend_url/sync_url setters run AFTER the Extra loop so a
	// valid normalized value always wins over a preserved raw value from
	// Extra; when empty (normalization failed), the Extra value passes
	// through and round-trips intact.
	if cfg.BackendURL != "" {
		payload["backend_url"] = cfg.BackendURL
	}
	if cfg.SyncURL != "" {
		payload["sync_url"] = cfg.SyncURL
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

// Profile CRUD — all use withOcodeConfigLock so concurrent sessions cannot drop each other's changes.

func SaveProfile(name string, delta ProfileDelta) error {
	if err := ValidateProfileName(name); err != nil {
		return err
	}
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if cfg.Profiles == nil {
			cfg.Profiles = make(map[string]ProfileDelta)
		}
		cfg.Profiles[name] = delta
		return nil
	})
}

func DeleteProfile(name string) error {
	if err := ValidateProfileName(name); err != nil {
		return err
	}
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if cfg.Profiles == nil {
			return nil
		}
		delete(cfg.Profiles, name)
		if len(cfg.Profiles) == 0 {
			cfg.Profiles = nil
		}
		return nil
	})
}

func RenameProfile(oldName, newName string) error {
	if err := ValidateProfileName(oldName); err != nil {
		return err
	}
	if err := ValidateProfileName(newName); err != nil {
		return err
	}
	if oldName == newName {
		return nil
	}
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if cfg.Profiles == nil {
			return fmt.Errorf("profile %q not found", oldName)
		}
		delta, ok := cfg.Profiles[oldName]
		if !ok {
			return fmt.Errorf("profile %q not found", oldName)
		}
		if _, exists := cfg.Profiles[newName]; exists {
			return fmt.Errorf("profile %q already exists", newName)
		}
		cfg.Profiles[newName] = delta
		delete(cfg.Profiles, oldName)
		return nil
	})
}

func GetProfile(name string) (ProfileDelta, bool) {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return ProfileDelta{}, false
	}
	if cfg.Profiles == nil {
		return ProfileDelta{}, false
	}
	delta, ok := cfg.Profiles[name]
	return delta, ok
}

func LoadOcodeConfigCopy() (*OcodeConfig, error) {
	return loadFullOcodeConfig()
}

func ListProfiles() []string {
	cfg, err := loadFullOcodeConfig()
	if err != nil || cfg.Profiles == nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Profiles))
	for n := range cfg.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func GetProfileProviderAPIKey(profile, providerID string) string {
	cfg, err := loadFullOcodeConfig()
	if err != nil || cfg.Profiles == nil {
		return ""
	}
	delta, ok := cfg.Profiles[profile]
	if !ok || delta.Provider == nil {
		return ""
	}
	raw, ok := delta.Provider[providerID]
	if !ok {
		return ""
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return ""
	}
	opts, ok := m["options"].(map[string]interface{})
	if !ok {
		return ""
	}
	apiKeyVal, ok := opts["apiKey"]
	if !ok {
		return ""
	}
	rawKey, ok := apiKeyVal.(string)
	if !ok {
		return ""
	}
	if strings.HasPrefix(rawKey, "{env:") && strings.HasSuffix(rawKey, "}") {
		envVar := strings.TrimSuffix(strings.TrimPrefix(rawKey, "{env:"), "}")
		return os.Getenv(envVar)
	}
	return rawKey
}

func SaveOcodePermissions(permissions PermissionConfig) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
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
		return nil
	})
}

// SaveMaxSteps persists the max steps setting to the ocode config.
// 0 or negative clears the override (unlimited, default cap of 100 applies).
func SaveMaxSteps(n int) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.MaxSteps = n
		return nil
	})
}

// SaveOcodeRecapConfig persists the /recap model selection and timeout.
func SaveOcodeRecapConfig(model string, enabled bool, timeoutSeconds int) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.RecapModel = model
		cfg.RecapModelEnabled = enabled
		cfg.RecapTimeoutSeconds = timeoutSeconds
		return nil
	})
}

// SaveMaxConcurrentAgents persists the max-concurrent-subagents limit.
// n <= 0 is stored as 0, meaning unlimited.
func SaveMaxConcurrentAgents(n int) error {
	if n < 0 {
		n = 0
	}
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.MaxConcurrentAgents = n
		return nil
	})
}

// SaveAutoPermissionEnabled persists only the auto-permission `enabled` flag
// using load-modify-write, so it cannot clobber a concurrent session's
// model/grants/tool rules the way a wholesale config write would.
func SaveAutoPermissionEnabled(enabled bool) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if cfg.Permissions.Auto == nil {
			cfg.Permissions.Auto = &AutoPermissionConfig{}
		}
		cfg.Permissions.Auto.Enabled = enabled
		return nil
	})
}

// SaveExtraAllowedPath appends one cleaned path to extra_allowed_paths.
// If in a project, persists to .ocode/settings.json; otherwise to global config.
// Deduplicates before saving (skips if path already present).
func SaveExtraAllowedPath(path string) error {
	cleaned := filepath.Clean(path)

	// Try to save to project settings first
	projectSettingsPath := getProjectSettingsPath()
	if projectSettingsPath != "" {
		projectPaths, err := loadProjectSettings(projectSettingsPath)
		if err != nil {
			return fmt.Errorf("load project settings: %w", err)
		}

		// Deduplicate
		for _, p := range projectPaths {
			if p == cleaned {
				return nil // Already present
			}
		}

		// Append and save to project config
		projectPaths = append(projectPaths, cleaned)
		return saveProjectSettings(projectSettingsPath, projectPaths)
	}

	// Fall back to global config
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		// Deduplicate; re-saving unchanged content when already present is
		// harmless (identical bytes on disk), so no special-case skip is needed.
		for _, p := range cfg.ExtraAllowedPaths {
			if filepath.Clean(p) == cleaned {
				return nil
			}
		}
		cfg.ExtraAllowedPaths = append(cfg.ExtraAllowedPaths, cleaned)
		return nil
	})
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
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
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
		return nil
	})
}

func SaveEditor(editor string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.Editor = editor
		return nil
	})
}

// SaveUploadDir persists only the upload_dir field using load-modify-write so
// it cannot clobber a concurrent session's other config.
func SaveUploadDir(dir string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.UploadDir = dir
		return nil
	})
}

// hubBackendURL is the legacy production backend host that used to be an
// accepted value for backend_url. It is now scoped to the dedicated sync
// channel (sync_url / internal/sync) instead. NormalizeBackendURL returns
// ErrBackendURLIsHub when it sees this value so callers (API handlers, tests)
// can detect the migration case and emit a useful "use sync_url instead"
// message rather than a generic "invalid URL" error.
const hubBackendURL = "https://hub.mercstudio.com"

// ErrBackendURLIsHub is returned by NormalizeBackendURL when the caller
// supplies the legacy production hub URL (https://hub.mercstudio.com). That
// host is no longer valid for backend_url — it belongs in sync_url. Callers
// can errors.Is this to surface a migration hint.
var ErrBackendURLIsHub = errors.New("backend_url no longer accepts " + hubBackendURL + "; use sync_url instead")

// NormalizeBackendURL validates and normalizes a backend URL. Allowed values
// are empty (same-origin), http://localhost[:port], or http://127.0.0.1[:port]
// (optional trailing slash normalized away). backend_url redirects every
// /api/* call the frontend makes, so it must stay scoped to local dev
// origins — it is not the config/auth sync channel (that's the dedicated
// /api/sync/* flow in internal/sync, which talks to kakiit directly and
// never touches this setting).
// It rejects credentials, paths, query strings, fragments, wrong schemes,
// host-subdomain tricks, and malformed ports.
func NormalizeBackendURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	// Trim single trailing slash for normalization (but keep root slash handling below).
	if strings.HasSuffix(trimmed, "/") && trimmed != "/" {
		trimmed = strings.TrimSuffix(trimmed, "/")
	}
	// Use net/url parsing for localhost variants.
	u, err := parseBackendURL(trimmed)
	if err != nil {
		return "", err
	}
	// Only http scheme for localhost.
	if u.Scheme != "http" {
		// Legacy migration: the production hub host used to be a valid
		// backend_url. It now belongs in sync_url. Return a typed sentinel
		// so callers can emit a migration hint instead of a generic error.
		if u.Scheme == "https" && u.Hostname() == "hub.mercstudio.com" && (u.Path == "" || u.Path == "/") {
			return "", ErrBackendURLIsHub
		}
		return "", fmt.Errorf("backend_url must be http for localhost, got %q", u.Scheme)
	}
	// Reject userinfo.
	if u.User != nil {
		return "", fmt.Errorf("backend_url must not contain credentials")
	}
	// Reject path, query, fragment.
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("backend_url must not contain a path")
	}
	if u.RawQuery != "" {
		return "", fmt.Errorf("backend_url must not contain a query string")
	}
	if u.Fragment != "" {
		return "", fmt.Errorf("backend_url must not contain a fragment")
	}
	// Host must be exactly localhost or 127.0.0.1 with optional port.
	hostname := u.Hostname()
	port := u.Port()
	if hostname != "localhost" && hostname != "127.0.0.1" {
		return "", fmt.Errorf("backend_url host must be localhost or 127.0.0.1")
	}
	// Validate port if present.
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("backend_url has invalid port %q", port)
		}
	}
	// Reconstruct normalized URL without path.
	if port != "" {
		return fmt.Sprintf("http://%s:%s", hostname, port), nil
	}
	return fmt.Sprintf("http://%s", hostname), nil
}

func parseBackendURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid backend_url %q: %w", raw, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("backend_url must be an absolute URL, got %q", raw)
	}
	if strings.Contains(raw, "@") {
		return nil, fmt.Errorf("backend_url must not contain credentials")
	}
	return u, nil
}

// ValidateBackendURL is a convenience wrapper that only checks validity.
func ValidateBackendURL(raw string) error {
	_, err := NormalizeBackendURL(raw)
	return err
}

// SaveBackendURL persists the backend_url field after validation/normalization.
func SaveBackendURL(raw string) error {
	normalized, err := NormalizeBackendURL(raw)
	if err != nil {
		return err
	}
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.BackendURL = normalized
		return nil
	})
}

// NormalizeSyncURL validates and normalizes the config/auth sync server URL
// (internal/sync's kakiit client). Unlike backend_url, this is meant to
// reach a real external host, so any https:// origin is allowed, plus
// http://localhost[:port] and http://127.0.0.1[:port] for local kakiit dev.
// Empty means "use the default" (OCODE_SYNC_URL env, then the production
// hub — see internal/sync.DefaultBaseURL). Rejects credentials, paths,
// query strings, and fragments — the sync client appends its own
// /api/ocode/... paths onto whatever origin this resolves to.
func NormalizeSyncURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if strings.HasSuffix(trimmed, "/") && trimmed != "/" {
		trimmed = strings.TrimSuffix(trimmed, "/")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid sync_url %q: %w", trimmed, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("sync_url must be an absolute URL, got %q", trimmed)
	}
	if u.User != nil || strings.Contains(trimmed, "@") {
		return "", fmt.Errorf("sync_url must not contain credentials")
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("sync_url must not contain a path")
	}
	if u.RawQuery != "" {
		return "", fmt.Errorf("sync_url must not contain a query string")
	}
	if u.Fragment != "" {
		return "", fmt.Errorf("sync_url must not contain a fragment")
	}
	hostname := strings.ToLower(u.Hostname())
	switch u.Scheme {
	case "https":
		// Any host is allowed over https — this is expected to reach a real
		// external kakiit deployment, not just localhost.
	case "http":
		if hostname != "localhost" && hostname != "127.0.0.1" {
			return "", fmt.Errorf("sync_url must be https, except http://localhost or http://127.0.0.1 for local dev")
		}
	default:
		return "", fmt.Errorf("sync_url scheme must be http or https, got %q", u.Scheme)
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("sync_url has invalid port %q", port)
		}
		return fmt.Sprintf("%s://%s:%s", u.Scheme, hostname, port), nil
	}
	return fmt.Sprintf("%s://%s", u.Scheme, hostname), nil
}

// ValidateSyncURL is a convenience wrapper that only checks validity.
func ValidateSyncURL(raw string) error {
	_, err := NormalizeSyncURL(raw)
	return err
}

// SaveSyncURL persists the sync_url field after validation/normalization,
// using load-modify-write so it cannot clobber a concurrent session's other
// config (see withOcodeConfigLock).
func SaveSyncURL(raw string) error {
	normalized, err := NormalizeSyncURL(raw)
	if err != nil {
		return err
	}
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.SyncURL = normalized
		return nil
	})
}

// SaveDiscoveryEnabled persists only the discovery.enabled flag using
// load-modify-write so it cannot clobber a concurrent session's other config.
func SaveDiscoveryEnabled(enabled bool) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.Discovery.Enabled = enabled
		return nil
	})
}

// SaveQueryEmbeddingModel persists the discovery embedding model + backend.
// An empty backend preserves the existing on-disk value.
func SaveQueryEmbeddingModel(modelID, backend string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.Discovery.EmbeddingModel = modelID
		if backend != "" {
			cfg.Discovery.EmbeddingBackend = backend
		}
		return nil
	})
}

// SaveDiscoveryIgnorePaths persists the discovery ignore-paths list.
func SaveDiscoveryIgnorePaths(paths []string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.Discovery.IgnorePaths = paths
		return nil
	})
}

// SaveLocalModelStatus persists the local model download status.
func SaveLocalModelStatus(status string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.Discovery.LocalModelStatus = status
		return nil
	})
}

// SaveLocalModelConfig persists (creating or updating) one registered local
// chat model's enabled flag, concurrent-slot limit, and context-size cap,
// using load-modify-write so it cannot clobber a concurrent session's other
// config.
func SaveLocalModelConfig(modelID string, enabled bool, maxParallel, contextSize int) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if cfg.LocalModels == nil {
			cfg.LocalModels = map[string]LocalModelConfig{}
		}
		cfg.LocalModels[modelID] = LocalModelConfig{Enabled: enabled, MaxParallel: maxParallel, ContextSize: contextSize}
		return nil
	})
}

// DeleteLocalModelConfig removes a registered local chat model entirely
// (distinct from disabling it — this forgets the MaxParallel setting too).
func DeleteLocalModelConfig(modelID string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		delete(cfg.LocalModels, modelID)
		return nil
	})
}

// RegisteredLocalModelIDs returns the sorted list of all registered local
// chat model ids, loaded FRESH from disk rather than any in-memory config
// cache. discovery.AssignChatPort's determinism guarantee ("multiple ocode
// processes agree on the same port without a shared allocation file") only
// holds if every process computes the sort over the same registered-id set
// at assignment time — sourcing from a session's possibly-stale in-memory
// config could make two processes compute different ports for the same
// model id.
func RegisteredLocalModelIDs() ([]string, error) {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(cfg.LocalModels))
	for id := range cfg.LocalModels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// LocalModelConfigFor returns modelID's registered config, loaded FRESH from
// disk (same rationale as RegisteredLocalModelIDs — a per-request concurrency
// gate must see another process's latest max_parallel, not a stale in-memory
// snapshot). ok is false if modelID is not registered.
func LocalModelConfigFor(modelID string) (lm LocalModelConfig, ok bool, err error) {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return LocalModelConfig{}, false, err
	}
	lm, ok = cfg.LocalModels[modelID]
	return lm, ok, nil
}

// SaveOcodeASTPlugin persists the enabled state of the opt-in "ast" tool.
func SaveOcodeASTPlugin(enabled bool) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.Plugins.AST = enabled
		return nil
	})
}

// SaveIDEMode persists only the ide_mode field using load-modify-write so it
// cannot clobber a concurrent session's other config.
func SaveIDEMode(mode string) error {
	switch mode {
	case IDEModeOff, IDEModeClaude:
	default:
		return fmt.Errorf("invalid ide_mode: %q (valid: %s, %s)", mode, IDEModeOff, IDEModeClaude)
	}
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.IDEMode = mode
		return nil
	})
}

func SaveEditorMode(mode string) error {
	switch mode {
	case EditorModeExternal, EditorModeTmuxSplit, EditorModeTmuxWindow:
	default:
		return fmt.Errorf("invalid editor_mode: %q (valid: %s, %s, %s)", mode, EditorModeExternal, EditorModeTmuxSplit, EditorModeTmuxWindow)
	}
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.EditorMode = mode
		return nil
	})
}

func init() {
	// Config saves always target the global path (see ActiveOcodeConfigPath),
	// so backups must live next to it, not in a cwd-relative ".opencode" dir
	// that may be read-only for the desktop/web server process.
	if path, err := getGlobalOcodeConfigPath(); err == nil {
		snapshot.SetGlobalBaseDir(filepath.Join(filepath.Dir(path), "snapshots"))
	}
}

func getGlobalOcodeConfigPath() (string, error) {
	dir, err := GlobalConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ocodeconfig.json"), nil
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
	// A model id without a "provider/model" or "provider:model" separator
	// cannot be resolved back to a provider by agent.NewClient — restoring it
	// on the next start makes every bootstrap fail with a misleading
	// "no API key for provider openai" refusal (observed 2026-08-23 with a
	// bare "gpt-4o-mini" landing here). Persisting is allowed (callers rely
	// on the UI reflecting the pick), but the bad value is called out so the
	// writer can be identified from the log.
	if !strings.Contains(providerModel, "/") && !strings.Contains(providerModel, ":") {
		log.Printf("config: saving last_model %q without a provider prefix; this model will not resolve on next start", providerModel)
	}
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		raw, _ := json.Marshal(providerModel)
		cfg.Extra[lastModelKey] = json.RawMessage(raw)
		return nil
	})
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

// ThinkingBudgetLevels lists the supported reasoning-effort token budgets in
// ascending order. Index 0 is "off" (no extended thinking). Shared by the TUI
// (ctrl+d cycle, /effort command, sidebar "reason:" line) and the web/desktop
// server (TUIStatus.ThinkingBudget + /api/config/thinking-budget) so both
// surfaces agree on the level set.
var ThinkingBudgetLevels = []int{0, 1024, 8000, 16000, 32000, 65536}

// ThinkingBudgetLabels names each ThinkingBudgetLevels entry, used for the
// "reason: <label>" display and the /effort + web API level names.
var ThinkingBudgetLabels = []string{"off", "low", "med", "high", "xhigh", "max"}

// ThinkingBudgetForLabel maps an effort level name (off/low/med/high/xhigh/max,
// plus the aliases none/medium and a level's exact token value) to its token
// budget. Shared by the -effort CLI flags and the TUI /effort command so every
// surface accepts the same level names.
func ThinkingBudgetForLabel(label string) (int, bool) {
	l := strings.ToLower(strings.TrimSpace(label))
	switch l {
	case "none":
		l = "off"
	case "medium":
		l = "med"
	}
	for i, name := range ThinkingBudgetLabels {
		if l == name || l == strconv.Itoa(ThinkingBudgetLevels[i]) {
			return ThinkingBudgetLevels[i], true
		}
	}
	return 0, false
}

// ThinkingLevelIndexForBudget returns the index into ThinkingBudgetLevels for
// budget, defaulting to 0 ("off") when budget is not one of the known levels.
func ThinkingLevelIndexForBudget(budget int) int {
	for i, level := range ThinkingBudgetLevels {
		if level == budget {
			return i
		}
	}
	return 0
}

// SaveLastThinkingBudget persists the last used thinking budget into ocodeconfig.json
// so it can be restored across sessions.
func SaveLastThinkingBudget(budget int) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		raw, _ := json.Marshal(budget)
		cfg.Extra[lastThinkingBudgetKey] = json.RawMessage(raw)
		return nil
	})
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

// loadFullOcodeConfig loads the global ocode config only.
// Used by Save* functions to load current state before modifying a specific field.
// Project paths are NOT merged here — merging would cause them to be written back
// to the global config on every save, leaking per-project paths into the global file.
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
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.TUI.Theme = theme
		return nil
	})
}

func SaveAdvisorModel(providerModel string) error {
	if providerModel != "" {
		if provider, model := SplitProviderModel(providerModel); provider == "" || model == "" {
			return fmt.Errorf("advisor model must be in provider/model format")
		}
	}
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if providerModel == "" {
			// Reset only the model/provider fields, not the entire advisor
			// block. Resetting the whole struct would silently restore the
			// default checkpoints (["plan","done"]) and re-enable the advisor,
			// which couples unrelated policy state to the model-picker command.
			def := defaultAdvisorConfig()
			cfg.Advisor.Provider = def.Provider
			cfg.Advisor.Model = def.Model
			cfg.Advisor.ClaudeCode = def.ClaudeCode
			return nil
		}
		provider, model := SplitProviderModel(providerModel)
		cfg.Advisor.Provider = provider
		cfg.Advisor.Model = model
		cfg.Advisor.ClaudeCode = (provider == "claude-code")
		return nil
	})
}

// SaveDocPromptEnabled persists the doc-prompt toggle to config.
func SaveDocPromptEnabled(enabled bool) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.DocPromptEnabled = enabled
		return nil
	})
}

// SaveProfileDebug persists the profile debug toggle to config.
func SaveProfileDebug(enabled bool) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.ProfileDebug = enabled
		return nil
	})
}

// SaveAdvisorEnabled persists the advisor enabled/disabled state to config.
func SaveAdvisorEnabled(enabled bool) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.Advisor.Enabled = enabled
		return nil
	})
}

// SaveTerminalScrollbackLines persists the interactive terminal's scrollback
// limit using the same locked read-modify-write path as other config setters.
func SaveTerminalScrollbackLines(lines int) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.TerminalScrollbackLines = NormalizeTerminalScrollbackLines(lines)
		return nil
	})
}

// SaveTerminalFontFamily persists the interactive terminal's font family using
// the same locked read-modify-write path as other config setters. An empty
// fontFamily clears the override (frontend falls back to its built-in default
// monospace stack).
func SaveTerminalFontFamily(fontFamily string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.TerminalFontFamily = strings.TrimSpace(fontFamily)
		return nil
	})
}

// SaveTerminalFontSize persists the interactive terminal's font size using the
// same locked read-modify-write path as other config setters. A size <= 0
// clears the override so the size tracks the built-in default again.
func SaveTerminalFontSize(fontSize int) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if fontSize <= 0 {
			cfg.TerminalFontSize = 0
			return nil
		}
		cfg.TerminalFontSize = NormalizeTerminalFontSize(fontSize)
		return nil
	})
}

// SaveTerminalShell persists the interactive terminal's shell binary using the
// same locked read-modify-write path as other config setters. An empty shell
// clears the override (the server falls back to $SHELL, then a platform
// default).
func SaveTerminalShell(shell string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.TerminalShell = strings.TrimSpace(shell)
		return nil
	})
}

// SaveMemoryEnabled persists the memory prompt-injection toggle to config.
func SaveMemoryEnabled(enabled bool) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.MemoryEnabled = enabled
		return nil
	})
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
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		mutate(&cfg.Security.Redaction)
		return nil
	})
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
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.SmallModel = model
		return nil
	})
}

// SaveSmallModelEnabled persists the small model enabled/disabled state to config.
func SaveSmallModelEnabled(enabled bool) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.SmallModelEnabled = enabled
		return nil
	})
}

// SaveRecapModel persists the recap model override to config.
// Set to empty string to clear the override and fall back to the small model.
func SaveRecapModel(model string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.RecapModel = model
		return nil
	})
}

// SaveRecapModelEnabled persists the recap model enabled/disabled state to config.
func SaveRecapModelEnabled(enabled bool) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.RecapModelEnabled = enabled
		return nil
	})
}

// SaveExplorerModel persists the explorer agent model override to config.
// Set to empty string to clear the override and fall back to the small model.
func SaveExplorerModel(model string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.ExplorerModel = model
		return nil
	})
}

// SaveExplorerModelEnabled persists the explorer model enabled/disabled state to config.
func SaveExplorerModelEnabled(enabled bool) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.ExplorerModelEnabled = enabled
		return nil
	})
}

// SaveContextModel persists the context agent model override to config.
// Set to empty string to clear the override and fall back to the small model.
func SaveContextModel(model string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.ContextModel = model
		return nil
	})
}

// SaveContextModelEnabled persists the context model enabled/disabled state to config.
func SaveContextModelEnabled(enabled bool) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.ContextModelEnabled = enabled
		return nil
	})
}

// SaveAutoContinueEnabled persists the auto-continue enabled/disabled state to config.
func SaveAutoContinueEnabled(enabled bool) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.AutoContinueEnabled = enabled
		return nil
	})
}

// SaveAutoContinueModel persists the auto-continue judge model override.
// Empty clears it, meaning auto-continue only ever fires on the hard
// StepLimitHit signal (no extra LLM call).
func SaveAutoContinueModel(model string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.AutoContinueModel = model
		return nil
	})
}

// SaveOcrConfig persists the full OCR configuration via load-modify-write.
// Only the OCR sub-tree is touched; all other fields are preserved from disk.
func SaveOcrConfig(ocrCfg ocr.OcrConfig) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.Ocr = ocrCfg
		return nil
	})
}

// SaveOcrModel persists just the OCR model ID to config (legacy compatibility).
// Writes to whichever backend is currently active.
func SaveOcrModel(model string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.Ocr.OpenAI.Model = model
		cfg.Ocr.Paddle.Variant = model
		return nil
	})
}

// SaveOcrEnabled persists just the OCR tool enabled/disabled state (legacy compatibility).
func SaveOcrEnabled(enabled bool) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.Ocr.Enabled = enabled
		return nil
	})
}

// SavePinnedSkills persists the list of permanently-discovered skill names.
// The list lives under the discovery block in the config file — there is a
// single source of truth (`Discovery.PinnedSkills`) so load/save are symmetric.
func SavePinnedSkills(skills []string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.Discovery.PinnedSkills = append([]string{}, skills...)
		return nil
	})
}

// SavePermissionModel persists the auto-permission model override.
// Set to empty string to clear the override and fall back to the small model.
func SavePermissionModel(providerModel string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if cfg.Permissions.Auto == nil {
			cfg.Permissions.Auto = &AutoPermissionConfig{Enabled: false}
		}
		cfg.Permissions.Auto.Model = providerModel
		return nil
	})
}

// SavePermissionModeSwitch persists the permission mode (normal/yolo/locked)
// via load-modify-write, so it cannot clobber a concurrent session's rules or
// auto-permission state.
func SavePermissionModeSwitch(mode string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.Permissions.Mode = mode
		return nil
	})
}

// SaveSingleToolRule sets one entry in permissions.tools via load-modify-write.
// Only the named tool's entry is touched; all other rules and permissions fields
// are preserved from disk.
func SaveSingleToolRule(tool, level string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if cfg.Permissions.Tools == nil {
			cfg.Permissions.Tools = make(map[string]string)
		}
		cfg.Permissions.Tools[tool] = level
		return nil
	})
}

// SaveSingleBashPrefixRule sets one entry in permissions.bash.prefixes via
// load-modify-write. Only the named prefix entry is touched.
func SaveSingleBashPrefixRule(prefix, level string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if cfg.Permissions.Bash.Prefixes == nil {
			cfg.Permissions.Bash.Prefixes = make(map[string]string)
		}
		cfg.Permissions.Bash.Prefixes[prefix] = level
		return nil
	})
}

// SaveBashAutoAllowPrefixEntry adds or removes a single entry from
// permissions.bash.auto_allow_prefixes via load-modify-write.
func SaveBashAutoAllowPrefixEntry(prefix string, add bool) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if add {
			for _, p := range cfg.Permissions.Bash.AutoAllowPrefixes {
				if p == prefix {
					return nil // already present
				}
			}
			cfg.Permissions.Bash.AutoAllowPrefixes = append(cfg.Permissions.Bash.AutoAllowPrefixes, prefix)
		} else {
			kept := cfg.Permissions.Bash.AutoAllowPrefixes[:0]
			for _, p := range cfg.Permissions.Bash.AutoAllowPrefixes {
				if p != prefix {
					kept = append(kept, p)
				}
			}
			cfg.Permissions.Bash.AutoAllowPrefixes = kept
		}
		return nil
	})
}

// SaveSingleBashPrefixMode sets one entry in permissions.bash.prefix_modes via
// load-modify-write. Only the named prefix mode entry is touched.
func SaveSingleBashPrefixMode(prefix, mode string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if cfg.Permissions.Bash.PrefixModes == nil {
			cfg.Permissions.Bash.PrefixModes = make(map[string]string)
		}
		cfg.Permissions.Bash.PrefixModes[prefix] = mode
		return nil
	})
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

// SaveOcodeCommitMsgConfig persists the commit-message generation model and prompt.
func SaveOcodeCommitMsgConfig(model, prompt string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.CommitMsgModel = model
		cfg.CommitMsgPrompt = prompt
		return nil
	})
}

// SaveOcodeCompactConfig persists the auto-compact settings block.
func SaveOcodeCompactConfig(cfg CompactConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.Compact = cfg
		return nil
	})
}

// SaveOcodeAutoPermissionConfig persists the auto-approval block, preserving
// Model (owned exclusively by the /permissions model setter) from whatever
// is currently on disk.
func SaveOcodeAutoPermissionConfig(cfg AutoPermissionConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		preservedModel := ""
		if c.Permissions.Auto != nil {
			preservedModel = c.Permissions.Auto.Model
		}
		cfg.Model = preservedModel
		c.Permissions.Auto = &cfg
		return nil
	})
}

// SaveOcodeDiscoveryConfig persists the discovery-based skill/MCP retrieval settings.
func SaveOcodeDiscoveryConfig(cfg DiscoveryConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.Discovery = cfg
		return nil
	})
}

// SaveOcodeTUIConfig persists the TUI theme/input/keybind settings.
func SaveOcodeTUIConfig(cfg TUIConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.TUI = cfg
		return nil
	})
}

// SaveOcodeEditorConfig persists the editor/editor-mode/ide-mode settings.
func SaveOcodeEditorConfig(editor, editorMode, ideMode string) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.Editor = editor
		c.EditorMode = editorMode
		c.IDEMode = ideMode
		return nil
	})
}

// SaveOcodePathsConfig persists the extra-allowed-paths and upload-directory settings.
func SaveOcodePathsConfig(extraAllowedPaths []string, uploadDir string) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.ExtraAllowedPaths = extraAllowedPaths
		c.UploadDir = uploadDir
		return nil
	})
}

// SaveOcodeLimits persists the execution limits in one atomic lock-held write.
// It does not replace SaveMaxSteps/SaveMaxConcurrentAgents, which remain for
// the TUI commands that use them.
func SaveOcodeLimits(maxSteps, maxImageDim, maxConcurrentAgents, undoMaxAgeDelta int) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.MaxSteps = maxSteps
		c.MaxImageDim = maxImageDim
		c.MaxConcurrentAgents = maxConcurrentAgents
		c.UndoMaxAgeDelta = undoMaxAgeDelta
		return nil
	})
}

// SaveOcodeFeatures persists the memory/doc-prompt feature toggles.
func SaveOcodeFeatures(memoryEnabled, docPromptEnabled bool) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.MemoryEnabled = memoryEnabled
		c.DocPromptEnabled = docPromptEnabled
		return nil
	})
}

// SaveOcodePluginsConfig persists the opt-in builtin tool gates.
func SaveOcodePluginsConfig(cfg PluginsConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.Plugins = cfg
		return nil
	})
}

// SaveOcodeLocalModels persists the registered local model instances.
func SaveOcodeLocalModels(models map[string]LocalModelConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.LocalModels = models
		return nil
	})
}

// SaveOcodeAdvisorConfig persists the whole advisor block (provider, model,
// claude_code, checkpoints). Callers that only want to change the model
// should keep using SaveAdvisorModel; this replaces the entire block.
func SaveOcodeAdvisorConfig(cfg AdvisorConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.Advisor = cfg
		return nil
	})
}
