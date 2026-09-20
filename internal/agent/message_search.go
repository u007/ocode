package agent

import "strings"

// MessageMatchesQuery reports whether a transcript message contains needle,
// case-insensitively. `needle` MUST already be lower-cased by the caller.
//
// This is the single source of truth for "what does /search look at", shared by
// the server's session-search endpoint and (by mirroring) the web find bar.
// The TUI's own matcher (internal/tui/chat_search.go) operates on its display
// wrapper, but matches the same underlying fields.
//
// Fields covered — keep in sync with web/src/components/Chat/ChatSearchBar.tsx
// `messageMatchesQuery` and the TUI's `messageMatchesQuery`:
//   - Content          (the visible text)
//   - ReasoningContent (thinking blocks)
//   - Notice           (transient notices rendered in the transcript)
//   - ToolCalls        (each call's function name and JSON arguments)
//
// Tool RESULTS are matched separately by the caller: a result lives on a
// separate `tool` message (matched here via its Content), while the web find
// bar additionally searches the result body attached to its parent group.
func MessageMatchesQuery(msg Message, needle string) bool {
	if needle == "" {
		return false
	}
	if msg.Content != "" && strings.Contains(strings.ToLower(msg.Content), needle) {
		return true
	}
	if msg.ReasoningContent != "" && strings.Contains(strings.ToLower(msg.ReasoningContent), needle) {
		return true
	}
	if msg.Notice != "" && strings.Contains(strings.ToLower(msg.Notice), needle) {
		return true
	}
	for _, tc := range msg.ToolCalls {
		if strings.Contains(strings.ToLower(tc.Function.Name), needle) {
			return true
		}
		if strings.Contains(strings.ToLower(tc.Function.Arguments), needle) {
			return true
		}
	}
	return false
}
