package agent

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Suggested responses — the wire format between the model and the composer.
//
// The model is told (by the suggested-responses prompt fragment) to end its
// reply with a machine-readable block:
//
//	<ocode:suggestions>
//	["Run the test suite", "Open a PR", "Explain the diff"]
//	</ocode:suggestions>
//
// The block rides in the ASSISTANT'S OWN OUTPUT, which is deliberate:
//
//   - It does not touch the tools array, so the prompt cache is not busted
//     (Anthropic caches tools → system → messages; a tool that appears in
//     some turns and not others re-caches the whole prefix every turn).
//   - It does not touch the system block either, so the cached prefix stays
//     byte-identical turn over turn.
//   - The assistant's output is not part of the cached prefix at all, so a
//     marker inside it is free.
//
// The backend strips the block from the visible content and carries the parsed
// options on the message (Message.Suggestions), so the transcript shows only
// the human-readable reply and the model never sees its own suggestions
// echoed back on the next turn.
const (
	// SuggestionsBlockOpen opens the suggestions block in the model's output.
	SuggestionsBlockOpen = "<ocode:suggestions>"
	// SuggestionsBlockClose closes the suggestions block in the model's output.
	SuggestionsBlockClose = "</ocode:suggestions>"
	// MaxSuggestions is the hard cap on how many options are kept. The prompt
	// asks for at most five; this is the server-side backstop for a model that
	// ignores the instruction.
	MaxSuggestions = 5
	// maxSuggestionLength caps one option's length. A "suggestion" the user
	// cannot read in a chip is not a suggestion, and an unbounded one would let
	// the model stuff an entire essay into the composer.
	maxSuggestionLength = 120
)

// ExtractSuggestions splits a model reply into its human-readable content and
// the suggested follow-up options.
//
// The returned content is the reply with the suggestions block removed (and
// with the whitespace the block left behind tidied), so the transcript never
// shows the raw JSON. When there is no closed block, the content is returned
// UNCHANGED and the suggestions are nil — a broken block must never damage the
// reply the user actually asked for.
//
// The block is anchored on the FINAL close marker and the last open marker
// before it. A prose mention of the open marker earlier in the reply (or one
// after the block) therefore cannot swallow the answer that sits between them.
// A malformed payload inside a closed block is still stripped, with no
// suggestions reported.
//
// A block with zero usable entries is treated as absent: the model emitted the
// scaffolding but nothing worth showing, so the row simply does not render.
func ExtractSuggestions(content string) (string, []string) {
	end := strings.LastIndex(content, SuggestionsBlockClose)
	if end < 0 {
		// Unterminated block: the model was cut off mid-block. Leave the
		// content alone — half a JSON array is not worth guessing at, and the
		// visible reply is more valuable than a guessed suggestion.
		return content, nil
	}
	start := strings.LastIndex(content[:end], SuggestionsBlockOpen)
	if start < 0 {
		// A close marker with no open marker before it is not a block.
		return content, nil
	}
	body := content[start+len(SuggestionsBlockOpen) : end]
	tail := content[end+len(SuggestionsBlockClose):]

	suggestions := parseSuggestionsPayload(body)
	if len(suggestions) == 0 {
		// Nothing usable. Still strip the block so the user does not see the
		// scaffolding, but report no suggestions.
		return cleanAroundBlock(content, start, tail), nil
	}
	return cleanAroundBlock(content, start, tail), suggestions
}

// parseSuggestionsPayload parses the JSON array between the block markers.
// It is deliberately tolerant: a malformed payload yields no suggestions
// rather than an error, because a parse failure must never fail the turn.
func parseSuggestionsPayload(body string) []string {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	var raw []string
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		// A multi-line option would break the chip layout; collapse it to one
		// line rather than dropping the suggestion.
		s = strings.Join(strings.Fields(s), " ")
		// Cut by rune, not byte: a byte cut can split a multi-byte rune and
		// leave invalid UTF-8 that json.Marshal turns into U+FFFD on the wire.
		if utf8.RuneCountInString(s) > maxSuggestionLength {
			s = strings.TrimSpace(string([]rune(s)[:maxSuggestionLength])) + "…"
		}
		out = append(out, s)
		if len(out) == MaxSuggestions {
			break
		}
	}
	return out
}

// cleanAroundBlock reassembles the content without the suggestions block,
// leaving exactly one blank line between the reply and whatever followed the
// block (usually nothing). The reply keeps its original leading whitespace.
func cleanAroundBlock(content string, start int, tail string) string {
	head := content[:start]
	// Trim the trailing whitespace the block introduced, then re-attach a
	// single separator so the reply does not end with a dangling blank line.
	head = strings.TrimRight(head, " \t\n\r")
	tail = strings.TrimLeft(tail, " \t\n\r")
	if tail == "" {
		return head
	}
	return head + "\n\n" + tail
}

// suggestionsStreamFilter keeps the suggestions block out of the STREAMED
// reply.
//
// The block is stripped from the complete message (ExtractSuggestions), but
// the model streams its text as it produces it — so without this filter the
// raw JSON would flash in the transcript before the cleaned message replaced
// it. The filter therefore suppresses the block at the source, which covers
// every consumer at once (the web live buffer, the TUI's streaming view).
//
// It holds back a possible marker prefix rather than dropping text: the
// marker can straddle two deltas, so the last few bytes of the stream are
// withheld until it is clear they are not the start of the marker. The
// hold-back is at most len(SuggestionsBlockOpen)-1 bytes.
//
// The text from the first open marker onward is held until flush, because only
// the end of the round shows which open marker ExtractSuggestions anchors on.
// flush runs that held text through ExtractSuggestions, the same function that
// cleans the persisted message, so the live stream and the saved message agree
// in every round, tool-call rounds included.
type suggestionsStreamFilter struct {
	onDelta func(kind, text string)
	// pending is the text received but not yet emitted.
	pending string
	// inBlock is set once the marker has been seen: pending then holds the
	// block and everything after it.
	inBlock bool
}

// delta is the OnDelta-compatible callback handed to the provider client.
func (f *suggestionsStreamFilter) delta(kind, text string) {
	if f == nil || f.onDelta == nil {
		return
	}
	if kind != "text" {
		// Reasoning/thinking deltas are never part of the block.
		f.onDelta(kind, text)
		return
	}
	if f.inBlock {
		f.pending += text
		return
	}
	f.pending += text
	if idx := strings.Index(f.pending, SuggestionsBlockOpen); idx >= 0 {
		f.onDelta("text", f.pending[:idx])
		f.pending = f.pending[idx:]
		f.inBlock = true
		return
	}
	hold := markerHoldBack(f.pending, SuggestionsBlockOpen)
	if hold > 0 {
		f.onDelta("text", f.pending[:len(f.pending)-hold])
		f.pending = f.pending[len(f.pending)-hold:]
		return
	}
	f.onDelta("text", f.pending)
	f.pending = ""
}

// flushAll ends the stream for a round, whether it is the final reply or
// continues with tool calls. Held text that never opened a block is emitted
// as is. Held text from an open marker is run through ExtractSuggestions, so
// it is emitted exactly as the persisted message keeps it: the block is dropped
// when it is closed, and an unterminated block or a mention with no closing
// marker is emitted verbatim.
func (f *suggestionsStreamFilter) flushAll() {
	if f == nil || f.onDelta == nil {
		return
	}
	if f.pending != "" {
		text := f.pending
		if f.inBlock {
			text, _ = ExtractSuggestions(text)
		}
		if text != "" {
			f.onDelta("text", text)
		}
	}
	f.pending = ""
	f.inBlock = false
}

// markerHoldBack returns how many trailing bytes of s could still grow into
// marker — the longest suffix of s that is a proper prefix of marker. Those
// bytes must be withheld until more text arrives, because the marker may
// straddle two deltas.
func markerHoldBack(s, marker string) int {
	max := len(marker) - 1
	if max > len(s) {
		max = len(s)
	}
	for n := max; n > 0; n-- {
		if strings.HasPrefix(marker, s[len(s)-n:]) {
			return n
		}
	}
	return 0
}
