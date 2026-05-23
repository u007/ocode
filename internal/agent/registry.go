package agent

type AgentSpec struct {
	Name         string
	Description  string
	SystemPrompt string
	Tools        []string
	DeniedTools  []string
	Mode         Mode
	MaxSteps     int
	// Model is an optional override; when non-empty, switching to this agent
	// swaps the active LLM client to the named model.
	Model string
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

func PrimaryAgentSpecs() []AgentSpec {
	specs := make([]AgentSpec, 0, len(DefaultAgents))
	seen := make(map[string]bool)
	for _, spec := range DefaultAgents {
		specs = append(specs, spec)
		seen[spec.Name] = true
	}
	if DefaultAgentRegistry == nil {
		return specs
	}
	for _, def := range DefaultAgentRegistry.PrimaryAgents() {
		if seen[def.Name] || def.Hidden {
			continue
		}
		specs = append(specs, agentSpecFromDefinition(def))
	}
	return specs
}

func FindAgentSpec(name string) *AgentSpec {
	for _, spec := range PrimaryAgentSpecs() {
		if spec.Name == name {
			copy := spec
			return &copy
		}
	}
	return nil
}

func NextAgentSpec(current string) *AgentSpec {
	specs := PrimaryAgentSpecs()
	for i := range specs {
		if specs[i].Name == current {
			next := (i + 1) % len(specs)
			return &specs[next]
		}
	}
	if len(specs) > 0 {
		return &specs[0]
	}
	return nil
}

func agentSpecFromDefinition(def AgentDefinition) AgentSpec {
	mode := ModeBuild
	switch def.Name {
	case "plan":
		mode = ModePlan
	case "review":
		mode = ModeReview
	case "debug":
		mode = ModeDebug
	case "docs":
		mode = ModeDocs
	}
	return AgentSpec{
		Name:         def.Name,
		Description:  def.Description,
		SystemPrompt: def.SystemPrompt,
		Tools:        def.Tools,
		DeniedTools:  def.DeniedTools,
		Mode:         mode,
		MaxSteps:     def.MaxSteps,
		Model:        def.Model,
	}
}
