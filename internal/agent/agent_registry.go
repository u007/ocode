package agent

import "sort"

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
		{
			Name:         "general",
			Description:  "Multi-step tasks, parallel work",
			SystemPrompt: "You are a general-purpose sub-agent. Complete the task efficiently and return the final result. Use a User Expectation Checklist for multi-step work, validate each checklist item with the strongest practical check available, and iterate if validation fails. Be concise in your output and include checklist status, validation performed, and remaining gaps.",
			Mode:         AgentModeSubagent,
			Source:       "builtin",
		},
		{
			Name:         "explore",
			Description:  "Fast read-only codebase exploration",
			SystemPrompt: "You are an exploration sub-agent. Your goal is to quickly investigate the codebase and return findings. Use only read, glob, grep, list, and lsp tools. Do not modify any files. Return a concise summary of what you found, which user expectations the findings cover, and any remaining unknowns.",
			Tools:        []string{"read", "glob", "grep", "list", "lsp"},
			Mode:         AgentModeSubagent,
			Source:       "builtin",
		},
		{
			Name:         "scout",
			Description:  "External docs, dependency research",
			SystemPrompt: "You are a scout sub-agent. Research external documentation, APIs, and dependencies. Use webfetch and websearch to find relevant information. Return a concise summary with key findings, source URLs, which user expectations the sources cover, and any remaining unknowns.",
			Tools:        []string{"webfetch", "websearch", "read"},
			Mode:         AgentModeSubagent,
			Source:       "builtin",
		},
	}
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
