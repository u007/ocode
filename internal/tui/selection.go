package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

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

// applySelectionHighlight injects reverse-video ANSI codes over the selected
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
// reverse-video ANSI codes (selection highlight).
func insertHighlight(rendered, raw string, rawStart, rawEnd int) string {
	return insertSGRSpan(rendered, raw, rawStart, rawEnd, "\x1b[7m", "\x1b[27m")
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
