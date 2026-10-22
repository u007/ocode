package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Byte-preserving edits of one top-level key in a JSON(C) object.
//
// Re-marshalling a whole settings file through map[string]json.RawMessage
// re-sorts every key and re-indents every nested value, so toggling one setting
// would rewrite a hand-formatted, possibly git-tracked file. These helpers
// locate the member's byte span and splice only that span: every other byte,
// comments included, is left exactly as the user wrote it.

// jsoncCommentRe matches either a quoted string (to skip it) or a comment.
// Shares its shape with jsoncComments in config.go.
var jsoncCommentRe = jsoncComments

// blankJSONCComments replaces // and /* */ comments with spaces (keeping
// newlines) so the result parses as plain JSON yet has the SAME length and
// offsets as the input. stripJSONCComments deletes comments instead, which
// shifts offsets and is unusable for splicing the original bytes.
func blankJSONCComments(data []byte) []byte {
	return jsoncCommentRe.ReplaceAllFunc(data, func(match []byte) []byte {
		if match[0] == '"' {
			return match
		}
		out := make([]byte, len(match))
		for i, b := range match {
			if b == '\n' || b == '\r' {
				out[i] = b
			} else {
				out[i] = ' '
			}
		}
		return out
	})
}

type jsonMember struct {
	key                        string
	keyStart, valStart, valEnd int
}

// scanTopLevelMembers returns the byte spans of the members of the top-level
// object in clean (comment-free JSON), plus the offsets of its braces.
func scanTopLevelMembers(clean []byte) (members []jsonMember, open, closeIdx int, err error) {
	dec := json.NewDecoder(bytes.NewReader(clean))
	tok, err := dec.Token()
	if err != nil {
		return nil, 0, 0, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, 0, 0, fmt.Errorf("settings must be a JSON object")
	}
	open = int(dec.InputOffset()) - 1
	for dec.More() {
		ktok, err := dec.Token()
		if err != nil {
			return nil, 0, 0, err
		}
		key, ok := ktok.(string)
		if !ok {
			return nil, 0, 0, fmt.Errorf("unexpected token %v in object", ktok)
		}
		keyEnd := int(dec.InputOffset())
		var rm json.RawMessage
		if err := dec.Decode(&rm); err != nil {
			return nil, 0, 0, err
		}
		valEnd := int(dec.InputOffset())
		members = append(members, jsonMember{
			key:      key,
			keyStart: openingQuote(clean, keyEnd-1),
			valStart: valEnd - len(rm),
			valEnd:   valEnd,
		})
	}
	if _, err := dec.Token(); err != nil { // closing brace
		return nil, 0, 0, err
	}
	closeIdx = int(dec.InputOffset()) - 1
	return members, open, closeIdx, nil
}

// openingQuote returns the offset of the quote opening the string whose closing
// quote is at closeQuote, skipping escaped quotes.
func openingQuote(b []byte, closeQuote int) int {
	for i := closeQuote - 1; i >= 0; i-- {
		if b[i] != '"' {
			continue
		}
		backslashes := 0
		for j := i - 1; j >= 0 && b[j] == '\\'; j-- {
			backslashes++
		}
		if backslashes%2 == 0 {
			return i
		}
	}
	return 0
}

// lineIndent returns the leading whitespace of the line holding offset pos when
// that whitespace is the only thing before pos on the line, and ok=false when
// pos shares its line with other text (a single-line object).
func lineIndent(data []byte, pos int) (indent string, ok bool) {
	nl := bytes.LastIndexByte(data[:pos], '\n')
	if nl < 0 {
		return "", false
	}
	seg := string(data[nl+1 : pos])
	if strings.TrimLeft(seg, " \t") != "" {
		return "", false
	}
	return seg, true
}

// spliceTopLevelKey sets (or, with remove, deletes) one top-level key of the
// JSON object in data, leaving every other byte untouched. data may carry //
// and /* */ comments. An empty data is treated as "{}". A malformed object is
// an error, never overwritten.
func spliceTopLevelKey(data []byte, key string, v any, remove bool) ([]byte, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		if remove {
			return data, nil
		}
		data = []byte("{}")
	}
	clean := blankJSONCComments(data)
	members, open, closeIdx, err := scanTopLevelMembers(clean)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i, m := range members {
		if m.key == key {
			idx = i // duplicate keys: the last one wins, as in encoding/json
		}
	}

	render := func(indent string, multiline bool) ([]byte, error) {
		if multiline {
			return json.MarshalIndent(v, indent, "  ")
		}
		return json.Marshal(v)
	}
	quotedKey, _ := json.Marshal(key)

	switch {
	case idx >= 0 && remove:
		m := members[idx]
		var from, to int
		switch {
		case idx < len(members)-1:
			from, to = m.keyStart, members[idx+1].keyStart
		case idx > 0:
			from, to = members[idx-1].valEnd, m.valEnd
		default:
			from, to = open+1, closeIdx
		}
		return append(append([]byte{}, data[:from]...), data[to:]...), nil

	case idx >= 0:
		m := members[idx]
		indent, multiline := lineIndent(data, m.keyStart)
		val, err := render(indent, multiline)
		if err != nil {
			return nil, err
		}
		return append(append(append([]byte{}, data[:m.valStart]...), val...), data[m.valEnd:]...), nil

	case remove:
		return data, nil

	case len(members) == 0:
		val, err := render("  ", true)
		if err != nil {
			return nil, err
		}
		var b bytes.Buffer
		b.Write(data[:open+1])
		fmt.Fprintf(&b, "\n  %s: %s\n", quotedKey, val)
		b.Write(data[closeIdx:])
		return b.Bytes(), nil

	default:
		last := members[len(members)-1]
		indent, multiline := lineIndent(data, last.keyStart)
		val, err := render(indent, multiline)
		if err != nil {
			return nil, err
		}
		var b bytes.Buffer
		b.Write(data[:last.valEnd])
		if multiline {
			fmt.Fprintf(&b, ",\n%s%s: %s", indent, quotedKey, val)
		} else {
			fmt.Fprintf(&b, ", %s: %s", quotedKey, val)
		}
		b.Write(data[last.valEnd:])
		return b.Bytes(), nil
	}
}
