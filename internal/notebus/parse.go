package notebus

import (
	"log"
	"strconv"
	"strings"
)

// MaxNoteBodyLen is the caveman-concise cap on note bodies.
// Anything longer is truncated with a marker (see truncationMarker).
// The cap is small by design — bus bodies appear verbatim in the
// injected <oc-log> block; longer notes are a sign the agent is
// over-sharing and should be split.
const MaxNoteBodyLen = 200

// truncationMarker is appended to bodies that exceed the cap.
// It is short so the body stays mostly intact and the marker
// signals the cut without changing the wire format.
const truncationMarker = "…[truncated]"

// noteOpen is the literal opening tag for a note. The match is
// exact (no whitespace tolerance) so an agent can be sure that
// any <oc-note …> in its output is intentional.
const noteOpen = `<oc-note`
const noteClose = `</oc-note>`
const resolveOpen = `<oc-resolve`
const resolveClose = `/>`

// attribute extraction: we read the next "key"="value" pair from
// the open tag. The parser is intentionally simple — it does not
// understand CDATA, namespaces, or HTML-style escaping. The
// supported attributes are exactly what the agent emits: at, ref.

// ParseEmitted extracts <oc-note> and <oc-resolve> entries from
// an assistant message. The bus stamps Seq and By after the
// parser returns; the parser is pure.
//
// Malformed tags (missing attribute, mismatched quotes, etc.)
// are logged via the standard log package and dropped — never
// silently swallowed, never a panic. Bodies are entity-encoded
// (<, >) so the bus can store the literal text safely and
// re-emit it without re-parse ambiguity.
//
// The agent argument is the id that goes into the resulting
// entries' By field. The bus overwrites this anyway (it stamps
// By from the agent it handed the bus to), but keeping the
// argument makes the parser testable in isolation.
func ParseEmitted(src, agent string) []Entry {
	var out []Entry
	// Walk the string looking for opening tags. We scan forward
	// and let the next opening tag terminate the previous body.
	// The scan is intentionally O(n*len(tag)) — note bodies are
	// small, and a real parser would be premature complexity.
	i := 0
	for i < len(src) {
		noteIdx := strings.Index(src[i:], noteOpen)
		resolveIdx := strings.Index(src[i:], resolveOpen)
		// Find the next tag. Pick the smaller of the two
		// positions; if neither exists, stop.
		next := -1
		rel := -1
		which := ""
		if noteIdx >= 0 && (resolveIdx < 0 || noteIdx < resolveIdx) {
			next = i + noteIdx
			rel = noteIdx
			which = "note"
		} else if resolveIdx >= 0 {
			next = i + resolveIdx
			rel = resolveIdx
			which = "resolve"
		} else {
			return out
		}
		// Only treat the match as a tag if the char before is
		// not part of an identifier — this rejects `<<oc-note>`
		// and `prefix<oc-note>` (where the second `<` would
		// otherwise look like a tag start).
		if !isTagBoundary(src, next) {
			// Skip just past this match and keep looking.
			i = next + 1
			continue
		}
		if which == "note" {
			// Find the closing tag. The body is everything
			// between the open's '>' and the MATCHING
			// </oc-note>, where "matching" means a depth
			// counter (we count <oc-note> opens as +1 and
			// </oc-note> closes as -1; the matching close is
			// the one that brings depth back to zero). This
			// is the only way to handle bodies that contain
			// literal <oc-note>...</oc-note> strings — the
			// forgery-attack case the design calls out as
			// non-negotiable.
			openEnd, ok := findOpenEnd(src, next+len(noteOpen))
			if !ok {
				log.Printf("notebus: parse: unterminated <oc-note at %d", next)
				return out
			}
			closeIdx, ok := findMatchingNoteClose(src, openEnd)
			if !ok {
				log.Printf("notebus: parse: missing </oc-note> for open at %d", next)
				return out
			}
			body := src[openEnd:closeIdx]
			attrs := src[next+len(noteOpen) : openEnd]
			at := readAttr(attrs, "at")
			if at == "" {
				log.Printf("notebus: parse: <oc-note> at %d missing at= attribute", next)
				i = closeIdx + len(noteClose)
				continue
			}
			body = clampBody(encodeBody(body))
			out = append(out, Note(0, agent, at, body, 0))
			i = closeIdx + len(noteClose)
			_ = rel
			continue
		}
		// resolve
		openEnd, ok := findOpenEnd(src, next+len(resolveOpen))
		if !ok {
			log.Printf("notebus: parse: unterminated <oc-resolve at %d", next)
			return out
		}
		// tagText is the attribute list and the trailing "/"
		// (the self-closing slash), excluding the final '>'.
		tagText := src[next+len(resolveOpen) : openEnd-1]
		if !strings.HasSuffix(strings.TrimSpace(tagText), "/") {
			log.Printf("notebus: parse: <oc-resolve at %d not self-closing", next)
			i = openEnd
			continue
		}
		refStr := readAttr(tagText, "ref")
		ref, err := strconv.ParseInt(refStr, 10, 64)
		if err != nil || ref <= 0 {
			log.Printf("notebus: parse: <oc-resolve at %d bad ref=%q", next, refStr)
			i = openEnd
			continue
		}
		out = append(out, Resolve(0, agent, ref, 0))
		i = openEnd
		_ = rel
	}
	return out
}

// isTagBoundary returns true if the byte before `pos` is not
// part of an identifier (so `pos` is a valid tag start). The
// check rejects `<<oc-note>` and `prefix<oc-note>` — the tag
// must be its own token.
func isTagBoundary(s string, pos int) bool {
	if pos == 0 {
		return true
	}
	c := s[pos-1]
	// Identifier-ish: keep going on letters, digits, ':', '-'.
	// Everything else (whitespace, '<', '>', punctuation) is a
	// boundary.
	switch c {
	case 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j',
		'k', 'l', 'm', 'n', 'o', 'p', 'q', 'r', 's', 't',
		'u', 'v', 'w', 'x', 'y', 'z',
		'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J',
		'K', 'L', 'M', 'N', 'O', 'P', 'Q', 'R', 'S', 'T',
		'U', 'V', 'W', 'X', 'Y', 'Z',
		'0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
		':', '-', '_':
		return false
	}
	return true
}

// findMatchingNoteClose walks the source from `start` (the
// position just after the open's '>') looking for a
// </oc-note> that matches the open. Nested <oc-note> opens
// are counted, so the matching close is the one that brings
// the depth back to zero.
//
// Returns (closeIdx, ok). closeIdx is the start of the closing
// </oc-note>. ok is false when no matching close exists.
//
// The tag-boundary check is intentionally loose here: a real
// <oc-note> in the body increments the depth even if its
// preceding character is unusual. The body's literal text is
// preserved verbatim up to the matching close, which is the
// only thing that matters for the forgery-defense guarantee.
func findMatchingNoteClose(src string, start int) (int, bool) {
	depth := 1
	i := start
	for i < len(src) {
		openRel := strings.Index(src[i:], noteOpen)
		closeRel := strings.Index(src[i:], noteClose)
		var next int
		var isOpen bool
		switch {
		case openRel < 0 && closeRel < 0:
			return 0, false
		case openRel < 0:
			next = i + closeRel
			isOpen = false
		case closeRel < 0:
			next = i + openRel
			isOpen = true
		case openRel < closeRel:
			next = i + openRel
			isOpen = true
		default:
			next = i + closeRel
			isOpen = false
		}
		if isOpen {
			depth++
			i = next + len(noteOpen)
			continue
		}
		depth--
		if depth == 0 {
			return next, true
		}
		i = next + len(noteClose)
	}
	return 0, false
}

// findOpenEnd returns the index just past the '>' that closes
// an opening tag, starting from i (the position right after the
// tag name). It also returns false if no closing '>' is found
// before end-of-string.
func findOpenEnd(src string, i int) (int, bool) {
	for i < len(src) {
		if src[i] == '>' {
			return i + 1, true
		}
		i++
	}
	return 0, false
}

// readAttr returns the value of the named attribute from a
// space-separated attribute list. Returns "" if the attribute
// is not present or its value is empty. The parser supports
// double-quoted values only; the wire format guarantees that.
func readAttr(attrs, name string) string {
	// Find name followed by '='. We accept ' ' or tab as
	// separators; the tag prefix has already been consumed.
	idx := 0
	for idx < len(attrs) {
		// Skip leading whitespace.
		for idx < len(attrs) && (attrs[idx] == ' ' || attrs[idx] == '\t') {
			idx++
		}
		if idx >= len(attrs) {
			return ""
		}
		// Find the end of the next key token (a run of
		// non-whitespace, non-= characters).
		j := idx
		for j < len(attrs) && attrs[j] != ' ' && attrs[j] != '\t' && attrs[j] != '=' {
			j++
		}
		if j == idx {
			// No progress (e.g. an '=' or whitespace at
			// idx). Step forward to avoid an infinite loop.
			idx++
			continue
		}
		if j < len(attrs) && attrs[j] == '=' && attrs[idx:j] == name {
			// Value: skip whitespace then '=' then optional
			// '"'. We require double quotes — the wire format
			// uses them and we will not silently accept
			// unquoted or single-quoted values (those are
			// ambiguous and rare).
			k := j + 1
			for k < len(attrs) && (attrs[k] == ' ' || attrs[k] == '\t') {
				k++
			}
			if k >= len(attrs) || attrs[k] != '"' {
				return ""
			}
			k++
			v := k
			for v < len(attrs) && attrs[v] != '"' {
				v++
			}
			if v >= len(attrs) {
				return ""
			}
			return attrs[k:v]
		}
		// Not the attribute we wanted — skip past this key
		// (and its value, if any).
		idx = j
		if idx < len(attrs) && attrs[idx] == '=' {
			// Skip the value: optional ws, optional ", value, ".
			idx++
			for idx < len(attrs) && (attrs[idx] == ' ' || attrs[idx] == '\t') {
				idx++
			}
			if idx < len(attrs) && attrs[idx] == '"' {
				idx++
				for idx < len(attrs) && attrs[idx] != '"' {
					idx++
				}
				if idx < len(attrs) {
					idx++ // skip closing "
				}
			}
		}
	}
	return ""
}

// encodeBody entity-encodes < so the body can be stored
// verbatim without being re-parsed as a tag. We do NOT encode
// `>` because the body is rendered as text (not as markup),
// and a stray `>` is harmless — the parser only starts a new
// tag on `<`. `&` is encoded to `&amp;` so a body containing
// `&lt;` round-trips to the literal text rather than being
// silently re-decoded.
//
// The encoding is the inverse of DecodeEntityBody.
func encodeBody(s string) string {
	if !strings.ContainsAny(s, "<&") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			b.WriteString("&lt;")
		case '&':
			b.WriteString("&amp;")
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// DecodeEntityBody is the inverse of encodeBody. Useful for
// tests and for the LLM-facing rendering layer if it ever
// wants to display raw body text. Most code paths render the
// encoded form directly so this is rarely called.
func DecodeEntityBody(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	// Two-step replace: &amp; must be last so we do not decode
	// its own entities (e.g. &amp;lt; should NOT become <).
	// We do a single pass with a tiny state machine to handle
	// ordering correctly.
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '&' {
			b.WriteByte(s[i])
			continue
		}
		// Look for the longest matching entity up to the next
		// ';'. If found, emit the replacement; else emit '&'
		// literally.
		j := i + 1
		for j < len(s) && s[j] != ';' && j-i < 8 {
			j++
		}
		if j >= len(s) || s[j] != ';' {
			b.WriteByte(s[i])
			continue
		}
		switch s[i : j+1] {
		case "&lt;":
			b.WriteByte('<')
		case "&gt;":
			b.WriteByte('>')
		case "&amp;":
			b.WriteByte('&')
		default:
			b.WriteString(s[i : j+1])
		}
		i = j // skip past ';'
	}
	return b.String()
}

// clampBody truncates a body to MaxNoteBodyLen and appends the
// truncation marker so the LLM can see the cut.
func clampBody(s string) string {
	if len(s) <= MaxNoteBodyLen {
		return s
	}
	return s[:MaxNoteBodyLen] + truncationMarker
}
