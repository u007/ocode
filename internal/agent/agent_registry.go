package agent

import (
	"sort"

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
}

type LoadDiagnostic struct {
	Level   string
	File    string
	Message string
}

type AgentRegistry struct {
	defs       []AgentDefinition
	diagnostic []LoadDiagnostic
}

func NewAgentRegistry() *AgentRegistry {
	r := &AgentRegistry{}
	r.registerBuiltins()
	return r
}

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
			Tools:       []string{"read", "glob", "grep", "list", "lsp", "bash", "webfetch", "websearch", "skill"},
			Mode:        AgentModePrimary,
			Source:      "builtin",
		},
		{
			Name:        "docs",
			Description: "Documentation writing with file operations",
			Tools:       []string{"read", "write", "edit", "glob", "grep", "list", "delete", "webfetch", "websearch", "skill"},
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

func (r *AgentRegistry) Get(name string) *AgentDefinition {
	for i := range r.defs {
		if r.defs[i].Name == name {
			return &r.defs[i]
		}
	}
	return nil
}

func (r *AgentRegistry) SubAgents() []AgentDefinition {
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
	result := make([]AgentDefinition, len(r.defs))
	copy(result, r.defs)
	return result
}

func (r *AgentRegistry) Diagnostics() []LoadDiagnostic {
	return r.diagnostic
}

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
