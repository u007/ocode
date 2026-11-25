package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// toolDigestMaxPageBytes is the largest parameter window one page may return.
const toolDigestMaxPageBytes = 8192

// toolDigestPayload is the canonical, MASKED form of one tool call's
// parameters. Every byte is pageable: page never clips, it only windows.
type toolDigestPayload struct {
	canonical  []byte
	totalBytes int
	sha256     string
	fieldBytes map[string]int
}

// toolDigestPage is one window of a toolDigestPayload.
type toolDigestPage struct {
	offset, nextOffset, totalBytes int
	sha256                         string
	remainingBytes                 int
	text                           string
}

// buildToolDigestPayload validates in.Args, masks every string and object key
// with mask, and returns the canonical bytes. The hash covers those masked
// bytes, never the raw input. Duplicate keys are rejected before masking, and a
// masked-key collision is rejected after it.
//
// Canonical form: the object {"tool","command","args","rule","scope"} in that
// order, with args encoded using sorted nested keys and HTML escaping off.
// The raw Args value is authoritative; Command is never used to rewrite it.
func buildToolDigestPayload(in ToolCallDigestInput, mask func(string) string) (*toolDigestPayload, error) {
	if mask == nil {
		return nil, errors.New("tool digest payload: mask is required")
	}
	args, err := toolDigestArgsValue(in.Args)
	if err != nil {
		return nil, fmt.Errorf("tool digest payload: invalid args: %w", err)
	}
	maskedArgs, err := toolDigestMaskValue(args, mask)
	if err != nil {
		return nil, fmt.Errorf("tool digest payload: %w", err)
	}

	parts := []struct {
		name  string
		value interface{}
	}{
		{"tool", mask(in.Tool)},
		{"command", mask(in.Command)},
		{"args", maskedArgs},
		{"rule", mask(in.Rule)},
		{"scope", mask(in.Scope)},
	}

	var canonical bytes.Buffer
	canonical.WriteByte('{')
	fieldBytes := make(map[string]int, len(parts))
	for i, part := range parts {
		value, err := toolDigestEncode(part.value)
		if err != nil {
			return nil, fmt.Errorf("tool digest payload: encode %s: %w", part.name, err)
		}
		key, err := toolDigestEncode(part.name)
		if err != nil {
			return nil, fmt.Errorf("tool digest payload: encode key %s: %w", part.name, err)
		}
		if i > 0 {
			canonical.WriteByte(',')
		}
		canonical.Write(key)
		canonical.WriteByte(':')
		canonical.Write(value)
		fieldBytes[part.name] = len(value)
	}
	canonical.WriteByte('}')

	sum := sha256.Sum256(canonical.Bytes())
	return &toolDigestPayload{
		canonical:  canonical.Bytes(),
		totalBytes: canonical.Len(),
		sha256:     hex.EncodeToString(sum[:]),
		fieldBytes: fieldBytes,
	}, nil
}

// page returns the window [offset, offset+maxBytes) of the canonical payload,
// shortened only to a UTF-8 boundary. It errors on an out-of-range offset or
// window, on an offset inside a rune, and on a window too small to advance.
// An offset equal to the total returns an empty final page.
func (p *toolDigestPayload) page(offset, maxBytes int) (toolDigestPage, error) {
	total := len(p.canonical)
	if maxBytes <= 0 || maxBytes > toolDigestMaxPageBytes {
		return toolDigestPage{}, fmt.Errorf("tool digest page: window %d outside 1..%d", maxBytes, toolDigestMaxPageBytes)
	}
	if offset < 0 || offset > total {
		return toolDigestPage{}, fmt.Errorf("tool digest page: offset %d outside 0..%d", offset, total)
	}
	if offset < total && !utf8.RuneStart(p.canonical[offset]) {
		return toolDigestPage{}, fmt.Errorf("tool digest page: offset %d splits a UTF-8 sequence", offset)
	}
	end := offset + maxBytes
	if end > total {
		end = total
	}
	if end < total {
		for end > offset && !utf8.RuneStart(p.canonical[end]) {
			end--
		}
		if end == offset {
			return toolDigestPage{}, fmt.Errorf("tool digest page: window %d cannot hold the UTF-8 sequence at offset %d", maxBytes, offset)
		}
	}
	return toolDigestPage{
		offset:         offset,
		nextOffset:     end,
		totalBytes:     total,
		sha256:         p.sha256,
		remainingBytes: total - end,
		text:           string(p.canonical[offset:end]),
	}, nil
}

// toolDigestArgsValue validates raw as exactly one JSON value and returns it
// decoded with UseNumber. Duplicate keys at any depth and trailing data are
// rejected. An absent or blank value is JSON null.
func toolDigestArgsValue(raw json.RawMessage) (interface{}, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := scanToolDigestJSON(dec); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after the JSON value")
	}
	var v interface{}
	dec = json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// scanToolDigestJSON consumes one JSON value from dec token by token and fails
// on any object with a repeated key, at any depth.
func scanToolDigestJSON(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyTok.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, dup := seen[key]; dup {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanToolDigestJSON(dec); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
	case '[':
		for dec.More() {
			if err := scanToolDigestJSON(dec); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
	return nil
}

// toolDigestMaskValue returns v with mask applied to every string value and
// every object key, recursively. Two keys that mask to the same text are an
// error, since the merged object would have lost one of them.
func toolDigestMaskValue(v interface{}, mask func(string) string) (interface{}, error) {
	switch x := v.(type) {
	case string:
		return mask(x), nil
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			mk := mask(k)
			if _, dup := out[mk]; dup {
				return nil, fmt.Errorf("masked object key collision on %q", mk)
			}
			mv, err := toolDigestMaskValue(val, mask)
			if err != nil {
				return nil, err
			}
			out[mk] = mv
		}
		return out, nil
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			mv, err := toolDigestMaskValue(val, mask)
			if err != nil {
				return nil, err
			}
			out[i] = mv
		}
		return out, nil
	default:
		return v, nil
	}
}

// toolDigestEncode marshals v without HTML escaping and without the trailing
// newline that Encoder adds, so the bytes match the canonical form exactly.
func toolDigestEncode(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
