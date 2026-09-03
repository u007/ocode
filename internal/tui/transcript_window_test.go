package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

// buildTranscriptWindowTestModel constructs a minimal chat model with n
// alternating user/assistant messages for transcript-window tests. Plain
// text blocks keep wrapped heights small and deterministic.
func buildTranscriptWindowTestModel(n int) model {
	msgs := make([]message, 0, n)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			msgs = append(msgs, message{role: roleUser, text: fmt.Sprintf("question %d", i)})
		} else {
			msgs = append(msgs, message{role: roleAssistant, text: fmt.Sprintf("answer %d", i)})
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
	return m
}

// TestTranscriptWindowInitialTruncation verifies a long session renders only
// the tail chunk with a notice line on top and -1 start lines for hidden
// messages.
func TestTranscriptWindowInitialTruncation(t *testing.T) {
	m := buildTranscriptWindowTestModel(120)
	m.renderTranscript()

	if m.transcriptWindowStart != 120-transcriptInitialWindowMsgs {
		t.Fatalf("windowStart = %d, want %d", m.transcriptWindowStart, 120-transcriptInitialWindowMsgs)
	}
	if got := m.transcriptHiddenCount(); got != 70 {
		t.Fatalf("hidden = %d, want 70", got)
	}
	for i := 0; i < 70; i++ {
		if m.transcriptMsgStartLine[i] != -1 {
			t.Fatalf("msg %d startLine = %d, want -1 (hidden)", i, m.transcriptMsgStartLine[i])
		}
	}
	if m.transcriptMsgStartLine[70] < 1 {
		t.Fatalf("first visible msg startLine = %d, want >= 1 (line 0 is the notice)", m.transcriptMsgStartLine[70])
	}
	if len(m.transcriptLines) == 0 || !strings.Contains(stripANSI(m.transcriptLines[0]), "earlier messages") {
		t.Fatalf("line 0 should be the load-more notice, got %q", m.transcriptLines[0])
	}
	// Short sessions render everything with no notice.
	small := buildTranscriptWindowTestModel(10)
	small.renderTranscript()
	if small.transcriptWindowStart != 0 || small.transcriptHiddenCount() != 0 {
		t.Fatalf("short session should show all (start=%d hidden=%d)",
			small.transcriptWindowStart, small.transcriptHiddenCount())
	}
}

// TestTranscriptWindowBoundedWhileAtBottom verifies tail-follow eviction: a
// live session pinned to the bottom can't grow the window without limit.
func TestTranscriptWindowBoundedWhileAtBottom(t *testing.T) {
	m := buildTranscriptWindowTestModel(400)
	m.renderTranscript()
	// Initial render windows to the tail chunk, not the max.
	if m.transcriptWindowStart != 400-transcriptInitialWindowMsgs {
		t.Fatalf("initial windowStart = %d, want %d", m.transcriptWindowStart, 400-transcriptInitialWindowMsgs)
	}
	// Simulate the user loading everything, then streaming at the bottom.
	m.transcriptWindowStart = 0
	m.transcriptWindowInit = true
	m.transcriptRenderedLen = len(m.messages)
	m.viewport.GotoBottom()
	for i := 0; i < 10; i++ {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("stream %d", i)})
	}
	m.renderTranscript()
	if size := len(m.messages) - m.transcriptWindowStart; size > transcriptMaxWindowMsgs {
		t.Fatalf("window size %d exceeds max %d while at bottom", size, transcriptMaxWindowMsgs)
	}
	if m.transcriptWindowStart != len(m.messages)-transcriptMaxWindowMsgs {
		t.Fatalf("windowStart = %d, want tail %d", m.transcriptWindowStart, len(m.messages)-transcriptMaxWindowMsgs)
	}
}

// TestTranscriptWindowFrozenWhileScrolledUp verifies a scrolled-up view never
// trims: new tail messages render below without shifting the window.
func TestTranscriptWindowFrozenWhileScrolledUp(t *testing.T) {
	m := buildTranscriptWindowTestModel(200)
	m.renderTranscript()
	// Load two older chunks, then scroll up away from the bottom.
	m.expandTranscriptWindow()
	m.expandTranscriptWindow()
	startBefore := m.transcriptWindowStart
	m.viewport.GotoBottom()
	m.viewport.ScrollUp(5)
	if m.viewport.AtBottom() {
		t.Fatal("test needs a scrolled-up viewport")
	}
	yBefore := m.viewport.YOffset()
	for i := 0; i < 100; i++ {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("later %d", i)})
	}
	m.renderTranscript()
	if m.transcriptWindowStart != startBefore {
		t.Fatalf("scrolled-up render moved windowStart %d -> %d", startBefore, m.transcriptWindowStart)
	}
	if m.viewport.YOffset() != yBefore {
		t.Fatalf("scrolled-up render moved YOffset %d -> %d", yBefore, m.viewport.YOffset())
	}
}

// TestExpandTranscriptWindowAnchor verifies prepending a chunk keeps the
// previously-visible top line on the same screen row.
func TestExpandTranscriptWindowAnchor(t *testing.T) {
	m := buildTranscriptWindowTestModel(120)
	m.renderTranscript()
	m.viewport.GotoBottom()
	m.viewport.SetYOffset(0)
	oldTotal := len(m.transcriptLines)
	// Anchor on the first visible *message* line, not the notice row: the
	// notice text itself changes ("70" -> "20 earlier messages") while the
	// message below it must stay on the same screen row.
	firstMsg := m.transcriptWindowStart
	oldMsgLine := m.transcriptMsgStartLine[firstMsg]
	oldContent := m.transcriptLines[oldMsgLine]
	if !m.expandTranscriptWindow() {
		t.Fatal("expected window to expand")
	}
	if m.transcriptWindowStart != 20 {
		t.Fatalf("windowStart = %d, want 20", m.transcriptWindowStart)
	}
	added := len(m.transcriptLines) - oldTotal
	if added <= 0 {
		t.Fatalf("expected prepended lines, old=%d new=%d", oldTotal, len(m.transcriptLines))
	}
	if got := m.viewport.YOffset(); got != added {
		t.Fatalf("YOffset = %d, want %d (anchor shift)", got, added)
	}
	newMsgLine := m.transcriptMsgStartLine[firstMsg]
	if newMsgLine != oldMsgLine+added {
		t.Fatalf("msg %d startLine = %d, want %d (anchor shift)", firstMsg, newMsgLine, oldMsgLine+added)
	}
	if got := m.transcriptLines[newMsgLine]; got != oldContent {
		t.Fatalf("anchored line changed:\nbefore %q\nafter  %q", oldContent, got)
	}
}

// TestScrollTranscriptUpLoadsAtTop verifies one wheel/page gesture pinned to
// the top loads an older chunk and moves visibly into it.
func TestScrollTranscriptUpLoadsAtTop(t *testing.T) {
	m := buildTranscriptWindowTestModel(120)
	m.renderTranscript()
	m.viewport.GotoBottom()
	m.viewport.SetYOffset(0)
	startBefore := m.transcriptWindowStart
	m.scrollTranscriptUp(3)
	if m.transcriptWindowStart >= startBefore {
		t.Fatalf("scroll at top did not expand window (start=%d)", m.transcriptWindowStart)
	}
	if m.viewport.AtTop() && m.transcriptHiddenCount() == 0 {
		t.Fatal("expected remaining hidden history or a moved viewport")
	}
	// Multi-chunk: repeated top gestures eventually load everything.
	for i := 0; i < 10 && m.transcriptHiddenCount() > 0; i++ {
		m.viewport.SetYOffset(0)
		m.scrollTranscriptUp(m.viewport.Height())
	}
	if m.transcriptHiddenCount() != 0 {
		t.Fatalf("repeated top scrolls left %d hidden messages", m.transcriptHiddenCount())
	}
}

// TestSearchJumpExpandsHidden verifies a chat-search jump to a hidden message
// expands the window instead of landing on the notice line.
func TestSearchJumpExpandsHidden(t *testing.T) {
	m := buildTranscriptWindowTestModel(120)
	m.renderTranscript()
	if m.transcriptMsgStartLine[5] != -1 {
		t.Fatal("test needs msg 5 hidden")
	}
	if !m.ensureTranscriptMessageVisible(5) {
		t.Fatal("expected hidden msg 5 to become visible")
	}
	if m.transcriptWindowStart > 5 {
		t.Fatalf("windowStart = %d, want <= 5", m.transcriptWindowStart)
	}
	if m.transcriptMsgStartLine[5] < 0 {
		t.Fatal("msg 5 should have a rendered start line after expansion")
	}
	if got := m.ensureTranscriptMessageVisible(5000); got {
		t.Fatal("out-of-range msgIdx should return false")
	}
}

// TestTranscriptWindowReset verifies reset returns to uninitialized so the
// next render recomputes the tail window.
func TestTranscriptWindowReset(t *testing.T) {
	m := buildTranscriptWindowTestModel(120)
	m.renderTranscript()
	m.resetTranscriptWindow()
	if m.transcriptWindowInit || m.transcriptWindowStart != 0 || m.transcriptRenderedLen != 0 {
		t.Fatalf("reset should clear window state, got init=%v start=%d renderedLen=%d",
			m.transcriptWindowInit, m.transcriptWindowStart, m.transcriptRenderedLen)
	}
	m.renderTranscript()
	if m.transcriptWindowStart != 120-transcriptInitialWindowMsgs {
		t.Fatalf("re-render windowStart = %d, want %d", m.transcriptWindowStart, 120-transcriptInitialWindowMsgs)
	}
}

// TestCompactionResetsWindow verifies applyCompactionResult shows the full
// post-compaction transcript (banner included) instead of stale offsets.
func TestCompactionResetsWindow(t *testing.T) {
	m := buildTranscriptWindowTestModel(10)
	for i := range m.messages {
		am := agent.Message{Role: "assistant", Content: fmt.Sprintf("content %d", i)}
		m.messages[i].raw = &am
	}
	m.renderTranscript()
	m.transcriptWindowStart = 5 // pretend an older window position
	m.transcriptWindowInit = true
	uiIdx := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	r := agent.CompactResult{
		OK:          true,
		ReplaceFrom: 2,
		ReplaceTo:   7,
		Summary:     agent.Message{Role: "system", Content: "[ocode:compaction-summary] test summary"},
	}
	ok, bannerIdx := m.applyCompactionResult(r, uiIdx)
	if !ok {
		t.Fatal("expected compaction to apply")
	}
	if m.transcriptWindowStart != 0 || !m.transcriptWindowInit {
		t.Fatalf("compaction should show full initialized window (start=0, init), got start=%d init=%v",
			m.transcriptWindowStart, m.transcriptWindowInit)
	}
	m.renderTranscript()
	if bannerIdx < 0 || bannerIdx >= len(m.messages) {
		t.Fatalf("bad bannerIdx %d for %d messages", bannerIdx, len(m.messages))
	}
	if m.transcriptMsgStartLine[bannerIdx] < 0 {
		t.Fatal("compaction banner should be rendered")
	}
}

// TestHiddenLinksSkipped verifies markdown links in the hidden prefix don't
// produce clickable regions with stale line coordinates.
func TestHiddenLinksSkipped(t *testing.T) {
	msgs := []message{
		{role: roleAssistant, text: "see [hidden docs](https://example.com/hidden)"},
	}
	for i := 0; i < 60; i++ {
		msgs = append(msgs, message{role: roleAssistant, text: fmt.Sprintf("filler %d", i)})
	}
	msgs = append(msgs, message{role: roleAssistant, text: "see [visible docs](https://example.com/visible)"})
	m := buildChatSearchTestModel(msgs)
	m.styles = ApplyThemeColors("tokyonight")
	m.transcriptWindowInit = false
	m.renderTranscript()
	if m.transcriptHiddenCount() == 0 {
		t.Fatal("test needs a truncated window")
	}
	for _, r := range m.urlLinkRegions {
		if strings.Contains(r.url, "hidden") {
			t.Fatalf("hidden-prefix link should not produce a region: %+v", r)
		}
	}
	found := false
	for _, r := range m.urlLinkRegions {
		if strings.Contains(r.url, "visible") {
			found = true
		}
	}
	if !found {
		t.Fatal("visible link should produce a region")
	}
}
