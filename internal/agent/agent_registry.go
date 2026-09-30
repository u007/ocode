package agent

import (
	"sort"
	"sync"

	"github.com/u007/ocode/internal/config"
)

var DefaultAgentRegistry *AgentRegistry

func init() {
	DefaultAgentRegistry = NewAgentRegistry()
	DefaultAgentRegistry.LoadMarkdownAgents()
}

type AgentMode string

const (
	AgentModePrimary  AgentMode = "primary"
	AgentModeSubagent AgentMode = "subagent"
	AgentModeAll      AgentMode = "all"
)

type AgentDefinition struct {
	Name         string
	Description  string
	SystemPrompt string
	Tools        []string
	DeniedTools  []string
	Mode         AgentMode
	Hidden       bool
	Permissions  map[string]interface{}
	Source       string
	MaxSteps     int
	// Model is an optional OpenCode-style override in "provider/model" or
	// "provider:model" form. Empty means inherit the session's current model.
	Model string
	// Color is an optional ANSI/hex color used by the TUI to tint the agent
	// name in the status bar. Accepts named colors ("blue", "green") or
	// hex ("#7AA2F7"). Empty means use the default text color.
	Color string
	// Temperature/TopP, when non-nil, override the client's sampling
	// parameters when this agent is active. Pointer so "unset" is distinct
	// from explicit zero.
	Temperature *float64
	TopP        *float64
	// ExpectedOutput is an optional default output contract for dispatches
	// of this agent: a short natural-language description of the shape or
	// content the caller requires of the result. When set, a dispatch of
	// this agent without an explicit expected_output argument is verified
	// against it before the result is returned (and retried once on
	// failure). Empty means no default contract — verification is skipped
	// entirely unless the call supplies expected_output.
	ExpectedOutput string
}

type LoadDiagnostic struct {
	Level   string
	File    string
	Message string
}

// AgentRegistry holds the agent definitions. ReloadMarkdownAgents REPLACES the
// whole list (it clears defs/diagnostic, re-registers the built-ins, then
// re-adds the markdown entries), so every read and every reload must be
// serialized: without this, a background goroutine resolving an agent (e.g.
// title generation) reads the slice while a config reload repopulates it, which
// is a data race and a torn read.
//
// The write lock is held for a WHOLE reload so no reader can observe a
// half-built list. Readers copy what they need under the read lock and then
// work on the copy, so a slow or re-entrant caller never blocks a reload. Get
// hands back a pointer to a COPY for the same reason: callers keep the pointer
// and read its fields after the call returns, so it must not alias the
// registry's live slice.
type AgentRegistry struct {
	mu         sync.RWMutex
	defs       []AgentDefinition
	diagnostic []LoadDiagnostic
}

func NewAgentRegistry() *AgentRegistry {
	r := &AgentRegistry{}
	r.registerBuiltins()
	return r
}

// registerBuiltins seeds the built-in definitions. Callers must hold the write
// lock, or be the constructor before the registry is published.
func (r *AgentRegistry) registerBuiltins() {
	r.defs = []AgentDefinition{
		{
			Name:        "build",
			Description: "Full development work with all tools enabled",
			Mode:        AgentModePrimary,
			Source:      "builtin",
		},
		{
			Name:        "plan",
			Description: "Analysis and planning without making changes",
			Mode:        AgentModePrimary,
			Source:      "builtin",
		},
		{
			Name:        "review",
			Description: "Code review with read-only access",
			Mode:        AgentModePrimary,
			Source:      "builtin",
		},
		{
			Name:        "debug",
			Description: "Focused investigation with bash and read tools",
			Tools:       []string{"read", "glob", "grep", "rgrep", "list", "lsp", "bash", "webfetch", "websearch", "skill", "load_skill"},
			Mode:        AgentModePrimary,
			Source:      "builtin",
		},
		{
			Name:        "docs",
			Description: "Documentation writing with file operations",
			Tools:       []string{"read", "write", "edit", "glob", "grep", "rgrep", "list", "delete", "webfetch", "websearch", "skill", "load_skill"},
			Mode:        AgentModePrimary,
			Source:      "builtin",
		},
	}
	// Subagents (general/explore/scout) come from DefaultSubAgents — single
	// source of truth for name/description/prompt/tools. Hidden agents
	// (title, compaction) drive runtime helpers and can be overridden by
	// users via markdown files in .opencode/agents/.
	for _, sa := range DefaultSubAgents {
		r.defs = append(r.defs, AgentDefinition{
			Name:         sa.Name,
			Description:  sa.Description,
			SystemPrompt: sa.SystemPrompt,
			Tools:        sa.Tools,
			Mode:         AgentModeSubagent,
			Source:       "builtin",
		})
	}
	r.defs = append(r.defs,
		AgentDefinition{
			Name:         "title",
			Description:  "Generates session titles after the first exchange",
			SystemPrompt: titleSystemPrompt,
			Mode:         AgentModeSubagent,
			Hidden:       true,
			Source:       "builtin",
		},
		AgentDefinition{
			Name:         "compaction",
			Description:  "Summarizes older context when the window fills",
			SystemPrompt: compactionSystemPrompt,
			Mode:         AgentModeSubagent,
			Hidden:       true,
			Source:       "builtin",
		},
		// "orchestrator" is a picker-only entry: the TUI session intercept
		// recognises this name and routes user messages to the orchestrator
		// pipeline instead of starting a normal LLM turn. No system prompt
		// is needed because the pipeline builds its own context per dispatch.
		AgentDefinition{
			Name:        "orchestrator",
			Description: "Self-healing multi-agent pipeline — plans, implements, and validates coding goals",
			Mode:        AgentModeAll,
			Hidden:      false,
			Source:      "builtin",
		},
	)
}

// Get returns a pointer to a COPY of the named definition, or nil.
//
// The copy is DEFENCE IN DEPTH, not a fix for a live bug: under the current
// write discipline (every reload starts by clearing defs, so a published array
// is never written again) a pointer into the slice would happen to stay valid.
// It is copied anyway because addLoaded mutates in place, so the first caller
// to add a post-publication update path would silently start mutating values
// callers already hold. TestAgentRegistryGetIsStableAcrossUpdate pins the
// contract so that path cannot regress unnoticed.
func (r *AgentRegistry) Get(name string) *AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := range r.defs {
		if r.defs[i].Name == name {
			def := r.defs[i]
			return &def
		}
	}
	return nil
}

func (r *AgentRegistry) SubAgents() []AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []AgentDefinition
	for _, d := range r.defs {
		if d.Mode == AgentModeSubagent || d.Mode == AgentModeAll {
			result = append(result, d)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (r *AgentRegistry) PrimaryAgents() []AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []AgentDefinition
	for _, d := range r.defs {
		if d.Mode == AgentModePrimary || d.Mode == AgentModeAll {
			result = append(result, d)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (r *AgentRegistry) All() []AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]AgentDefinition, len(r.defs))
	copy(result, r.defs)
	return result
}

// Diagnostics returns the load diagnostics. The read lock is load-bearing: a
// rebuild reassigns r.diagnostic several times, so an unguarded read can observe
// a half-built value. The returned slice is NOT copied — unlike Get, nothing
// mutates the diagnostic slice in place after publication (each reload builds a
// fresh one), so aliasing it is safe today and a copy could not be justified by
// any test. Callers must treat it as read-only, which is the pre-existing
// contract.
func (r *AgentRegistry) Diagnostics() []LoadDiagnostic {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.diagnostic
}

// addLoaded inserts or replaces one definition, preserving precedence order. It
// mutates in place, so callers must hold the write lock AND the slice must not
// be published yet — the only caller is reloadMarkdownAgents, which builds the
// new list under the lock before any reader can see it.
func (r *AgentRegistry) addLoaded(def AgentDefinition) {
	for i := range r.defs {
		if r.defs[i].Name == def.Name {
			r.defs[i] = def
			return
		}
	}
	r.defs = append(r.defs, def)
}

func ApplyAgentConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}
	DefaultAgentRegistry.ReloadMarkdownAgents(enabledPluginMap(cfg))
	if cfg.Agent == nil {
		return
	}
	for name, raw := range cfg.Agent {
		agentCfg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		steps, ok := extractSteps(agentCfg)
		if !ok {
			continue
		}
		for i := range DefaultAgents {
			if DefaultAgents[i].Name == name {
				DefaultAgents[i].MaxSteps = steps
			}
		}
		def := DefaultAgentRegistry.Get(name)
		if def != nil {
			def.MaxSteps = steps
		}
	}
}

func enabledPluginMap(cfg *config.Config) map[string]bool {
	if cfg == nil || len(cfg.Plugins) == 0 {
		return nil
	}
	enabled := make(map[string]bool, len(cfg.Plugins))
	for name, p := range cfg.Plugins {
		enabled[name] = p.Enabled
	}
	return enabled
}

func extractSteps(cfg map[string]interface{}) (int, bool) {
	if v, ok := cfg["steps"]; ok {
		switch n := v.(type) {
		case float64:
			if int(n) > 0 {
				return int(n), true
			}
		case int:
			if n > 0 {
				return n, true
			}
		}
	}
	if v, ok := cfg["maxSteps"]; ok {
		switch n := v.(type) {
		case float64:
			if int(n) > 0 {
				return int(n), true
			}
		case int:
			if n > 0 {
				return n, true
			}
		}
	}
	return 0, false
}
