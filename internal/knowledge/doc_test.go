package knowledge

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// conformingDoc is a well-formed OKF document with all recommended fields
// plus two unknown keys (one scalar, one mapping).
const conformingDoc = `---
type: concept
title: My Test Doc
description: A test document for OKF parsing
resource: https://example.com/docs/test
tags:
  - go
  - testing
timestamp: 2024-01-15T10:00:00Z
status: deprecated
deprecated_reason: No longer needed
custom_key: custom_value
another_key:
  nested: true
subkey: trailing
---
This is the body content.

It has multiple paragraphs.
`

func TestParseConformingDoc(t *testing.T) {
	doc, err := ParseDoc("test/doc.md", []byte(conformingDoc))
	if err != nil {
		t.Fatalf("ParseDoc returned unexpected error: %v", err)
	}
	if !doc.Conforming {
		t.Fatal("expected Conforming=true for a well-formed document")
	}
	if doc.Path != "test/doc.md" {
		t.Errorf("Path = %q, want %q", doc.Path, "test/doc.md")
	}
	if doc.Type != "concept" {
		t.Errorf("Type = %q, want %q", doc.Type, "concept")
	}
	if doc.Title != "My Test Doc" {
		t.Errorf("Title = %q, want %q", doc.Title, "My Test Doc")
	}
	if doc.Description != "A test document for OKF parsing" {
		t.Errorf("Description = %q, want %q", doc.Description, "A test document for OKF parsing")
	}
	if doc.Resource != "https://example.com/docs/test" {
		t.Errorf("Resource = %q, want %q", doc.Resource, "https://example.com/docs/test")
	}
	if len(doc.Tags) != 2 || doc.Tags[0] != "go" || doc.Tags[1] != "testing" {
		t.Errorf("Tags = %v, want [go testing]", doc.Tags)
	}
	expectedTime, _ := time.Parse(time.RFC3339, "2024-01-15T10:00:00Z")
	if !doc.Timestamp.Equal(expectedTime) {
		t.Errorf("Timestamp = %v, want %v", doc.Timestamp, expectedTime)
	}
	if doc.Status != "deprecated" {
		t.Errorf("Status = %q, want %q", doc.Status, "deprecated")
	}
	if doc.DeprecatedReason != "No longer needed" {
		t.Errorf("DeprecatedReason = %q, want %q", doc.DeprecatedReason, "No longer needed")
	}
	if doc.Body != "This is the body content.\n\nIt has multiple paragraphs.\n" {
		t.Errorf("Body = %q, want %q", doc.Body, "This is the body content.\n\nIt has multiple paragraphs.\n")
	}

	// Extra must contain the unknown keys: custom_key, another_key, subkey
	if doc.Extra == nil {
		t.Fatal("Extra is nil, expected unknown keys")
	}
	if doc.Extra.Kind != yaml.MappingNode {
		t.Fatalf("Extra.Kind = %d, want MappingNode", doc.Extra.Kind)
	}

	// Collect unknown key names from Extra
	unknownKeys := make(map[string]bool)
	for i := 0; i < len(doc.Extra.Content); i += 2 {
		unknownKeys[doc.Extra.Content[i].Value] = true
	}
	for _, k := range []string{"custom_key", "another_key", "subkey"} {
		if !unknownKeys[k] {
			t.Errorf("Extra missing unknown key %q", k)
		}
	}
}

func TestRenderPreservesUnknownKeysAndOrder(t *testing.T) {
	doc, err := ParseDoc("roundtrip.md", []byte(conformingDoc))
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}

	// Render without modifying anything
	out, err := doc.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Parse again to verify the round-trip preserved unknown keys
	doc2, err := ParseDoc("roundtrip.md", out)
	if err != nil {
		t.Fatalf("second ParseDoc: %v", err)
	}

	if doc2.Extra == nil {
		t.Fatal("Extra is nil after round-trip")
	}

	// Verify unknown key names preserved
	unknownKeys := make([]string, 0)
	for i := 0; i < len(doc2.Extra.Content); i += 2 {
		unknownKeys = append(unknownKeys, doc2.Extra.Content[i].Value)
	}

	// Order of unknown keys must be: custom_key, another_key, subkey
	// (they appear after known fields in the original)
	if len(unknownKeys) != 3 {
		t.Fatalf("got %d unknown keys, want 3: %v", len(unknownKeys), unknownKeys)
	}
	wantOrder := []string{"custom_key", "another_key", "subkey"}
	for i, want := range wantOrder {
		if unknownKeys[i] != want {
			t.Errorf("unknown key[%d] = %q, want %q", i, unknownKeys[i], want)
		}
	}
}

func TestParseFrontmatterLessFile(t *testing.T) {
	input := []byte("# Just a regular markdown file\n\nWithout frontmatter.\n")
	doc, err := ParseDoc("plain.md", input)
	if err != nil {
		t.Fatalf("ParseDoc returned unexpected error: %v", err)
	}
	if doc.Conforming {
		t.Fatal("expected Conforming=false for a file without frontmatter")
	}
	if doc.Body != string(input) {
		t.Errorf("Body = %q, want the entire input as Body", doc.Body)
	}
	if doc.Type != "" || doc.Title != "" {
		t.Error("expected empty frontmatter fields for non-conforming doc")
	}
}

func TestParseBrokenYAML(t *testing.T) {
	input := []byte("---\ntype: concept\ntitle: \"bad quotes\n  - missing\n---\nBody text")
	doc, err := ParseDoc("broken.md", input)
	if err != nil {
		t.Fatalf("ParseDoc returned unexpected error: %v", err)
	}
	if doc.Conforming {
		t.Fatal("expected Conforming=false for broken YAML frontmatter")
	}
	if doc.Body != "Body text" {
		t.Errorf("Body = %q, want %q", doc.Body, "Body text")
	}
}

func TestRoundTripTimestampOnlyChange(t *testing.T) {
	input := `---
type: concept
title: Stable Doc
description: This doc tests byte-stable round-trip
resource: https://example.com
tags:
  - stable
timestamp: 2024-06-01T12:00:00Z
custom_field: keep-me
another_unknown:
  deep: true
---
Body content here.
`
	doc, err := ParseDoc("stable.md", []byte(input))
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}

	// Change only the Timestamp
	newTime, _ := time.Parse(time.RFC3339, "2025-01-01T00:00:00Z")
	doc.Timestamp = newTime

	out, err := doc.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	outStr := string(out)
	inLines := strings.Split(input, "\n")
	outLines := strings.Split(outStr, "\n")

	// Compare every frontmatter line (between --- delimiters)
	// Lines that are NOT the timestamp line must be byte-identical
	inFrontmatter := false
	outFrontmatter := false
	inIdx := 0
	outIdx := 0

	for inIdx < len(inLines) && outIdx < len(outLines) {
		inLine := inLines[inIdx]
		outLine := outLines[outIdx]

		// Track frontmatter state
		if inLine == "---" {
			inFrontmatter = !inFrontmatter
		}
		if outLine == "---" {
			outFrontmatter = !outFrontmatter
		}

		// Skip the timestamp line (only one should change)
		if strings.HasPrefix(inLine, "timestamp:") && strings.HasPrefix(outLine, "timestamp:") {
			inIdx++
			outIdx++
			continue
		}

		if inLine != outLine {
			t.Errorf("line %d (in) vs line %d (out) differ:\n  in:  %q\n  out: %q",
				inIdx, outIdx, inLine, outLine)
		}
		inIdx++
		outIdx++
	}
}

func TestExtraNilForConformingDocNoUnknownKeys(t *testing.T) {
	input := `---
type: guide
title: Clean Doc
---
Body.
`
	doc, err := ParseDoc("clean.md", []byte(input))
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if !doc.Conforming {
		t.Fatal("expected Conforming=true")
	}
	if doc.Extra != nil {
		t.Fatal("Extra should be nil when no unknown keys exist")
	}
}

func TestEmptyFrontmatterIsNonConforming(t *testing.T) {
	input := []byte("---\n---\nBody content")
	doc, err := ParseDoc("empty.md", input)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if doc.Conforming {
		t.Fatal("expected Conforming=false for empty frontmatter")
	}
}

func TestRenderConformingDocProducesFrontmatter(t *testing.T) {
	input := `---
type: guide
title: Simple
---
Body text.
`
	doc, err := ParseDoc("simple.md", []byte(input))
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	out, err := doc.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	outStr := string(out)
	if !strings.HasPrefix(outStr, "---\n") {
		t.Error("rendered output must start with ---")
	}
	if !strings.Contains(outStr, "\n---\n") {
		t.Error("rendered output must contain closing ---")
	}
	if !strings.Contains(outStr, "Body text.") {
		t.Error("rendered output must contain body")
	}
}

func TestRenderNonConformingDoc(t *testing.T) {
	doc := &Doc{
		Path:       "raw.md",
		Body:       "Just text, no frontmatter.",
		Conforming: false,
	}
	out, err := doc.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if string(out) != "Just text, no frontmatter." {
		t.Errorf("Render non-conforming = %q, want body only", string(out))
	}
}

// TestExtractFrontmatterEdgeCases exercises boundary conditions that previously
// caused panics or incorrect body extraction.
func TestExtractFrontmatterEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantOK   bool // whether extractFrontmatter should report a valid frontmatter block
		wantFM   string
		wantBody string
	}{
		// Panic regression: ---\n--- (closing delimiter at EOF, no trailing newline).
		// This is a valid (empty) frontmatter with no body.
		{
			name:     "closing delimiter at EOF no trailing newline",
			raw:      "---\n---",
			wantOK:   true,
			wantFM:   "",
			wantBody: "",
		},
		// Panic regression: ---\n---\n (closing delimiter at EOF with trailing newline).
		// The trailing newline belongs to the body.
		{
			name:     "closing delimiter at EOF with trailing newline",
			raw:      "---\n---\n",
			wantOK:   true,
			wantFM:   "",
			wantBody: "\n",
		},
		// Empty frontmatter with body.
		{
			name:     "empty frontmatter with body",
			raw:      "---\n---\nBody content",
			wantOK:   true,
			wantFM:   "",
			wantBody: "\nBody content",
		},
		// Normal doc with frontmatter.
		{
			name:     "normal conforming doc",
			raw:      "---\ntype: guide\ntitle: Example\n---\nHello world\n",
			wantOK:   true,
			wantFM:   "type: guide\ntitle: Example",
			wantBody: "Hello world\n",
		},
		// CRLF line endings. The frontmatter captures up to the \r before the
		// closing \n---\r\n delimiter (CRLF pairs leave a trailing \r).
		{
			name:     "CRLF line endings",
			raw:      "---\r\ntype: guide\r\ntitle: CRLF\r\n---\r\nHello\r\n",
			wantOK:   true,
			wantFM:   "type: guide\r\ntitle: CRLF\r",
			wantBody: "Hello\r\n",
		},
		// No frontmatter: just body.
		{
			name:   "no frontmatter",
			raw:    "Just content\n",
			wantOK: false,
		},
		// --- not followed by newline is not frontmatter.
		{
			name:   "dashes not frontmatter",
			raw:    "---inline dashes are not frontmatter",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fmBytes, body, ok := extractFrontmatter([]byte(tt.raw))
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (fm=%q body=%q)", ok, tt.wantOK, string(fmBytes), body)
			}
			if !ok {
				return
			}
			if string(fmBytes) != tt.wantFM {
				t.Errorf("frontmatter = %q, want %q", string(fmBytes), tt.wantFM)
			}
			if body != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

// TestParseDocEdgeCases verifies ParseDoc does not panic on malformed inputs
// and correctly sets Conforming=false for empty/invalid frontmatter.
func TestParseDocEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		raw         []byte
		wantPanic   bool
		wantConform bool
	}{
		{name: "closing delimiter at EOF", raw: []byte("---\n---"), wantConform: false},
		{name: "closing delimiter at EOF with nl", raw: []byte("---\n---\n"), wantConform: false},
		{name: "empty frontmatter with body", raw: []byte("---\n---\nBody"), wantConform: false},
		{name: "normal doc", raw: []byte("---\ntype: guide\n---\nBody\n"), wantConform: true},
		{name: "CRLF doc", raw: []byte("---\r\ntype: guide\r\ntitle: CRLF\r\n---\r\nBody\r\n"), wantConform: true},
		{name: "two-byte input", raw: []byte("--"), wantConform: false},
		{name: "empty input", raw: []byte(""), wantConform: false},
		{name: "just newline after dashes", raw: []byte("---\n"), wantConform: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseDoc("x.md", tt.raw)
			if err != nil {
				t.Fatalf("ParseDoc returned error: %v", err)
			}
			if doc.Conforming != tt.wantConform {
				t.Errorf("Conforming = %v, want %v", doc.Conforming, tt.wantConform)
			}
		})
	}
}
