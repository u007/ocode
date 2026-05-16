package agent

type AgentSpec struct {
	Name         string
	Description  string
	SystemPrompt string
	Tools        []string
	DeniedTools  []string
	Mode         Mode
}

var DefaultAgents = []AgentSpec{
	{
		Name:        "build",
		Description: "Full development work with all tools enabled",
		Mode:        ModeBuild,
	},
	{
		Name:        "plan",
		Description: "Analysis and planning without making changes",
		Mode:        ModePlan,
	},
	{
		Name:        "review",
		Description: "Code review with read-only access",
		Mode:        ModeReview,
	},
	{
		Name:        "debug",
		Description: "Focused investigation with bash and read tools",
		Tools:       []string{"read", "glob", "grep", "list", "lsp", "bash", "webfetch", "websearch", "skill"},
		Mode:        ModeDebug,
	},
	{
		Name:        "docs",
		Description: "Documentation writing with file operations",
		Tools:       []string{"read", "write", "edit", "glob", "grep", "list", "delete", "webfetch", "websearch", "skill"},
		Mode:        ModeDocs,
	},
}

func FindAgentSpec(name string) *AgentSpec {
	for i := range DefaultAgents {
		if DefaultAgents[i].Name == name {
			return &DefaultAgents[i]
		}
	}
	return nil
}

func NextAgentSpec(current string) *AgentSpec {
	for i := range DefaultAgents {
		if DefaultAgents[i].Name == current {
			next := (i + 1) % len(DefaultAgents)
			return &DefaultAgents[next]
		}
	}
	if len(DefaultAgents) > 0 {
		return &DefaultAgents[0]
	}
	return nil
}
