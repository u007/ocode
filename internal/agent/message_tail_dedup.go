package agent

import "strings"

// dedupeTrailingUserMessages collapses a run of identical user messages at the
// very end of the transcript down to a single message before it goes on the
// wire.
//
// A user who hits send twice (double Enter, a double click, an IME committing
// the same text twice) leaves two byte-identical user messages in a row, and a
// retried submit can re-append the tail the same way. The model only needs the
// request once: seeing it twice invites "do it again" answers and burns context
// on a repeat. The transcript itself is never touched — the copy the UI renders
// and the copy the session persists both keep every message, so nothing is lost
// and the dedup applies only to what is serialized for the provider.
//
// The run is the maximal suffix of user-role messages. Anything else at the
// tail (an assistant reply, a tool result, a system note) ends it: a user
// message that follows a real assistant turn is a follow-up, not a repeat, and
// collapsing it would delete meaning. Within the run only ADJACENT repeats are
// dropped, so the order the user actually typed in always survives.
//
// Volatile user-role tail blocks (discovery, todo re-anchor, notes delta, LSP
// delta, selection) are appended after the user's message and are therefore
// themselves part of the trailing run. They never separate a duplicate pair, and
// their distinct content means they are never themselves dropped.
//
// When there is nothing to trim the input slice is returned as-is, so a turn
// with no duplicate sends a byte-identical messages array.
func dedupeTrailingUserMessages(messages []Message) []Message {
	// Walk back over the maximal suffix of user-role messages.
	start := len(messages)
	for start > 0 && messages[start-1].Role == "user" {
		start--
	}
	run := messages[start:]
	if len(run) < 2 {
		return messages
	}
	// Cheap pre-scan: skip the copy when the run has no adjacent repeat, which
	// is every ordinary turn. The run is a handful of messages, so computing the
	// keys twice costs less than allocating a slice per request.
	hasRepeat := false
	for i := 1; i < len(run); i++ {
		if trailingUserMessageKey(run[i-1]) == trailingUserMessageKey(run[i]) {
			hasRepeat = true
			break
		}
	}
	if !hasRepeat {
		return messages
	}
	out := make([]Message, 0, len(messages))
	out = append(out, messages[:start]...)
	out = append(out, run[0])
	prev := trailingUserMessageKey(run[0])
	for _, m := range run[1:] {
		key := trailingUserMessageKey(m)
		if key == prev {
			// Keep the NEWEST copy so the surviving message carries the latest
			// UserSeq. Content is equal, so the request the model receives is
			// unchanged.
			out[len(out)-1] = m
			continue
		}
		out = append(out, m)
		prev = key
	}
	return out
}

// trailingUserMessageKey identifies the request a user message makes to the
// model: its text plus every attached image, so the same words around a
// different picture stay two distinct messages.
//
// Only what the provider actually receives is included. UserSeq is transcript
// bookkeeping (the frontend's snapshot-before-SSE identity), DisplayContent and
// Notice are UI-only, and a user-role message carries no tool state or thought
// signature by construction — none of them change what the model sees, so two
// messages differing only in those are the same request. Content is compared
// with surrounding whitespace trimmed, because a stray trailing newline is not
// a second, different instruction.
func trailingUserMessageKey(m Message) string {
	parts := make([]string, 0, 1+3*len(m.Images))
	parts = append(parts, strings.TrimSpace(m.Content))
	for _, img := range m.Images {
		parts = append(parts, img.MIMEType, img.Path, img.Data)
	}
	return lengthPrefixedKey(parts...)
}
