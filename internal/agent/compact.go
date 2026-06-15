package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Token estimation constants. The base heuristic is ~4 chars per token for regular text.
// Extended thinking / reasoning content is billed separately and costs ~2-3x more per
// character due to different tokenization and special handling by the LLM provider.
// Message framing overhead (role markers, JSON structure) adds ~50-100 tokens per message.
const (
	charsPerToken                = 4
	reasoningCharsPerToken       = 2   // Reasoning is more expensive; use ~2 chars per token
	framingOverheadPerMessage    = 75  // ~75 tokens for role, content key, JSON overhead
	messageStructureCharOverhead = 300 // ~300 chars worth of overhead per message for structure
)

// CompactResult describes the outcome of a compaction pass.
//
// When OK is true, the caller should splice its message list by replacing
// messages[ReplaceFrom:ReplaceTo] with the single Summary message. When OK
// is false, Err carries the reason (or it is nil if compaction was skipped
// because the threshold was not reached).
type CompactResult struct {
	OK          bool
	ReplaceFrom int
	ReplaceTo   int
	Summary     Message
	OriginalLen int
	// Note carries user-visible details about how the pass ran (e.g. batch
	// count, messages dropped for size) for display next to the banner.
	Note string
	Err  error
}

// tokenEstimate is a coarse heuristic used when real Usage data is unavailable.
// It properly separates regular text (~4 chars/token) from extended thinking
// content (~2 chars/token) which is billed at a higher rate. Includes message
// framing overhead. WARNING: This is unreliable and can be off by 20-40%;
// prefer real Usage data from the API when available.
func tokenEstimate(m Message) int {
	regularChars := len(m.Content)
	reasoningChars := len(m.ReasoningContent)

	for _, tc := range m.ToolCalls {
		regularChars += len(tc.Function.Name) + len(tc.Function.Arguments)
	}

	// Calculate tokens for each content type with appropriate multiplier
	regularTokens := (regularChars + charsPerToken - 1) / charsPerToken
	reasoningTokens := (reasoningChars + reasoningCharsPerToken - 1) / reasoningCharsPerToken

	// Add message framing overhead
	return regularTokens + reasoningTokens + framingOverheadPerMessage
}

func messagesTokens(msgs []Message) int {
	n := 0
	for _, m := range msgs {
		n += tokenEstimate(m)
	}
	// Add aggregate overhead for message structure and separators
	n += len(msgs) * (messageStructureCharOverhead / charsPerToken)
	return n
}

// findTurnBoundary walks backward through messages and returns the index of
// the start of the Nth-most-recent user turn (a turn starts at role=user).
// Returns 0 if fewer than N user turns exist.
func findTurnBoundary(msgs []Message, recentTurns int) int {
	if recentTurns <= 0 {
		return len(msgs)
	}
	seen := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			seen++
			if seen >= recentTurns {
				return i
			}
		}
	}
	return 0
}

// safeCut walks `cut` backward until messages[cut:] is a self-contained API
// request: every role=tool in the suffix has its matching assistant{ToolCalls}
// also in the suffix, AND every assistant{ToolCalls} in the suffix has a
// matching role=tool result. Returns the adjusted cut index (>= 0, <= cut).
func safeCut(msgs []Message, cut int) int {
	if cut <= 0 {
		return 0
	}
	if cut >= len(msgs) {
		cut = len(msgs)
	}
	for cut > 0 {
		// Build sets of assistant tool_call IDs and tool result IDs in the suffix [cut:].
		suffixCallIDs := map[string]struct{}{}
		suffixResultIDs := map[string]struct{}{}
		for i := cut; i < len(msgs); i++ {
			if msgs[i].Role == "assistant" {
				for _, tc := range msgs[i].ToolCalls {
					if tc.ID != "" {
						suffixCallIDs[tc.ID] = struct{}{}
					}
				}
			}
			if msgs[i].Role == "tool" && msgs[i].ToolID != "" {
				suffixResultIDs[msgs[i].ToolID] = struct{}{}
			}
		}
		safe := true
		// Every tool result must have a matching assistant tool_call.
		for i := cut; i < len(msgs); i++ {
			if msgs[i].Role == "tool" && msgs[i].ToolID != "" {
				if _, ok := suffixCallIDs[msgs[i].ToolID]; !ok {
					safe = false
					break
				}
			}
		}
		if safe {
			// Every assistant tool_call must have a matching tool result.
			for id := range suffixCallIDs {
				if _, ok := suffixResultIDs[id]; !ok {
					safe = false
					break
				}
			}
		}
		if safe {
			return cut
		}
		cut--
	}
	return 0
}

// findPrefixEnd determines how much of the front of `msgs` to keep verbatim:
// the leading run of role=system messages (typically one), plus the first user
// message if one immediately follows. This anchors the conversation with the
// original ask so the model never loses sight of why it was invoked.
func findPrefixEnd(msgs []Message) int {
	i := 0
	for i < len(msgs) && msgs[i].Role == "system" {
		// Stop before a compaction summary — it's conversation history,
		// not part of the base prompt prefix.
		if strings.HasPrefix(msgs[i].Content, compactionSummaryMarker) {
			break
		}
		i++
	}
	if i < len(msgs) && msgs[i].Role == "user" {
		i++
	}
	return i
}

// compactionSummaryMarker prefixes the Content of any synthetic system
// message produced by compaction. It lets later compactions locate the
// previous anchored summary so they can update it in place instead of
// re-summarising blended history.
const compactionSummaryMarker = "[ocode:compaction-summary]"

// findPreviousSummary scans messages for the most recent compaction summary
// (a role=system message whose Content begins with compactionSummaryMarker).
// Returns the summary body (marker stripped) and the index in msgs, or
// ("", -1) when no prior summary exists.
func findPreviousSummary(msgs []Message) (string, int) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "system" {
			continue
		}
		if !strings.HasPrefix(msgs[i].Content, compactionSummaryMarker) {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(msgs[i].Content, compactionSummaryMarker))
		return body, i
	}
	return "", -1
}

// pruneToolResults returns a copy of middle with each role=tool Content
// shrunk to maxChars + a "<N chars pruned>" suffix when it exceeds the cap.
// Other messages pass through untouched. This runs before summarisation so
// the summary model spends its budget on signal, not on cargo log output.
// In-memory only — the original messages on the agent are unmodified, since
// the splice replaces the middle wholesale with the new summary.
// compactPruneToolMaxChars mirrors opencode's TOOL_OUTPUT_MAX_CHARS (2000).
const compactPruneToolMaxChars = 2000

// Keep enough budget to preserve the prune suffix after a tool result has been
// capped to compactPruneToolMaxChars.
const compactRenderedToolMaxChars = compactPruneToolMaxChars + 128

func pruneToolResults(middle []Message, maxChars int) []Message {
	if maxChars <= 0 {
		return middle
	}
	out := make([]Message, len(middle))
	for i, m := range middle {
		if m.Role == "tool" && len(m.Content) > maxChars {
			pruned := len(m.Content) - maxChars
			m.Content = m.Content[:maxChars] + fmt.Sprintf("\n... [pruned %d chars from tool output before summarisation]", pruned)
		}
		out[i] = m
	}
	return out
}

// compactionSystemPrompt is the instruction prepended to every compaction
// summary call. Also exposed as the SystemPrompt of the hidden "compaction"
// registry agent so users can override it via .opencode/agents/compaction.md.
const compactionSystemPrompt = `You are an anchored context summariser for an ongoing coding-assistant conversation. ` +
	`Produce a single durable summary that the assistant can rely on after older turns are dropped from the context window. ` +
	`If a <previous-summary> block is supplied, update it: merge in new facts from the conversation segment and keep it current. ` +
	`Entries under "User Directives (verbatim)", "Key Decisions", and "Constraints & Preferences" are append-only: never remove, weaken, or paraphrase them away unless the conversation segment shows the user explicitly reversing them — mark such entries "(superseded: <why>)" instead of deleting. ` +
	`If no previous summary is supplied, create a fresh summary from the conversation segment. ` +
	`Do not narrate that you are summarising. Do not include filler. Preserve exact file paths, function names, command strings, identifiers, and error text.`

// summaryTemplate is the fixed Markdown structure every summary must follow.
// Every section must appear, even if its content is "(none)". Keeping the
// structure stable lets later compactions reliably update prior summaries.
const summaryTemplate = `Output exactly this Markdown structure and keep the section order unchanged:
---
## Original Request
- [the user's initial ask, in their words]

## Current Scope
- [what is being worked on now; note each scope change since the original request, or "(same as original request)"]

## User Directives (verbatim)
- [standing instructions, conditions, and corrections quoted exactly as the user stated them, or "(none)"]

## Constraints & Preferences
- [user constraints, preferences, specs, or "(none)"]

## Progress
### Done
- [completed work or "(none)"]

### In Progress
- [current work or "(none)"]

### Blocked
- [blockers or "(none)"]

## Current Work
- [precisely what was happening in the most recent messages of the segment, or "(none)"]

## Key Decisions
- [decision and why; mark reversed decisions "(superseded: <why>)" instead of deleting, or "(none)"]

## Errors & Fixes
- [what failed, root cause, what fixed it, and any user corrections, or "(none)"]

## Next Steps
- [ordered next actions or "(none)"]

## Critical Context
- [important technical facts, open questions, or "(none)"]

## Relevant Files
- [file or directory path: why it matters, or "(none)"]
---

Rules:
- Keep every section, even when empty.
- Use terse bullets, not prose paragraphs.
- Preserve exact file paths, commands, error strings, and identifiers when known.
- Do not mention the summary process or that context was compacted.`

// requiredSummarySections lists the headers a well-formed summary must
// contain; used to validate model output before it becomes the anchor for
// all future compactions.
var requiredSummarySections = []string{
	"## Original Request",
	"## Current Scope",
	"## User Directives (verbatim)",
	"## Constraints & Preferences",
	"## Progress",
	"## Current Work",
	"## Key Decisions",
	"## Errors & Fixes",
	"## Next Steps",
	"## Critical Context",
	"## Relevant Files",
}

// validateSummary returns an error naming the missing sections when the
// summary text does not contain every required header.
func validateSummary(text string) error {
	var missing []string
	for _, h := range requiredSummarySections {
		if !strings.Contains(text, h) {
			missing = append(missing, h)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("summary missing sections: %s", strings.Join(missing, ", "))
	}
	return nil
}

// compactUserMsgMaxChars caps user-message fragments in the summary input.
// User messages are the highest-signal content (directives, corrections,
// scope changes) so they get a much larger budget than tool noise.
const compactUserMsgMaxChars = 12000

// renderMiddleFragments pre-renders each middle message as a transcript
// fragment. Tool results are labelled with the tool's name (resolved from the
// matching assistant tool_call) rather than the opaque call ID. Non-marker
// system messages are rendered as [context] so injected context (hooks,
// skills, reminders) survives summarisation.
func renderMiddleFragments(middle []Message) []string {
	toolNames := map[string]string{}
	for _, m := range middle {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				toolNames[tc.ID] = tc.Function.Name
			}
		}
	}

	fragments := make([]string, 0, len(middle))
	for _, m := range middle {
		var b strings.Builder
		switch m.Role {
		case "user":
			fmt.Fprintf(&b, "[user]: %s", truncateForSummary(m.Content, compactUserMsgMaxChars))
		case "assistant":
			if m.ReasoningContent != "" {
				fmt.Fprintf(&b, "[assistant reasoning]: %s\n", truncateForSummary(m.ReasoningContent, 1500))
			}
			if m.Content != "" {
				fmt.Fprintf(&b, "[assistant]: %s", truncateForSummary(m.Content, 4000))
			}
			for _, tc := range m.ToolCalls {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				fmt.Fprintf(&b, "[tool_call %s]: %s", tc.Function.Name, truncateForSummary(tc.Function.Arguments, 800))
			}
		case "tool":
			toolName := toolNames[m.ToolID]
			if toolName == "" {
				toolName = m.ToolID
			}
			fmt.Fprintf(&b, "[tool_result %s]: %s", toolName, truncateForSummary(m.Content, compactRenderedToolMaxChars))
		case "system":
			// System messages carry injected context worth preserving. A prior
			// compaction summary normally sits before the middle (it is the
			// anchor), but the manual re-compact path feeds it through here —
			// render its body so the model has something to re-summarise.
			content := m.Content
			if strings.HasPrefix(content, compactionSummaryMarker) {
				content = strings.TrimSpace(strings.TrimPrefix(content, compactionSummaryMarker))
				fmt.Fprintf(&b, "[previous summary to re-summarise]: %s", truncateForSummary(content, compactUserMsgMaxChars))
				break
			}
			fmt.Fprintf(&b, "[context]: %s", truncateForSummary(content, 4000))
		}
		if b.Len() > 0 {
			fragments = append(fragments, b.String())
		}
	}
	return fragments
}

// summaryChunkReservedChars is the share of the prompt budget reserved for
// the fixed instructions, the anchored previous summary, and the focus note
// when slicing the middle into batches.
const summaryChunkReservedFraction = 0.35

// chunkMiddleByBudget splits the middle messages into consecutive batches
// whose rendered fragments each fit the per-call prompt budget, so the whole
// middle can be summarised in sequential anchored passes instead of dropping
// the oldest messages outright. A single oversized message still forms its
// own batch; buildSummaryPrompt's last-resort trimming handles it.
func chunkMiddleByBudget(middle []Message, maxInputTokens int) [][]Message {
	if maxInputTokens <= 0 {
		maxInputTokens = 50000
	}
	budget := int(float64(maxInputTokens*charsPerToken) * (1 - summaryChunkReservedFraction))
	var chunks [][]Message
	var cur []Message
	curChars := 0
	for i, m := range middle {
		frags := renderMiddleFragments(middle[i : i+1])
		size := 0
		for _, f := range frags {
			size += len(f) + 2 // separator
		}
		if len(cur) > 0 && curChars+size > budget {
			chunks = append(chunks, cur)
			cur = nil
			curChars = 0
		}
		cur = append(cur, m)
		curChars += size
	}
	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}
	return chunks
}

// buildSummaryPrompt assembles the prompt sent to the summary model. It walks
// the middle slice and emits a structured transcript that preserves tool
// calls, tool results, reasoning content, and injected context.
// If previousSummary is non-empty it is included as an anchor the model must
// update in place rather than re-synthesize from scratch. focus is optional
// user guidance (from "/compact <focus>") appended to the instructions.
// If the assembled prompt would exceed maxInputTokens, the oldest middle
// messages are dropped until it fits — callers should pre-chunk via
// chunkMiddleByBudget so this is a last resort, not the normal path.
func buildSummaryPrompt(middle []Message, maxInputTokens int, previousSummary, focus string) (string, int) {
	if maxInputTokens <= 0 {
		maxInputTokens = 50000
	}
	maxChars := maxInputTokens * charsPerToken

	fragments := renderMiddleFragments(middle)

	prev := strings.TrimSpace(previousSummary)

	// Drop oldest fragments until the full rendered prompt fits. Returns count
	// of dropped middle messages.
	dropped := 0
	joined := strings.Join(fragments, "\n\n")
	prompt := renderSummaryPrompt(prev, joined, dropped, focus)
	for len(prompt) > maxChars && len(fragments) > 1 {
		fragments = fragments[1:]
		dropped++
		joined = strings.Join(fragments, "\n\n")
		prompt = renderSummaryPrompt(prev, joined, dropped, focus)
	}

	// If an anchored previous summary still pushes us over the cap, shrink the
	// anchor section-by-section before sacrificing the most recent
	// conversation fragment. Headers always survive; tail sections (Next
	// Steps, Critical Context, Relevant Files) are never silently cut off.
	if len(prompt) > maxChars && prev != "" {
		prev = shrinkSummarySections(prev, maxChars, func(candidate string) string {
			return renderSummaryPrompt(candidate, joined, dropped, focus)
		})
		prompt = renderSummaryPrompt(prev, joined, dropped, focus)
	}

	// Last resort: trim the conversation segment itself while keeping the final
	// prompt under the configured cap.
	if len(prompt) > maxChars && joined != "" {
		joined = fitSummaryPromptSection(maxChars, joined, func(candidate string) string {
			return renderSummaryPrompt(prev, candidate, dropped, focus)
		})
		prompt = renderSummaryPrompt(prev, joined, dropped, focus)
	}

	return prompt, dropped
}

func renderSummaryPrompt(previousSummary, joined string, dropped int, focus string) string {
	var b strings.Builder
	b.WriteString(compactionSystemPrompt)
	b.WriteString("\n\n")
	b.WriteString(summaryTemplate)
	b.WriteString("\n\n")
	if f := strings.TrimSpace(focus); f != "" {
		fmt.Fprintf(&b, "The user asked this compaction to pay particular attention to: %s\n\n", f)
	}
	if prev := strings.TrimSpace(previousSummary); prev != "" {
		b.WriteString("<previous-summary>\n")
		b.WriteString(prev)
		b.WriteString("\n</previous-summary>\n\n")
		b.WriteString("Update the summary above using the conversation segment below.\n\n")
	} else {
		b.WriteString("Create a new summary from the conversation segment below.\n\n")
	}
	if dropped > 0 {
		fmt.Fprintf(&b, "[NOTE: %d earlier messages omitted from this batch due to size.]\n\n", dropped)
	}
	b.WriteString("Conversation segment:\n\n")
	b.WriteString(joined)
	return b.String()
}

// shrinkSummarySections fits an anchored previous summary into the prompt
// budget by trimming the longest section bodies first, keeping every "## "
// header intact. This replaces blind tail-truncation, which used to cut off
// whichever sections happened to sit at the end of the template.
func shrinkSummarySections(prev string, maxChars int, render func(string) string) string {
	if len(render(prev)) <= maxChars {
		return prev
	}
	lines := strings.Split(prev, "\n")
	// Group into sections: index 0 is any preamble before the first header.
	type section struct {
		header string // "" for preamble
		body   []string
	}
	sections := []section{{}}
	for _, ln := range lines {
		if strings.HasPrefix(ln, "## ") {
			sections = append(sections, section{header: ln})
			continue
		}
		sections[len(sections)-1].body = append(sections[len(sections)-1].body, ln)
	}
	rebuild := func() string {
		var b strings.Builder
		for i, s := range sections {
			if s.header != "" {
				if i > 0 {
					b.WriteString("\n")
				}
				b.WriteString(s.header)
				b.WriteString("\n")
			}
			b.WriteString(strings.Join(s.body, "\n"))
		}
		return b.String()
	}
	const minSectionChars = 200
	for {
		candidate := rebuild()
		if len(render(candidate)) <= maxChars {
			return candidate
		}
		// Halve the longest still-shrinkable section body.
		longest, longestLen := -1, minSectionChars
		for i, s := range sections {
			n := len(strings.Join(s.body, "\n"))
			if n > longestLen {
				longest, longestLen = i, n
			}
		}
		if longest < 0 {
			// Every section is already minimal; fall back to whole-string
			// binary-search truncation as the absolute last resort.
			return fitSummaryPromptSection(maxChars, candidate, render)
		}
		body := strings.Join(sections[longest].body, "\n")
		sections[longest].body = []string{truncatePromptSection(body, longestLen/2)}
	}
}

func fitSummaryPromptSection(maxChars int, value string, render func(string) string) string {
	if value == "" || len(render(value)) <= maxChars {
		return value
	}
	lo, hi := 0, len(value)
	best := ""
	for lo <= hi {
		mid := (lo + hi) / 2
		candidate := truncatePromptSection(value, mid)
		if len(render(candidate)) <= maxChars {
			best = candidate
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return best
}

func truncatePromptSection(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("... [+%d chars truncated]", len(s)-max)
}

func truncateForSummary(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("... [+%d chars truncated]", len(s)-max)
}

// runSummary invokes the summary client with a context deadline and retry
// loop. A response missing required template sections is treated as
// retryable; if retries are exhausted the best malformed response is still
// returned (with a debug warning) rather than failing the whole compaction.
// It is intentionally synchronous; callers run it from a goroutine.
func runSummary(ctx context.Context, client LLMClient, prompt string, maxRetries int, recordUsage func(*Message)) (string, error) {
	if client == nil {
		return "", errors.New("compact: no summary client")
	}
	var lastErr error
	malformed := ""
	// +1 grants one dedicated retry when the only failure is a malformed
	// (template-violating) summary, even with maxRetries=0.
	maxAttempts := maxRetries + 1
	for attempt := 0; attempt <= maxAttempts; attempt++ {
		if attempt > 0 {
			if attempt > maxRetries && malformed == "" {
				break // extra attempt is reserved for malformed output only
			}
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("compact: context cancelled during retry: %w", ctx.Err())
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		done := make(chan struct {
			content string
			err     error
		}, 1)
		go func() {
			resp, err := client.Chat([]Message{{Role: "user", Content: prompt}}, nil)
			if err != nil {
				done <- struct {
					content string
					err     error
				}{"", err}
				return
			}
			if recordUsage != nil {
				recordUsage(resp)
			}
			done <- struct {
				content string
				err     error
			}{resp.Content, nil}
		}()
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("compact: summary timed out: %w", ctx.Err())
		case r := <-done:
			if r.err == nil && strings.TrimSpace(r.content) != "" {
				if verr := validateSummary(r.content); verr != nil {
					emitDebug("COMPACT", fmt.Sprintf("attempt %d: %v; retrying", attempt+1, verr))
					malformed = r.content
					lastErr = verr
					continue
				}
				return r.content, nil
			}
			if r.err != nil {
				lastErr = r.err
			} else {
				lastErr = errors.New("compact: empty summary response")
			}
		}
	}
	if malformed != "" {
		// A summary that misses sections still beats failing the compaction
		// outright; the next compaction pass re-imposes the template.
		emitDebug("COMPACT", fmt.Sprintf("using malformed summary after retries: %v", lastErr))
		return malformed, nil
	}
	return "", lastErr
}

// CurrentContextEstimate returns the best estimate for the token count that
// will be sent on the next LLM request (excluding any new user prompt).
// It prefers real Usage data from the most recent API response and adds a
// heuristic estimate for any messages appended after that response.
func CurrentContextEstimate(msgs []Message) (tokens int, source string) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Usage == nil {
			continue
		}
		var base int64
		if msgs[i].Usage.TotalTokens != nil && *msgs[i].Usage.TotalTokens > 0 {
			base = *msgs[i].Usage.TotalTokens
		} else {
			if msgs[i].Usage.PromptTokens != nil {
				base += *msgs[i].Usage.PromptTokens
			}
			if msgs[i].Usage.CompletionTokens != nil {
				base += *msgs[i].Usage.CompletionTokens
			}
		}
		if base > 0 {
			tail := messagesTokens(msgs[i+1:])
			if tail > 0 {
				return int(base) + tail, "actual+tail"
			}
			return int(base), "actual"
		}
	}
	return messagesTokens(msgs), "estimated"
}

// shouldCompact decides whether the current message list warrants compaction.
// It uses CurrentContextEstimate so that messages appended after the latest
// Usage-bearing response (tool results, new user prompts, etc.) are counted.
// Falls back to a character heuristic with a 15% safety margin only when no
// Usage data exists at all.
func shouldCompact(msgs []Message, cfg compactRuntime) (bool, int) {
	if !cfg.Enabled {
		return false, 0
	}
	if len(msgs) < cfg.MinMessages {
		return false, 0
	}

	usedTokens, source := CurrentContextEstimate(msgs)
	if source == "estimated" {
		// Apply 15% safety margin to account for reasoning content and message
		// framing overhead that may be underestimated in the heuristic
		usedTokens = int(float64(usedTokens) * 1.15)
	}

	threshold := cfg.WindowTokens
	if threshold <= 0 {
		// No model window known: fall back to a conservative default so we compact
		// before hitting real limits. Allows compaction to trigger sooner when window
		// is unknown.
		threshold = 100_000
	}
	limit := int(float64(threshold) * cfg.TokenThreshold)
	return usedTokens >= limit, usedTokens
}

// contextWithTimeout returns a context that fires after `seconds` seconds.
// A zero/negative value yields context.Background with a no-op cancel.
func contextWithTimeout(seconds int) (context.Context, context.CancelFunc) {
	if seconds <= 0 {
		return context.Background(), func() {}
	}
	return context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
}

// inactivityContext creates a context that only times out after `seconds` of
// inactivity (no data received). The returned reset function must be called
// each time data is received to extend the deadline. This is useful for long-
// running LLM calls where the timeout should not fire while data is flowing.
func inactivityContext(seconds int) (context.Context, context.CancelFunc, func()) {
	if seconds <= 0 {
		return context.Background(), func() {}, func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	var mu sync.Mutex
	reset := func() {
		mu.Lock()
		deadline = time.Now().Add(time.Duration(seconds) * time.Second)
		mu.Unlock()
	}
	// Watchdog goroutine: periodically check if the deadline has passed.
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				if time.Now().After(deadline) {
					mu.Unlock()
					cancel()
					return
				}
				mu.Unlock()
			}
		}
	}()
	return ctx, cancel, reset
}

// compactRuntime is the resolved set of knobs the compaction pass needs at
// runtime, derived from CompactConfig + the active model's window size.
type compactRuntime struct {
	Enabled               bool
	TokenThreshold        float64
	KeepRecentTurns       int
	MinMessages           int
	SummaryTimeoutSeconds int
	SummaryMaxRetries     int
	MaxSummaryInputTokens int
	WindowTokens          int
}
