package agent

import "github.com/u007/ocode/internal/config"

// PromptKind says where a catalog prompt is used. The settings UI shows it as a
// badge in place of the prompt text. The values are display strings; the web
// type PromptKind in web/src/api/types.ts mirrors them.
type PromptKind string

const (
	PromptKindSubagent  PromptKind = "Sub-agent"
	PromptKindJudge     PromptKind = "Judge"
	PromptKindHelper    PromptKind = "Helper task"
	PromptKindMainAgent PromptKind = "Main agent prompt"
)

// PromptCatalogEntry describes one built-in system prompt that the user can
// refine with a prompt override (see config.ApplyPromptOverride). Key is the
// stable identifier stored in ocodeconfig.json, and Builtin is the unmodified
// prompt text the override is appended to. Builtin is read by the override
// logic only; it is never sent to the settings UI.
type PromptCatalogEntry struct {
	Key         string
	Label       string
	Description string
	Kind        PromptKind
	Builtin     string
}

// PromptCatalog lists every overridable built-in prompt, sorted by Key. The
// permission judge (permissions.auto.prompt), the Pulse assistant, the commit
// message prompt and the provider main prompts are configured elsewhere and
// are deliberately absent.
func PromptCatalog() []PromptCatalogEntry {
	return []PromptCatalogEntry{
		{
			Key:         "advisor",
			Label:       "Advisor",
			Description: "Role and tool-use rules for the advisor reviewer, which trusts supplied context and explores only when it must.",
			Kind:        PromptKindHelper,
			Builtin:     advisorSystemPrompt,
		},
		{
			Key:         "advisor_claude_code",
			Label:       "Advisor (Claude Code)",
			Description: "Read-only advisor prompt appended to Claude Code when it serves as the advisor, refusing file writes.",
			Kind:        PromptKindHelper,
			Builtin:     claudeCodeAdvisorPrompt,
		},
		{
			Key:         "auto_continue_judge",
			Label:       "Auto-continue judge",
			Description: "Triage rubric the auto-continue judge uses to pick the single next action after an assistant reply ends.",
			Kind:        PromptKindJudge,
			Builtin:     typesafeAutoContinueInstructions,
		},
		{
			Key:         "compaction",
			Label:       "Compaction summary",
			Description: "Instruction for the anchored summariser that compacts a long conversation into a running summary.",
			Kind:        PromptKindHelper,
			Builtin:     compactionSystemPrompt,
		},
		{
			Key:         "content_guard",
			Label:       "Content guard",
			Description: "Rubric for the inbound-content guardrail that decides whether fetched external content must be shown to a human.",
			Kind:        PromptKindJudge,
			Builtin:     contentGuardInstructions,
		},
		{
			Key:         "discovery_contract",
			Label:       "Tool discovery contract",
			Description: "Tells the model that unloaded MCP tools exist and to call discover_more before saying a tool is unavailable.",
			Kind:        PromptKindMainAgent,
			Builtin:     discoveryPromptContract,
		},
		{
			Key:         "doc_prompt",
			Label:       "Documentation-first prompt",
			Description: "Documentation-first development rules that require inspection and doc updates for nontrivial changes.",
			Kind:        PromptKindMainAgent,
			Builtin:     docPromptContent,
		},
		{
			Key:         "goal_judge",
			Label:       "Goal judge",
			Description: "Rubric the goal judge uses to decide whether a user-set goal is met, not met, or impossible.",
			Kind:        PromptKindJudge,
			Builtin:     goalJudgeInstructions,
		},
		{
			Key:         "md_summary",
			Label:       "Markdown file summary",
			Description: "One-sentence summary prompt for project markdown files that feeds the discovery corpus.",
			Kind:        PromptKindHelper,
			Builtin:     mdSummarySystemPrompt,
		},
		{
			Key:         "network_guard",
			Label:       "Network guard",
			Description: "Rubric for the outbound-network guardrail that decides whether a request sending data off this machine needs a human.",
			Kind:        PromptKindJudge,
			Builtin:     networkGuardInstructions,
		},
		{
			Key:         "recap",
			Label:       "Recap",
			Description: "Fragment that asks primary agents to close a turn that did work with a short recap for the user.",
			Kind:        PromptKindMainAgent,
			Builtin:     recapPromptContent,
		},
		{
			Key:         "redaction_awareness",
			Label:       "Redaction awareness",
			Description: "Explains the OCSEC redaction token format so the model treats redacted secrets as real values, not placeholders.",
			Kind:        PromptKindMainAgent,
			Builtin:     redactionAwarenessPrompt,
		},
		{
			Key:         "speech_summary",
			Label:       "Speech summary",
			Description: "Rewrites an assistant reply so a developer can listen to it instead of reading it.",
			Kind:        PromptKindHelper,
			Builtin:     speechSummarySystemPrompt,
		},
		{
			Key:         "subagent_context",
			Label:       "Sub-agent: context",
			Description: "System prompt for the context sub-agent, the knowledge curator for the project's docs bundle and codebase navigator.",
			Kind:        PromptKindSubagent,
			Builtin:     contextSubAgentPrompt,
		},
		{
			Key:         "subagent_explore",
			Label:       "Sub-agent: explore",
			Description: "System prompt for the explore sub-agent, a read-only navigator of this repository.",
			Kind:        PromptKindSubagent,
			Builtin:     exploreSubAgentPrompt,
		},
		{
			Key:         "subagent_general",
			Label:       "Sub-agent: general",
			Description: "System prompt for the general-purpose sub-agent that completes multi-step tasks.",
			Kind:        PromptKindSubagent,
			Builtin:     generalSubAgentPrompt,
		},
		{
			Key:         "subagent_scout",
			Label:       "Sub-agent: scout",
			Description: "System prompt for the scout sub-agent, a read-only researcher for code outside the workspace.",
			Kind:        PromptKindSubagent,
			Builtin:     scoutSubAgentPrompt,
		},
		{
			Key:         "suggested_responses",
			Label:       "Suggested responses",
			Description: "Fragment that asks primary agents to end a reply with a few short suggested follow-up responses.",
			Kind:        PromptKindMainAgent,
			Builtin:     suggestedResponsesPromptContent,
		},
		{
			Key:         "title",
			Label:       "Session title",
			Description: "Prompt that generates a concise three to seven word title for a conversation.",
			Kind:        PromptKindHelper,
			Builtin:     titleSystemPrompt,
		},
		{
			Key:         "tool_digest",
			Label:       "Tool call digest",
			Description: "Explains one pending tool call to the developer who must decide whether to approve it.",
			Kind:        PromptKindHelper,
			Builtin:     toolDigestSystemPrompt,
		},
	}
}

// PromptCatalogEntryFor returns the catalog entry for key and whether one exists.
func PromptCatalogEntryFor(key string) (PromptCatalogEntry, bool) {
	for _, e := range PromptCatalog() {
		if e.Key == key {
			return e, true
		}
	}
	return PromptCatalogEntry{}, false
}

// SubAgentPromptWithOverride returns a sub-agent's system prompt with the user's
// subagent_<name> override applied at call time. Every path that starts a
// built-in sub-agent or makes it the main agent must use it, so the override
// means the same thing everywhere. A prompt the user replaced in an agent file
// passes through unchanged.
func SubAgentPromptWithOverride(name, prompt string) string {
	return overrideBuiltinPrompt("subagent_"+name, prompt)
}

// overrideBuiltinPrompt applies the user's override for key to a prompt that
// was read from a long-lived definition (the agent registry or
// DefaultSubAgents). Those definitions are built once, so calling
// config.ApplyPromptOverride on the stored value would freeze the override at
// startup. This helper runs at use time instead, and only when prompt is still
// the unmodified built-in text for key. A prompt the user replaced in an agent
// file, or any key not in the catalog, passes through unchanged.
func overrideBuiltinPrompt(key, prompt string) string {
	entry, ok := PromptCatalogEntryFor(key)
	if !ok || prompt != entry.Builtin {
		return prompt
	}
	return config.ApplyPromptOverride(key, prompt)
}
