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

// PromptCatalogEntry describes one built-in system prompt the settings UI shows.
// Key is the stable identifier stored in ocodeconfig.json. Builtin is the exact
// Go value the runtime sends, not a copy, so the text the user reviews is the
// text that is used. An overridable entry has a user override appended to
// Builtin (see config.ApplyPromptOverride). A ReadOnly entry is shown for review
// only: the server refuses to override it and no override is applied to it, so
// a judge rubric cannot be edited through the UI.
type PromptCatalogEntry struct {
	Key         string
	Label       string
	Description string
	Kind        PromptKind
	Builtin     string
	ReadOnly    bool
}

// PromptCatalog lists every built-in prompt the settings UI shows, sorted by
// Key. Overridable entries take a user override. ReadOnly entries (the
// permission judge prompts and the provider prompts) are listed for review
// only. The Pulse assistant prompt and the commit message prompt are configured
// elsewhere and are deliberately absent. The user's own permissions.auto.prompt
// addendum is separate from the judge prompts listed here.
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
			Key:         "advisor_gate",
			Label:       "Advisor gate",
			Description: "Rubric the advisor gate judge uses to decide whether a second-model review is worth running before a commit or at the end of a turn.",
			Kind:        PromptKindJudge,
			Builtin:     advisorGateInstructions,
		},
		{
			Key:         "auto_continue_judge",
			Label:       "Auto-continue judge",
			Description: "Triage rubric the auto-continue judge uses to pick the single next action after an assistant reply ends.",
			Kind:        PromptKindJudge,
			Builtin:     typesafeAutoContinueInstructions,
		},
		{
			Key:         "auto_continue_question_judge",
			Label:       "Auto-continue question judge",
			Description: "Rubric the auto-continue question judge uses to answer a structured question prompt on the user's behalf only when the transcript already determines the answer.",
			Kind:        PromptKindJudge,
			Builtin:     typesafeAutoContinueQuestionInstructions,
		},
		{
			Key:         "communication_style",
			Label:       "Communication style",
			Description: "ASD-STE100 sentence limits, active voice, consistent naming, and code-first output for primary-agent replies.",
			Kind:        PromptKindMainAgent,
			Builtin:     communicationStylePromptContent,
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
			Key:         "permission_judge",
			Label:       "Permission judge",
			Description: "Gatekeeper rubric the auto-permission judge sends with each tool call that is waiting for approval, deciding whether it may run unasked.",
			Kind:        PromptKindJudge,
			Builtin:     typesafeJudgeInstructions,
			ReadOnly:    true,
		},
		{
			Key:         "permission_judge_chat",
			Label:       "Permission judge (chat model)",
			Description: "Request template the chat-model permission judge fills in with a tool call's arguments, scope and project context before it answers ALLOW or DENY.",
			Kind:        PromptKindJudge,
			Builtin:     permissionJudgePromptTemplate,
			ReadOnly:    true,
		},
		{
			Key:         "permission_judge_concern",
			Label:       "Permission judge: concern question",
			Description: "Rubric the auto-permission judge uses to name the single most serious concern with a tool call, and to say what it is unsure about when it leans toward allowing.",
			Kind:        PromptKindJudge,
			Builtin:     typesafeConcernInstructions,
			ReadOnly:    true,
		},
		{
			Key:         "permission_judge_enforced_concern",
			Label:       "Permission judge: concern with categories off",
			Description: "Concern question used when the user has switched some concern categories off, which names the most serious concern among the categories still enforced.",
			Kind:        PromptKindJudge,
			Builtin:     typesafeEnforcedConcernInstructions,
			ReadOnly:    true,
		},
		{
			Key:         "permission_judge_interpreter",
			Label:       "Permission judge (interpreter source)",
			Description: "Request template the chat-model permission judge fills in to report the concrete effects of an interpreter's source before the run is approved.",
			Kind:        PromptKindJudge,
			Builtin:     permissionInterpreterPromptTemplate,
			ReadOnly:    true,
		},
		{
			Key:         "permission_judge_rulebook",
			Label:       "Permission judge rulebook",
			Description: "Shipped default rulebook the chat-model permission judge prepends to every request; an auto-permission-prompt.md installed on disk replaces it while it is present and not superseded by this build.",
			Kind:        PromptKindJudge,
			Builtin:     config.BundledAutoPermissionPromptBody,
			ReadOnly:    true,
		},
		{
			Key:         "plan",
			Label:       "Plan then execute",
			Description: "Fragment that asks primary agents to write a todo plan before multi-step work, work it item by item, and delegate self-contained items with task(todo_item).",
			Kind:        PromptKindMainAgent,
			Builtin:     planExecutePromptContent,
		},
		{
			Key:         "provider_claude",
			Label:       "Provider prompt: Claude",
			Description: "Output-discipline fragment added to the main agent's context for Claude models and the anthropic provider.",
			Kind:        PromptKindMainAgent,
			Builtin:     claudePromptText,
			ReadOnly:    true,
		},
		{
			Key:         "provider_copilot",
			Label:       "Provider prompt: Copilot",
			Description: "Output-discipline fragment added to the main agent's context for the copilot and github providers.",
			Kind:        PromptKindMainAgent,
			Builtin:     copilotPromptText,
			ReadOnly:    true,
		},
		{
			Key:         "provider_deepseek",
			Label:       "Provider prompt: DeepSeek",
			Description: "Output-discipline fragment added to the main agent's context for DeepSeek models and the deepseek provider.",
			Kind:        PromptKindMainAgent,
			Builtin:     deepseekPromptText,
			ReadOnly:    true,
		},
		{
			Key:         "provider_gemini",
			Label:       "Provider prompt: Gemini",
			Description: "Output-discipline fragment added to the main agent's context for Gemini models and the google, gemini and vertex providers.",
			Kind:        PromptKindMainAgent,
			Builtin:     geminiPromptText,
			ReadOnly:    true,
		},
		{
			Key:         "provider_gpt",
			Label:       "Provider prompt: GPT",
			Description: "Output-discipline fragment added to the main agent's context for GPT models and the openai, gpt and azure providers.",
			Kind:        PromptKindMainAgent,
			Builtin:     gptPromptText,
			ReadOnly:    true,
		},
		{
			Key:         "provider_kimi",
			Label:       "Provider prompt: Kimi",
			Description: "Output-discipline fragment added to the main agent's context for Kimi models and the moonshot and kimi providers.",
			Kind:        PromptKindMainAgent,
			Builtin:     kimiPromptText,
			ReadOnly:    true,
		},
		{
			Key:         "provider_reasoning",
			Label:       "Provider prompt: reasoning models",
			Description: "Output-discipline fragment added to the main agent's context for reasoning models (o-series and thinking variants), keeping their chain of thought out of the reply.",
			Kind:        PromptKindMainAgent,
			Builtin:     reasoningPromptText,
			ReadOnly:    true,
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
			Key:         "small_model",
			Label:       "Small model guidance",
			Description: "Intent-analysis guidance added after the provider prompt for small, cheap models.",
			Kind:        PromptKindMainAgent,
			Builtin:     smallModelPromptText,
			ReadOnly:    true,
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
			Key:         "task_verifier",
			Label:       "Task output verifier",
			Description: "System prompt for the verifier that checks a sub-agent's result against its expected_output contract; a failed check triggers one in-place retry.",
			Kind:        PromptKindHelper,
			Builtin:     taskVerifierPromptText,
			ReadOnly:    true,
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
// file, a ReadOnly entry, or any key not in the catalog, passes through unchanged.
func overrideBuiltinPrompt(key, prompt string) string {
	entry, ok := PromptCatalogEntryFor(key)
	if !ok || entry.ReadOnly || prompt != entry.Builtin {
		return prompt
	}
	return config.ApplyPromptOverride(key, prompt)
}
