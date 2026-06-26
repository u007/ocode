package tui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// selectionHighlightOpen and selectionHighlightClose are the ANSI SGR
// sequences used by insertHighlight to mark the selected range. They are
// set from the theme's SelectedBg/SelectedFg in ApplyThemeColors so that
// text selection on any surface (transcript, file preview, git diff, log,
// etc.) uses a visible background highlight that respects the current theme,
// rather than reverse-video (\x1b[7m) which can be invisible on
// syntax-highlighted content.
//
// Default values provide a visible background before theme application.
var selectionHighlightOpen, selectionHighlightClose = "\x1b[48;2;122;162;247m", "\x1b[49m"

// SetSelectionHighlightCodes updates the ANSI open/close sequences used
// for selection highlighting. Called from ApplyThemeColors.
func SetSelectionHighlightCodes(open, close string) {
	selectionHighlightOpen = open
	selectionHighlightClose = close
}

// hexColorToANSIBackground converts a "#RRGGBB" hex string to an ANSI SGR
// true-color background sequence ("\x1b[48;2;R;G;Bm"). Returns the empty
// string if the input cannot be parsed.
func hexColorToANSIBackground(hex string) string {
	r, g, b, ok := parseHexColor(hex)
	if !ok {
		return ""
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}

// hexColorToANSIForeground converts a "#RRGGBB" hex string to an ANSI SGR
// true-color foreground sequence ("\x1b[38;2;R;G;Bm"). Returns the empty
// string if the input cannot be parsed.
func hexColorToANSIForeground(hex string) string {
	r, g, b, ok := parseHexColor(hex)
	if !ok {
		return ""
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

// parseHexColor parses a "#RRGGBB" string into (R,G,B,true) or
// (0,0,0,false) on failure.
func parseHexColor(s string) (r, g, b int, ok bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 24)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(v >> 16), int((v >> 8) & 0xFF), int(v & 0xFF), true
}

type selectionState struct {
	active    bool
	dragging  bool
	startLine int
	startCol  int
	endLine   int
	endCol    int
}

func stripANSI(s string) string {
	return ansi.Strip(s)
}

// viewportSelectionConfig describes how a scrollable text surface maps screen
// coordinates back to its underlying raw text.
//
// It is intentionally generic so file previews, git diffs, and future viewers
// can share the same hit-testing logic without duplicating wrap/scroll math.
type viewportSelectionConfig struct {
	contentTopY  int
	contentLeftX int
	yOffset      int
	wrapWidth    int
	gutterWidth  int
	softWrap     bool
}

// visualLineWidth returns the terminal cell width of a plain-text line,
// accounting for wide runes and tabs.
func visualLineWidth(line string) int {
	col := 0
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRuneInString(line[i:])
		w := runewidth.RuneWidth(r)
		if r == '\t' {
			w = tabWidth - (col % tabWidth)
		}
		col += w
		i += size
	}
	return col
}

// visualLineHeight returns how many wrapped terminal rows a plain-text line
// occupies at the provided width.
func visualLineHeight(line string, width int) int {
	if width <= 0 {
		width = 1
	}
	lineWidth := visualLineWidth(line)
	if lineWidth <= 0 {
		return 1
	}
	rows := lineWidth / width
	if lineWidth%width != 0 {
		rows++
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

// visualLineAtOffset maps a soft-wrapped visual row offset back to the raw line
// that owns it, plus the wrapped-row offset within that line.
func visualLineAtOffset(rawLines []string, width, offset int) (lineIdx, wrappedOffset int) {
	if len(rawLines) == 0 {
		return 0, 0
	}
	if width <= 0 {
		width = 1
	}
	if offset < 0 {
		return 0, 0
	}
	remaining := offset
	for i, line := range rawLines {
		height := visualLineHeight(line, width)
		if remaining < height {
			return i, remaining
		}
		remaining -= height
	}
	return len(rawLines) - 1, 0
}

// point maps a screen coordinate back to a raw line/column in the configured
// surface.
func (c viewportSelectionConfig) point(rawLines []string, screenX, screenY int) (lineIdx, col int) {
	if len(rawLines) == 0 {
		return 0, 0
	}
	wrapWidth := c.wrapWidth
	if wrapWidth <= 0 {
		wrapWidth = 1
	}
	row := screenY - c.contentTopY + c.yOffset
	if row < 0 {
		row = 0
	}
	if c.softWrap {
		var wrappedOffset int
		lineIdx, wrappedOffset = visualLineAtOffset(rawLines, wrapWidth, row)
		col = screenX - c.contentLeftX - c.gutterWidth + wrappedOffset*wrapWidth
	} else {
		if row >= len(rawLines) {
			lineIdx = len(rawLines) - 1
		} else {
			lineIdx = row
		}
		col = screenX - c.contentLeftX - c.gutterWidth
	}
	if lineIdx < 0 {
		lineIdx = 0
	}
	if lineIdx >= len(rawLines) {
		lineIdx = len(rawLines) - 1
	}
	if col < 0 {
		col = 0
	}
	return lineIdx, col
}

// tabWidth is the terminal tab stop used when mapping screen columns.
const tabWidth = 8

// visualColToRuneIdx converts a 0-based visual column offset in a plain-text
// line to a byte index, accounting for wide runes (CJK, emoji) and tabs
// (which advance to the next tab stop on the terminal).
func visualColToRuneIdx(line string, visualCol int) int {
	col := 0
	i := 0
	for i < len(line) {
		if col >= visualCol {
			break
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		w := runewidth.RuneWidth(r)
		if r == '\t' {
			w = tabWidth - (col % tabWidth)
		}
		if col+w > visualCol {
			break
		}
		col += w
		i += size
	}
	return i
}

// normaliseSelection ensures start ≤ end in content space.
func normaliseSelection(startLine, startCol, endLine, endCol int) (int, int, int, int) {
	if startLine > endLine || (startLine == endLine && startCol > endCol) {
		return endLine, endCol, startLine, startCol
	}
	return startLine, startCol, endLine, endCol
}

// applySelectionHighlight injects selection-highlight ANSI codes (theme's
// SelectedFg/SelectedBg foreground+background colors) over the selected
// range in the rendered ANSI lines. rawLines provides the stripped text for
// column-to-byte mapping. Returns a new slice; input is not modified.
func applySelectionHighlight(lines []string, rawLines []string, startLine, startCol, endLine, endCol int) []string {
	startLine, startCol, endLine, endCol = normaliseSelection(startLine, startCol, endLine, endCol)
	out := make([]string, len(lines))
	copy(out, lines)

	for lineIdx := startLine; lineIdx <= endLine; lineIdx++ {
		if lineIdx < 0 || lineIdx >= len(lines) {
			continue
		}
		raw := ""
		if lineIdx < len(rawLines) {
			raw = rawLines[lineIdx]
		}

		colStart := 0
		colEnd := len(raw)
		if lineIdx == startLine {
			colStart = visualColToRuneIdx(raw, startCol)
		}
		if lineIdx == endLine {
			colEnd = visualColToRuneIdx(raw, endCol)
		}

		out[lineIdx] = insertHighlight(lines[lineIdx], raw, colStart, colEnd)
	}
	return out
}

// insertHighlight wraps the bytes [rawStart, rawEnd) of `rendered` with
// the currently-configured selection highlight ANSI codes (foreground +
// background from the theme's SelectedFg/SelectedBg, set in
// ApplyThemeColors). Unlike reverse-video (\x1b[7m), explicit foreground
// and background highlighting is reliably visible on
// syntax-highlighted content.
func insertHighlight(rendered, raw string, rawStart, rawEnd int) string {
	return insertSGRSpan(rendered, raw, rawStart, rawEnd, selectionHighlightOpen, selectionHighlightClose)
}

// insertSGRSpan wraps the bytes [rawStart, rawEnd) of `rendered` (aligned to
// the corresponding raw text positions) with the given SGR open/close codes.
// It pre-computes all ANSI escape positions to avoid O(n²) regex scanning.
func insertSGRSpan(rendered, raw string, rawStart, rawEnd int, openSeq, closeSeq string) string {
	if rawStart >= rawEnd || rawStart < 0 || rawEnd < 0 {
		return rendered
	}
	if rawStart >= len(raw) {
		return rendered
	}
	if rawEnd > len(raw) {
		rawEnd = len(raw)
	}

	// Pre-compute ANSI escape spans so the walk loop is O(n).
	// Each span is [start, end) byte offsets in rendered.
	type span struct{ start, end int }
	var escSpans []span
	{
		s := rendered
		base := 0
		for {
			loc := ansiEscapeIdx(s)
			if loc[0] < 0 {
				break
			}
			escSpans = append(escSpans, span{base + loc[0], base + loc[1]})
			base += loc[1]
			s = s[loc[1]:]
		}
	}
	nextEsc := 0

	renderedStart := -1
	renderedEnd := -1
	ri := 0
	rwi := 0

	for ri <= len(rendered) {
		// Skip any ANSI escapes at the current rendered position.
		for nextEsc < len(escSpans) && escSpans[nextEsc].start == ri {
			ri = escSpans[nextEsc].end
			nextEsc++
		}

		if rwi == rawStart {
			renderedStart = ri
		}
		if rwi == rawEnd {
			renderedEnd = ri
			break
		}
		if ri >= len(rendered) || rwi >= len(raw) {
			break
		}

		_, rSize := utf8.DecodeRuneInString(rendered[ri:])
		_, rwSize := utf8.DecodeRuneInString(raw[rwi:])
		ri += rSize
		rwi += rwSize
	}
	if renderedEnd == -1 {
		renderedEnd = len(rendered)
	}
	if renderedStart == -1 {
		return rendered
	}

	var b strings.Builder
	b.WriteString(rendered[:renderedStart])
	b.WriteString(openSeq)
	b.WriteString(rendered[renderedStart:renderedEnd])
	b.WriteString(closeSeq)
	b.WriteString(rendered[renderedEnd:])
	return b.String()
}

// ansiEscapeIdx returns the [start, end) of the first ANSI escape in s,
// or [-1, -1] if none found.
func ansiEscapeIdx(s string) [2]int {
	for i := 0; i < len(s); i++ {
		if s[i] != '\x1b' {
			continue
		}
		if i+1 >= len(s) || s[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(s) && (s[j] == ';' || (s[j] >= '0' && s[j] <= '9')) {
			j++
		}
		if j < len(s) && s[j] == 'm' {
			return [2]int{i, j + 1}
		}
	}
	return [2]int{-1, -1}
}

// extractSelectionText returns the plain-text substring for the selection.
func extractSelectionText(rawLines []string, startLine, startCol, endLine, endCol int) string {
	startLine, startCol, endLine, endCol = normaliseSelection(startLine, startCol, endLine, endCol)
	var parts []string
	for lineIdx := startLine; lineIdx <= endLine; lineIdx++ {
		if lineIdx < 0 || lineIdx >= len(rawLines) {
			parts = append(parts, "")
			continue
		}
		line := rawLines[lineIdx]
		cs := 0
		ce := len(line)
		if lineIdx == endLine {
			ce = visualColToRuneIdx(line, endCol)
		}
		if lineIdx == startLine {
			cs = visualColToRuneIdx(line, startCol)
		}
		if cs > ce {
			cs = ce
		}
		parts = append(parts, line[cs:ce])
	}
	return strings.Join(parts, "\n")
}
