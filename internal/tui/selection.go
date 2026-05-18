package tui

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiEscapeRE.ReplaceAllString(s, "")
}

// visualColToRuneIdx converts a 0-based visual column offset in a plain-text
// line to a byte index, accounting for wide runes (CJK, emoji).
func visualColToRuneIdx(line string, visualCol int) int {
	col := 0
	i := 0
	for i < len(line) {
		if col >= visualCol {
			break
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		w := runewidth.RuneWidth(r)
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
		rendered := lines[lineIdx]

		colStart := 0
		colEnd := len(raw) // select to end of line by default

		if lineIdx == startLine {
			colStart = visualColToRuneIdx(raw, startCol)
		}
		if lineIdx == endLine {
			colEnd = visualColToRuneIdx(raw, endCol)
		}

		// Map byte positions in raw to byte positions in rendered by walking
		// both strings together, skipping ANSI escape sequences in rendered.
		highlighted := insertHighlight(rendered, raw, colStart, colEnd)
		out[lineIdx] = highlighted
	}
	return out
}

// insertHighlight wraps the bytes [rawStart, rawEnd) of `rendered` (aligned
// to the corresponding raw text positions) with reverse-video ANSI codes.
func insertHighlight(rendered, raw string, rawStart, rawEnd int) string {
	if rawStart >= rawEnd || rawStart < 0 || rawEnd < 0 {
		return rendered
	}
	if rawStart >= len(raw) {
		return rendered
	}
	if rawEnd > len(raw) {
		rawEnd = len(raw)
	}

	// Walk rendered and raw together. ANSI escapes in rendered do not consume
	// raw characters. We find the rendered byte offsets corresponding to
	// rawStart and rawEnd, then inject the highlight codes there.
	escRe := ansiEscapeRE
	renderedStart := -1
	renderedEnd := -1
	ri := 0  // index into rendered
	rwi := 0 // index into raw
	for ri <= len(rendered) {
		if rwi == rawStart {
			renderedStart = ri
		}
		if rwi == rawEnd {
			renderedEnd = ri
			break
		}
		if ri >= len(rendered) {
			if rwi == rawEnd {
				renderedEnd = ri
			}
			break
		}
		// Check for ANSI escape at current rendered position
		if loc := escRe.FindStringIndex(rendered[ri:]); loc != nil && loc[0] == 0 {
			ri += loc[1]
			continue
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
	b.WriteString("\x1b[7m")
	b.WriteString(rendered[renderedStart:renderedEnd])
	b.WriteString("\x1b[27m")
	b.WriteString(rendered[renderedEnd:])
	return b.String()
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
		cs := visualColToRuneIdx(line, startCol)
		ce := len(line)
		if lineIdx == endLine {
			ce = visualColToRuneIdx(line, endCol)
		}
		if lineIdx == startLine {
			cs = visualColToRuneIdx(line, startCol)
		} else {
			cs = 0
		}
		if cs < 0 {
			cs = 0
		}
		if ce > len(line) {
			ce = len(line)
		}
		if cs > ce {
			cs = ce
		}
		parts = append(parts, line[cs:ce])
	}
	return strings.Join(parts, "\n")
}
