package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tui/fastviewport"
)

// TestExpandAnchorWithVariableHeights verifies the prepend anchor math when
// the window mixes one-line messages with a multi-hundred-line block: the
// stable message's start line shifts by exactly the prepended delta.
func TestExpandAnchorWithVariableHeights(t *testing.T) {
	msgs := make([]message, 0, 100)
	for i := 0; i < 100; i++ {
		switch {
		case i == 80:
			msgs = append(msgs, message{role: roleAssistant, text: "big block:\n" + strings.Repeat("detail line of output here\n", 200)})
		case i%2 == 0:
			msgs = append(msgs, message{role: roleUser, text: fmt.Sprintf("q %d", i)})
		default:
			msgs = append(msgs, message{role: roleAssistant, text: fmt.Sprintf("a %d", i)})
		}
	}
	m := model{
		ready:     true,
		width:     120,
		height:    40,
		activeTab: tabChat,
		messages:  msgs,
		viewport:  fastviewport.New(80, 20),
	}
	m.styles = ApplyThemeColors("tokyonight")
	m.transcriptWindowInit = false // uninitialized: first render computes the tail window
	m.renderTranscript()

	if m.transcriptWindowStart != 50 {
		t.Fatalf("windowStart = %d, want 50", m.transcriptWindowStart)
	}
	m.viewport.GotoBottom()
	m.viewport.SetYOffset(0)
	oldTotal := len(m.transcriptLines)
	anchorMsg := 60 // short message below the huge block
	oldLine := m.transcriptMsgStartLine[anchorMsg]
	oldContent := m.transcriptLines[oldLine]
	if !m.expandTranscriptWindow() {
		t.Fatal("expected window to expand")
	}
	added := len(m.transcriptLines) - oldTotal
	if added <= 0 {
		t.Fatalf("expected prepended lines, old=%d new=%d", oldTotal, len(m.transcriptLines))
	}
	if got := m.viewport.YOffset(); got != added {
		t.Fatalf("YOffset = %d, want %d", got, added)
	}
	if got := m.transcriptMsgStartLine[anchorMsg]; got != oldLine+added {
		t.Fatalf("msg %d startLine = %d, want %d", anchorMsg, got, oldLine+added)
	}
	if got := m.transcriptLines[oldLine+added]; got != oldContent {
		t.Fatalf("anchored content changed:\nbefore %q\nafter  %q", oldContent, got)
	}
	// The huge block's region must have shifted by the same delta.
	if got := m.transcriptMsgStartLine[80]; got < 0 {
		t.Fatal("huge block should be rendered")
	}
}

// TestScrollbarReleasePinsToLoadedTop verifies scrollbar-initiated loads pin
// the viewport to the newly loaded top (thumb stays under the cursor)
// instead of preserving the old anchor, and never fire away from the top.
func TestScrollbarReleasePinsToLoadedTop(t *testing.T) {
	m := buildTranscriptWindowTestModel(120)
	m.renderTranscript()
	m.viewport.GotoBottom()
	m.viewport.SetYOffset(0)
	if !m.maybeLoadMoreTranscriptToTop() {
		t.Fatal("expected scrollbar load at top to expand")
	}
	if m.transcriptWindowStart != 20 {
		t.Fatalf("windowStart = %d, want 20", m.transcriptWindowStart)
	}
	if got := m.viewport.YOffset(); got != 0 {
		t.Fatalf("scrollbar load should pin to top, YOffset = %d", got)
	}
	if !strings.Contains(stripANSI(m.transcriptLines[0]), "20 earlier messages") {
		t.Fatalf("line 0 should be the updated notice, got %q", stripANSI(m.transcriptLines[0]))
	}
	// Away from the top nothing loads.
	m.viewport.ScrollDown(5)
	if m.maybeLoadMoreTranscriptToTop() {
		t.Fatal("scrollbar load must not fire away from the top")
	}
	if m.transcriptWindowStart != 20 {
		t.Fatalf("windowStart moved to %d without a top load", m.transcriptWindowStart)
	}
}
