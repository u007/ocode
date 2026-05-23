package agent

import "strings"

// providerPrompt returns a short provider-tuned prompt fragment to nudge the
// model toward output styles its family handles best. Returns "" for unknown
// providers so we stay silent rather than hallucinating guidance.
//
// These fragments are intentionally small (one or two short paragraphs). They
// complement, not replace, the agent/mode prompt.
func providerPrompt(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic", "claude":
		return anthropicProviderPrompt
	case "openai", "gpt", "azure":
		return openaiProviderPrompt
	case "google", "gemini", "vertex":
		return geminiProviderPrompt
	case "copilot", "github":
		return copilotProviderPrompt
	case "moonshot", "kimi":
		return kimiProviderPrompt
	default:
		return ""
	}
}

const anthropicProviderPrompt = `Output discipline (Claude):
- Prefer concise prose. Use XML-style tags (<plan>, <file>, <diff>) only when structure clearly helps the reader; otherwise plain text.
- When tools are available, call them rather than describing what you would do.
- Think before acting: silently reason about edge cases, then output the result. Do not narrate internal deliberation in user-visible text.`

const openaiProviderPrompt = `Output discipline (GPT):
- Be direct and terse. Lead with the answer, then evidence.
- Prefer tool calls over textual descriptions when a tool fits.
- Use minimal structure — short paragraphs, sparing bullets. Avoid filler ("Sure!", "Certainly!", "Of course!").`

const geminiProviderPrompt = `Output discipline (Gemini):
- Structure complex answers with explicit headings or numbered steps; Gemini handles structured output well.
- Prefer tool calls over textual descriptions when a tool fits.
- Stay grounded in observed evidence; cite files or sources when making factual claims.`

const copilotProviderPrompt = `Output discipline (Copilot):
- Be direct and terse. Lead with the answer; show only the most relevant code.
- Prefer tool calls over textual descriptions when a tool fits.
- Avoid filler and unnecessary preamble.`

const kimiProviderPrompt = `Output discipline (Kimi):
- Be concise. Lead with the answer, then evidence.
- Prefer tool calls over textual descriptions when a tool fits.
- Avoid filler and unnecessary preamble.`
