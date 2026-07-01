package tui

import (
	"testing"
)

// TestResizeFastPathMatchesFullRerender guards the width-only fast path in
// renderMessageBlock (model.go): on a viewport resize, tool/assistant/user
// blocks reuse the cached Chroma/markdown innerContent and only redo the
// cheap box-layout step, instead of a full re-render. That optimization is
// only correct if it produces byte-identical output to a from-scratch render
// at the new width. This test renders a tool-heavy transcript (with a user
// message, an assistant text message, a tool block, and a thinking block) at
// width 100, resizes to 80 (hitting the fast path), then clears the cache and
// renders fresh at 80, asserting the two transcripts are identical.
func TestResizeFastPathMatchesFullRerender(t *testing.T) {
	m := buildHeavyTranscriptModel(3, 2*1024)
	m.messages = append(m.messages, message{role: roleUser, text: "a user question with **bold** text and a fairly long line of prose to force wrapping"})

	m.viewport.SetWidth(100)
	m.renderTranscript() // prime cache at width=100

	m.viewport.SetWidth(80) // triggers the width-only fast path
	m.renderTranscript()
	fastPathLines := append([]string(nil), m.transcriptLines...)

	m.msgRenderCache = nil // force full re-render at the same width=80
	m.renderTranscript()
	fullRenderLines := m.transcriptLines

	if len(fastPathLines) != len(fullRenderLines) {
		t.Fatalf("line count mismatch: fast-path=%d full-render=%d", len(fastPathLines), len(fullRenderLines))
	}
	for i := range fullRenderLines {
		if fastPathLines[i] != fullRenderLines[i] {
			t.Fatalf("line %d mismatch:\nfast-path:   %q\nfull-render: %q", i, fastPathLines[i], fullRenderLines[i])
		}
	}
}
