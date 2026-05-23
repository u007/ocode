package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Token estimation constants. The base heuristic is ~4 chars per token for regular text.
// Extended thinking / reasoning content is billed separately and costs ~2-3x more per
// character due to different tokenization and special handling by the LLM provider.
// Message framing overhead (role markers, JSON structure) adds ~50-100 tokens per message.
const (
	charsPerToken                = 4
	reasoningCharsPerToken       = 2     // Reasoning is more expensive; use ~2 chars per token
	framingOverheadPerMessage    = 75    // ~75 tokens for role, content key, JSON overhead
	messageStructureCharOverhead = 300   // ~300 chars worth of overhead per message for structure
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
	Err         error
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
// also in the suffix. Returns the adjusted cut index (>= 0, <= cut).
func safeCut(msgs []Message, cut int) int {
	if cut <= 0 {
		return 0
	}
	if cut >= len(msgs) {
		cut = len(msgs)
	}
	for cut > 0 {
		// Build set of assistant tool_call IDs in the suffix [cut:].
		suffixCallIDs := map[string]struct{}{}
		for i := cut; i < len(msgs); i++ {
			if msgs[i].Role == "assistant" {
				for _, tc := range msgs[i].ToolCalls {
					if tc.ID != "" {
						suffixCallIDs[tc.ID] = struct{}{}
					}
				}
			}
		}
		safe := true
		for i := cut; i < len(msgs); i++ {
			if msgs[i].Role == "tool" && msgs[i].ToolID != "" {
				if _, ok := suffixCallIDs[msgs[i].ToolID]; !ok {
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
		i++
	}
	if i < len(msgs) && msgs[i].Role == "user" {
		i++
	}
	return i
}

// buildSummaryPrompt assembles the prompt sent to the summary model. It walks
// the middle slice and emits a structured transcript that preserves tool
// calls, tool results, and reasoning content (not just user/assistant text).
// If the assembled prompt would exceed maxInputTokens, the oldest middle
// messages are dropped until it fits.
func buildSummaryPrompt(middle []Message, maxInputTokens int) (string, int) {
	if maxInputTokens <= 0 {
		maxInputTokens = 50000
	}
	maxChars := maxInputTokens * charsPerToken

	// Pre-render each middle message as a transcript fragment.
	fragments := make([]string, 0, len(middle))
	for _, m := range middle {
		var b strings.Builder
		switch m.Role {
		case "user":
			fmt.Fprintf(&b, "[user]: %s", truncateForSummary(m.Content, 4000))
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
			toolName := m.ToolID
			fmt.Fprintf(&b, "[tool_result %s]: %s", toolName, truncateForSummary(m.Content, 1500))
		case "system":
			// Skip transient system messages from the middle; the prefix system
			// already carries durable context.
			continue
		}
		if b.Len() > 0 {
			fragments = append(fragments, b.String())
		}
	}

	// Drop oldest fragments until we fit. Returns count of dropped messages.
	dropped := 0
	joined := strings.Join(fragments, "\n\n")
	for len(joined) > maxChars && len(fragments) > 1 {
		fragments = fragments[1:]
		dropped++
		joined = strings.Join(fragments, "\n\n")
	}

	prompt := "You are summarizing a portion of an ongoing coding-assistant " +
		"conversation that is being compacted to save context. Preserve: " +
		"(1) what the user asked for, (2) decisions made, (3) files/code " +
		"that were inspected or modified, (4) tool calls and their outcomes, " +
		"(5) any unresolved issues or pending work. Be terse but specific " +
		"with file paths, function names, and concrete results. Do not " +
		"include filler.\n\n"
	if dropped > 0 {
		prompt += fmt.Sprintf("[NOTE: %d earlier messages omitted from this batch due to size.]\n\n", dropped)
	}
	prompt += "Conversation segment:\n\n" + joined
	return prompt, dropped
}

func truncateForSummary(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("... [+%d chars truncated]", len(s)-max)
}

// runSummary invokes the summary client with a context deadline and retry
// loop. It is intentionally synchronous; callers run it from a goroutine.
func runSummary(ctx context.Context, client LLMClient, prompt string, maxRetries int) (string, error) {
	if client == nil {
		return "", errors.New("compact: no summary client")
	}
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
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
				return r.content, nil
			}
			if r.err != nil {
				lastErr = r.err
			} else {
				lastErr = errors.New("compact: empty summary response")
			}
		}
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
