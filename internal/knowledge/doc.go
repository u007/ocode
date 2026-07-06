// Package knowledge implements the OKF (Open Knowledge Format) bundle system for
// ocode. It provides frontmatter-aware markdown document parsing with round-trip
// preservation of unknown frontmatter keys, bundle detection and enumeration, and
// index/log management.
package knowledge

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Doc represents a single OKF knowledge document. Conforming documents have
// well-formed YAML frontmatter delimited by "---" markers. Non-conforming files
// (no frontmatter, bad YAML) are tolerated — Conforming is false, and Body
// contains the full file content.
type Doc struct {
	// Path is the bundle-relative path to the document.
	Path string

	// Type is the document's type (e.g. "concept", "guide", "playbook").
	// Required for conforming docs.
	Type string `yaml:"type"`

	// Title is a human-readable name for the document.
	Title string `yaml:"title"`

	// Description summarises the document's content.
	Description string `yaml:"description"`

	// Resource is an optional URL or reference to related material.
	Resource string `yaml:"resource"`

	// Tags are free-form labels for categorisation.
	Tags []string `yaml:"tags"`

	// Timestamp records when the document was last meaningfully updated.
	Timestamp time.Time `yaml:"timestamp"`

	// Status is empty for active docs, or "deprecated".
	Status string `yaml:"status"`

	// DeprecatedReason explains a deprecation.
	DeprecatedReason string `yaml:"deprecated_reason"`

	// Extra preserves unknown frontmatter keys as a yaml.Node MappingNode,
	// maintaining their original order through Parse→Render cycles.
	Extra *yaml.Node `yaml:",omitempty"`

	// Body is the content after the frontmatter block (or the entire file
	// content for non-conforming documents).
	Body string

	// Conforming is true when the document has parseable YAML frontmatter.
	Conforming bool

	// rawFM stores the original frontmatter bytes (non-conforming docs).
	rawFM []byte

	// fmNode preserves the original frontmatter YAML node tree for
	// byte-stable round-trip rendering.
	fmNode *yaml.Node
}

// ParseDoc parses a markdown document at relPath with the given raw content.
//
// It never returns an error for missing or unparseable frontmatter — those
// produce a non-conforming Doc with Conforming=false and Body set to the full
// content. Errors are only returned for I/O-level impossibilities (which are
// none for this in-memory function, so ParseDoc returns nil error in practice).
func ParseDoc(relPath string, raw []byte) (*Doc, error) {
	doc := &Doc{
		Path:       relPath,
		Body:       string(raw),
		Conforming: false,
	}

	fmBytes, body, ok := extractFrontmatter(raw)
	if !ok {
		slog.Debug("knowledge: no frontmatter found", "path", relPath)
		return doc, nil
	}

	doc.Body = body
	doc.rawFM = fmBytes

	// Parse frontmatter YAML into a node tree.
	var node yaml.Node
	if err := yaml.Unmarshal(fmBytes, &node); err != nil {
		slog.Debug("knowledge: unparseable frontmatter YAML", "path", relPath, "err", err)
		return doc, nil
	}

	// Unwrap DocumentNode to get the root mapping.
	fmNode := &node
	if fmNode.Kind == yaml.DocumentNode && len(fmNode.Content) > 0 {
		fmNode = fmNode.Content[0]
	}
	if fmNode.Kind != yaml.MappingNode {
		slog.Debug("knowledge: frontmatter root is not a mapping", "path", relPath, "kind", fmNode.Kind)
		return doc, nil
	}

	doc.Conforming = true
	doc.rawFM = nil // We store the node tree instead for round-trip

	// Store a deep copy of the original node tree for round-trip rendering.
	doc.fmNode = deepCopyNode(fmNode)

	// Extract known fields and collect unknown keys.
	var unknownContent []*yaml.Node

	for i := 0; i < len(fmNode.Content); i += 2 {
		keyNode := fmNode.Content[i]
		valNode := fmNode.Content[i+1]

		switch keyNode.Value {
		case "type":
			if err := valNode.Decode(&doc.Type); err != nil {
				slog.Debug("knowledge: failed to decode type", "path", relPath, "err", err)
			}
		case "title":
			if err := valNode.Decode(&doc.Title); err != nil {
				slog.Debug("knowledge: failed to decode title", "path", relPath, "err", err)
			}
		case "description":
			if err := valNode.Decode(&doc.Description); err != nil {
				slog.Debug("knowledge: failed to decode description", "path", relPath, "err", err)
			}
		case "resource":
			if err := valNode.Decode(&doc.Resource); err != nil {
				slog.Debug("knowledge: failed to decode resource", "path", relPath, "err", err)
			}
		case "tags":
			if err := valNode.Decode(&doc.Tags); err != nil {
				slog.Debug("knowledge: failed to decode tags", "path", relPath, "err", err)
			}
		case "timestamp":
			if err := valNode.Decode(&doc.Timestamp); err != nil {
				slog.Debug("knowledge: failed to decode timestamp", "path", relPath, "err", err)
			}
		case "status":
			if err := valNode.Decode(&doc.Status); err != nil {
				slog.Debug("knowledge: failed to decode status", "path", relPath, "err", err)
			}
		case "deprecated_reason":
			if err := valNode.Decode(&doc.DeprecatedReason); err != nil {
				slog.Debug("knowledge: failed to decode deprecated_reason", "path", relPath, "err", err)
			}
		default:
			// Clone the key-value pair for Extra (these are kept separate from
			// fmNode, which retains all keys for round-trip rendering).
			unknownContent = append(unknownContent, deepCopyNode(keyNode), deepCopyNode(valNode))
		}
	}

	if len(unknownContent) > 0 {
		doc.Extra = &yaml.Node{
			Kind:    yaml.MappingNode,
			Tag:     "!!map",
			Content: unknownContent,
		}
	}

	return doc, nil
}

// Render serialises the document back to bytes. For conforming docs it produces
// frontmatter delimiters, YAML frontmatter (known fields + Extra unknown keys),
// and body content. For non-conforming docs it returns the body unchanged.
// Render serialises the document back to bytes. For conforming docs it produces
// frontmatter delimiters, YAML frontmatter (known fields + Extra unknown keys),
// and body content. For non-conforming docs it returns the body unchanged.
//
// A Parse→Render cycle of any conforming doc must produce byte-stable output
// for frontmatter keys that were not modified.
func (d *Doc) Render() ([]byte, error) {
	if !d.Conforming || d.rawFM == nil && d.fmNode == nil {
		// Non-conforming docs always return body unchanged.
		if !d.Conforming {
			return []byte(d.Body), nil
		}
		// Conforming doc with no frontmatter source: build from known fields.
		return d.renderFromFields()
	}

	// If we have the original frontmatter bytes, start from those for
	// maximum byte-stability.
	var fmSrc *yaml.Node
	if d.rawFM != nil {
		var node yaml.Node
		if err := yaml.Unmarshal(d.rawFM, &node); err != nil {
			return nil, fmt.Errorf("knowledge: re-marshal error: %w", err)
		}
		fmSrc = &node
	} else if d.fmNode != nil {
		fmSrc = deepCopyNode(d.fmNode)
	} else {
		return nil, fmt.Errorf("knowledge: cannot render doc with no frontmatter source")
	}

	// Update known field values in the cloned node tree.
	existingKeys := make(map[string]bool, len(fmSrc.Content)/2)
	for i := 0; i < len(fmSrc.Content); i += 2 {
		keyNode := fmSrc.Content[i]
		valNode := fmSrc.Content[i+1]
		key := keyNode.Value
		existingKeys[key] = true

		switch key {
		case "type":
			if err := setNodeValue(valNode, d.Type); err != nil {
				slog.Debug("knowledge: failed to set type in render", "err", err)
			}
		case "title":
			if err := setNodeValue(valNode, d.Title); err != nil {
				slog.Debug("knowledge: failed to set title in render", "err", err)
			}
		case "description":
			if err := setNodeValue(valNode, d.Description); err != nil {
				slog.Debug("knowledge: failed to set description in render", "err", err)
			}
		case "resource":
			if err := setNodeValue(valNode, d.Resource); err != nil {
				slog.Debug("knowledge: failed to set resource in render", "err", err)
			}
		case "tags":
			if err := setNodeSequence(valNode, d.Tags); err != nil {
				slog.Debug("knowledge: failed to set tags in render", "err", err)
			}
		case "timestamp":
			if err := setNodeTimestamp(valNode, d.Timestamp); err != nil {
				slog.Debug("knowledge: failed to set timestamp in render", "err", err)
			}
		case "status":
			if err := setNodeValue(valNode, d.Status); err != nil {
				slog.Debug("knowledge: failed to set status in render", "err", err)
			}
		case "deprecated_reason":
			if err := setNodeValue(valNode, d.DeprecatedReason); err != nil {
				slog.Debug("knowledge: failed to set deprecated_reason in render", "err", err)
			}
		}
	}

	// Add new non-empty known fields that are not already in the frontmatter.
	// This handles the case where a field is being set for the first time.
	var newContent []*yaml.Node
	addIfMissing := func(key, value string) {
		if value == "" || existingKeys[key] {
			return
		}
		newContent = append(newContent,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: "!!str"},
		)
	}
	addTagsIfMissing := func(key string, tags []string) {
		if len(tags) == 0 || existingKeys[key] {
			return
		}
		tagNodes := make([]*yaml.Node, len(tags))
		for i, tag := range tags {
			tagNodes[i] = &yaml.Node{Kind: yaml.ScalarNode, Value: tag, Tag: "!!str"}
		}
		newContent = append(newContent,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"},
			&yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: tagNodes},
		)
	}
	addTimestampIfMissing := func(key string, ts time.Time) {
		if ts.IsZero() || existingKeys[key] {
			return
		}
		tsStr := ts.Format(time.RFC3339)
		newContent = append(newContent,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: tsStr, Tag: "!!timestamp"},
		)
	}

	addIfMissing("type", d.Type)
	addIfMissing("title", d.Title)
	addIfMissing("description", d.Description)
	addIfMissing("resource", d.Resource)
	addTagsIfMissing("tags", d.Tags)
	addTimestampIfMissing("timestamp", d.Timestamp)
	addIfMissing("status", d.Status)
	addIfMissing("deprecated_reason", d.DeprecatedReason)

	if len(newContent) > 0 {
		fmSrc.Content = append(fmSrc.Content, newContent...)
	}

	return d.renderNodeTree(fmSrc)
}

// renderFromFields builds frontmatter from Doc's known fields + Extra for
func (d *Doc) renderFromFields() ([]byte, error) {
	content := make([]*yaml.Node, 0, 20)

	// Known fields in canonical order.
	addFMField := func(key, value string) {
		if value == "" {
			return
		}
		content = append(content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: "!!str"},
		)
	}

	addFMField("type", d.Type)
	addFMField("title", d.Title)
	addFMField("description", d.Description)
	addFMField("resource", d.Resource)

	// Tags (sequence).
	if len(d.Tags) > 0 {
		tagNodes := make([]*yaml.Node, len(d.Tags))
		for i, tag := range d.Tags {
			tagNodes[i] = &yaml.Node{Kind: yaml.ScalarNode, Value: tag, Tag: "!!str"}
		}
		content = append(content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "tags", Tag: "!!str"},
			&yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: tagNodes},
		)
	}

	// Timestamp.
	if !d.Timestamp.IsZero() {
		tsStr := d.Timestamp.Format(time.RFC3339)
		content = append(content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "timestamp", Tag: "!!str"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: tsStr, Tag: "!!timestamp"},
		)
	}

	// Status and deprecated_reason.
	addFMField("status", d.Status)
	addFMField("deprecated_reason", d.DeprecatedReason)

	// Extra unknown keys (preserved order).
	if d.Extra != nil && len(d.Extra.Content) > 0 {
		for i := 0; i < len(d.Extra.Content); i += 2 {
			keyNode := deepCopyNode(d.Extra.Content[i])
			valNode := deepCopyNode(d.Extra.Content[i+1])
			content = append(content, keyNode, valNode)
		}
	}

	fmNode := &yaml.Node{
		Kind:    yaml.MappingNode,
		Tag:     "!!map",
		Content: content,
	}

	return d.renderNodeTree(fmNode)
}

// renderNodeTree encodes a yaml.Node tree as frontmatter-delimited output.
func (d *Doc) renderNodeTree(fmSrc *yaml.Node) ([]byte, error) {
	// Encode with 2-space indent.
	var encBuf bytes.Buffer
	encoder := yaml.NewEncoder(&encBuf)
	encoder.SetIndent(2)
	if err := encoder.Encode(fmSrc); err != nil {
		return nil, fmt.Errorf("knowledge: marshal error: %w", err)
	}
	encoder.Close()

	fmBytes := encBuf.Bytes()

	// Build output: ---\n + frontmatter + \n---\n + body
	var buf strings.Builder
	buf.WriteString("---\n")
	buf.Write(fmBytes)
	buf.WriteString("---\n")
	buf.WriteString(d.Body)
	return []byte(buf.String()), nil
}

// deepCopyNode recursively copies a yaml.Node tree, preserving value, tag,
// style, and content structure. This is needed because yaml.Nodes share
// pointers and we need independent trees for parallel access.
func deepCopyNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := &yaml.Node{
		Kind:        n.Kind,
		Tag:         n.Tag,
		Value:       n.Value,
		Style:       n.Style,
		Line:        n.Line,
		Column:      n.Column,
		HeadComment: n.HeadComment,
		LineComment: n.LineComment,
		FootComment: n.FootComment,
	}
	if len(n.Content) > 0 {
		c.Content = make([]*yaml.Node, len(n.Content))
		for i, child := range n.Content {
			c.Content[i] = deepCopyNode(child)
		}
	}
	if n.Alias != nil {
		c.Alias = deepCopyNode(n.Alias)
	}
	return c
}

// extractFrontmatter splits raw markdown bytes into frontmatter YAML (between
// the first "---\n" and second "\n---" delimiters) and the body (everything
// after the closing delimiter). Returns (nil, fullContent, false) when no
// frontmatter is found.
//
// Handles edge cases safely:
//   - "---\n---" (closing delimiter at EOF, no trailing newline)
//   - "---\n---\n" (closing delimiter at EOF with trailing newline)
//   - Empty frontmatter ("---\n---\nbody")
//   - CRLF line endings ("---\r\n...\r\n---\r\nbody")
func extractFrontmatter(raw []byte) ([]byte, string, bool) {
	s := string(raw)

	// Check for opening --- on the first line.
	if !strings.HasPrefix(s, "---\n") && s != "---\n" && !strings.HasPrefix(s, "---\r\n") {
		return nil, s, false
	}

	// Find the first newline to determine the line ending style and offset.
	firstNl := strings.Index(s, "\n")
	if firstNl < 0 {
		return nil, s, false
	}

	rest := s[firstNl+1:]

	// Find the closing delimiter. Track the delimiter length to correctly compute
	// the body start (5 for "\n---\n", 6 for "\n---\r\n", 3 for "---" at EOF).
	var closingIdx int
	var delimLen int

	closingIdx = strings.Index(rest, "\n---\n")
	if closingIdx >= 0 {
		delimLen = 5
	} else {
		closingIdx = strings.Index(rest, "\n---\r\n")
		if closingIdx >= 0 {
			delimLen = 6
		} else if strings.HasPrefix(rest, "---") {
			// Closing "---" at the very start of rest (no preceding frontmatter
			// content). The delimiter length is just the 3 dashes.
			closingIdx = 0
			delimLen = 3
		} else {
			return nil, s, false
		}
	}

	fmBytes := []byte(rest[:closingIdx])

	bodyStart := closingIdx + delimLen
	// Clamp to len(rest) when the closing delimiter trails off the end (e.g.
	// "---\n---" where rest is just "---" of length 3).
	if bodyStart > len(rest) {
		bodyStart = len(rest)
	}
	body := rest[bodyStart:]

	return fmBytes, body, true
}

// setNodeValue sets a yaml scalar node's value from a Go string, preserving
// the node's tag and style.
func setNodeValue(n *yaml.Node, v string) error {
	if n.Kind != yaml.ScalarNode {
		n.Kind = yaml.ScalarNode
	}
	n.Value = v
	if n.Tag == "" {
		n.Tag = "!!str"
	}
	return nil
}

// setNodeTimestamp sets a yaml scalar node's value from a time.Time, using
// RFC3339 format, with the !!timestamp tag.
func setNodeTimestamp(n *yaml.Node, t time.Time) error {
	if n.Kind != yaml.ScalarNode {
		n.Kind = yaml.ScalarNode
	}
	if t.IsZero() {
		n.Value = ""
		n.Tag = "!!null"
		return nil
	}
	n.Value = t.Format(time.RFC3339)
	n.Tag = "!!timestamp"
	return nil
}

// setNodeSequence rebuilds a yaml sequence node's content from a string slice.
func setNodeSequence(n *yaml.Node, vals []string) error {
	n.Kind = yaml.SequenceNode
	n.Tag = "!!seq"
	n.Content = make([]*yaml.Node, len(vals))
	for i, v := range vals {
		n.Content[i] = &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: v,
			Tag:   "!!str",
		}
	}
	return nil
}
