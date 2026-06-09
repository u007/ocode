// Package fastviewport is a minimal scrollable surface over PRE-WRAPPED,
// no-soft-wrap content (e.g. the chat transcript, which is already hard-wrapped
// to the viewport width before being set).
//
// It exists because bubbles/v2's viewport.SetContentLines is O(N) on every
// call: it scans every line for "\r\n" and computes maxLineWidth
// (ansi.StringWidth per line) to populate longestLineWidth — a value only the
// horizontal-scroll paths read. The chat transcript never horizontally scrolls
// and is already wrapped to width, so both scans are pure waste. On a streaming
// turn renderTranscript runs ~11×/sec; benchmarked at 1000 message pairs the
// SetContentLines scan alone was 28.6ms of a 30.3ms render (0 allocs — pure
// CPU), i.e. 94% of the cost fed a value nothing ever read.
//
// fastviewport stores the line slice by reference (SetContentLines is O(1)) and
// makes View plus all scroll math O(visible window). It deliberately supports
// only the no-soft-wrap, no-horizontal-scroll, no-gutter case — surfaces that
// need soft wrap (log/git diff/files preview) must keep the bubbles viewport.
package fastviewport

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Model is a scroll surface over a slice of already-wrapped content lines.
type Model struct {
	width   int
	height  int
	yOffset int
	lines   []string
}

// New returns a Model sized to width×height.
func New(width, height int) Model {
	return Model{width: width, height: height}
}

// SetContent splits s on "\n" and stores the result. Provided for parity with
// the bubbles viewport API; SetContentLines is the cheaper entry point.
func (m *Model) SetContent(s string) { m.SetContentLines(strings.Split(s, "\n")) }

// SetContentLines stores lines by reference — O(1), no scanning. The caller
// must pass already-wrapped lines that contain no embedded "\n"/"\r" (the chat
// transcript assembles them this way). Matches the bubbles viewport's behavior
// of snapping to the bottom when the current offset would now be out of range
// (content shrank below the scroll position).
func (m *Model) SetContentLines(lines []string) {
	m.lines = lines
	if m.yOffset > m.maxYOffset() {
		m.GotoBottom()
	}
}

// Width returns the viewport width.
func (m Model) Width() int { return m.width }

// Height returns the viewport height.
func (m Model) Height() int { return m.height }

// SetWidth sets the viewport width and re-clamps the scroll offset.
func (m *Model) SetWidth(n int) {
	m.width = n
	m.SetYOffset(m.yOffset)
}

// SetHeight sets the viewport height and re-clamps the scroll offset.
func (m *Model) SetHeight(n int) {
	m.height = n
	m.SetYOffset(m.yOffset)
}

// YOffset returns the current vertical scroll offset (top visible line index).
func (m Model) YOffset() int { return m.yOffset }

// maxYOffset is the largest in-range scroll offset.
func (m Model) maxYOffset() int { return max(0, len(m.lines)-m.height) }

// SetYOffset sets the vertical scroll offset, clamped to [0, maxYOffset].
func (m *Model) SetYOffset(n int) { m.yOffset = clamp(n, 0, m.maxYOffset()) }

// AtTop reports whether the viewport is scrolled to the first line.
func (m Model) AtTop() bool { return m.yOffset <= 0 }

// AtBottom reports whether the viewport is scrolled to the last line.
func (m Model) AtBottom() bool { return m.yOffset >= m.maxYOffset() }

// GotoTop scrolls to the first line.
func (m *Model) GotoTop() { m.SetYOffset(0) }

// GotoBottom scrolls to the last line.
func (m *Model) GotoBottom() { m.SetYOffset(m.maxYOffset()) }

// ScrollUp moves the view up by n lines.
func (m *Model) ScrollUp(n int) {
	if m.AtTop() || n <= 0 || len(m.lines) == 0 {
		return
	}
	m.SetYOffset(m.yOffset - n)
}

// ScrollDown moves the view down by n lines.
func (m *Model) ScrollDown(n int) {
	if m.AtBottom() || n <= 0 || len(m.lines) == 0 {
		return
	}
	m.SetYOffset(m.yOffset + n)
}

// TotalLineCount returns the total number of content lines.
func (m Model) TotalLineCount() int { return len(m.lines) }

// VisibleLineCount returns the number of content lines currently on screen
// (excluding blank padding added by View to fill the height).
func (m Model) VisibleLineCount() int {
	if m.height <= 0 || len(m.lines) == 0 {
		return 0
	}
	return min(m.height, len(m.lines)-m.yOffset)
}

// Update is a no-op: the chat transcript drives scrolling through explicit
// ScrollUp/ScrollDown/GotoBottom calls (mouse wheel and keyboard scroll are
// handled by the parent model, never forwarded here). Kept for API parity so
// the parent's `vp, cmd = vp.Update(msg)` call site is unchanged.
func (m Model) Update(tea.Msg) (Model, tea.Cmd) { return m, nil }

// View renders the visible window, padded to width×height — matching the
// bubbles viewport's lipgloss Width/Height padding so callers that wrap the
// output (constrainView) see identical geometry.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	bottom := min(m.yOffset+m.height, len(m.lines))
	var visible []string
	if m.yOffset < bottom {
		visible = m.lines[m.yOffset:bottom]
	}
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Render(strings.Join(visible, "\n"))
}

func clamp(v, low, high int) int {
	if high < low {
		low, high = high, low
	}
	return min(high, max(low, v))
}
