package tool

import "strings"

// UnansweredAsk reports whether a tool result is an ask the user has not
// answered yet — a permission request or a question prompt. An answered ask has
// its sentinel content replaced in place (or rewritten as
// QuestionDismissedResult on a dismissal), so a resolved ask stops matching.
//
// This is the canonical predicate for "a dialog is on screen". It lives in
// internal/tool, next to the sentinels themselves, because both
// internal/agent (which cannot import internal/session — session imports agent)
// and internal/session need it and internal/tool imports neither.
//
// It deliberately does NOT match a tool result that merely MENTIONS
// WAITING_FOR_USER_RESPONSE (a grep hit on agent.go, say): only the `question`
// tool writes that marker, and it always writes it alongside
// SentinelQuestionPrompt. Requiring the prefix keeps this predicate in step
// with what the desktop/web app actually renders from the sentinel, so a gate
// keyed on it can never disagree with "is there a dialog open".
func UnansweredAsk(content string) bool {
	if strings.HasPrefix(content, SentinelPermissionAsk) {
		return true
	}
	return strings.HasPrefix(content, SentinelQuestionPrompt) &&
		strings.Contains(content, SentinelWaitingForUser)
}
