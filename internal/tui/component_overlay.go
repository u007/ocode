package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// compositeOverlay splices box over backdrop at visual position (x, y).
// It is ANSI-aware: styled runs on the left of the box keep their styles,
// and styles are re-opened after the box on the right. CJK (double-width)
// characters at the splice boundary are handled without half-character
// artifacts. If the box is taller or wider than the backdrop, it is clamped.
// Empty backdrop returns "".
func compositeOverlay(backdrop, box string, x, y int) string {
	if backdrop == "" {
		return ""
	}
	if box == "" {
		return backdrop
	}

	bLines := strings.Split(backdrop, "\n")
	boxLines := strings.Split(box, "\n")

	// Clamp y to valid range.
	if y < 0 {
		y = 0
	}
	if y >= len(bLines) {
		return backdrop
	}

	// Clamp box height to remaining backdrop.
	boxH := len(boxLines)
	if y+boxH > len(bLines) {
		boxH = len(bLines) - y
	}

	// Clamp x to backdrop width (visual).
	backdropWidth := ansi.StringWidth(bLines[y])
	if x < 0 {
		x = 0
	}
	if x >= backdropWidth {
		return backdrop
	}

	result := make([]string, len(bLines))
	copy(result, bLines)

	for i := 0; i < boxH; i++ {
		// Use each line's own width for clamping.
		lineWidth := ansi.StringWidth(bLines[y+i])
		result[y+i] = spliceLine(bLines[y+i], boxLines[i], x, lineWidth)
	}

	return strings.Join(result, "\n")
}

// spliceLine splices boxText into backdropLine at visual column x.
// It preserves ANSI styles on the left and right of the splice region.
func spliceLine(backdropLine, boxText string, x, backdropLineWidth int) string {
	availWidth := backdropLineWidth - x
	if availWidth <= 0 {
		return backdropLine
	}

	// Clamp box width to available space first.
	boxWidth := ansi.StringWidth(boxText)
	if boxWidth > availWidth {
		boxText = ansi.Truncate(boxText, availWidth, "")
		boxWidth = availWidth
	}

	if x == 0 {
		// Box starts at the beginning — take backdrop after the box.
		right := takeAfterVisual(backdropLine, boxWidth)
		return boxText + right
	}

	// Walk the backdrop to find the byte split point at visual column x
	// and capture the SGR state at that position.
	left, right, sgrState := splitBackdropAtVisual(backdropLine, x)

	// If backdrop is shorter than x, pad with spaces in the current SGR state.
	if left == "" && right == "" && x > 0 {
		left = sgrState + strings.Repeat(" ", x)
	}

	// Right portion: truncate to exactly (availWidth - boxWidth) visual characters.
	rightNeed := availWidth - boxWidth
	if rightNeed <= 0 {
		right = ""
	} else {
		right = ansi.Truncate(right, rightNeed, "")
	}

	return left + boxText + right
}

// splitBackdropAtVisual splits a backdrop line at visual column visualCol.
// Returns (left, right, currentSGR) where:
//   - left is the ANSI-styled text for visual columns [0, visualCol)
//   - right is the ANSI-styled text for visual columns [visualCol, end)
//   - currentSGR is the SGR state at the split point (for padding)
func splitBackdropAtVisual(line string, visualCol int) (left, right, currentSGR string) {
	// Walk the line, tracking visual position and SGR state.
	visualPos := 0
	// Track the full SGR state: all active SGR parameters.
	sgrParams := make(map[byte]int) // parameter code → value
	var sgrCodes []int              // ordered codes for reconstruction

	i := 0
	for i < len(line) {
		if line[i] == '\x1b' && i+1 < len(line) && line[i+1] == '[' {
			// Parse ANSI escape sequence.
			j := i + 2
			for j < len(line) && ((line[j] >= '0' && line[j] <= '9') || line[j] == ';') {
				j++
			}
			if j < len(line) && line[j] == 'm' {
				// SGR sequence — update state.
				seq := line[i : j+1]
				params := parseSGRParams(seq)
				if len(params) == 0 {
					// ESC[m is shorthand for ESC[0m — reset all.
					for k := range sgrParams {
						delete(sgrParams, k)
					}
					sgrCodes = sgrCodes[:0]
				} else {
					for _, p := range params {
						if p == 0 {
							// Reset.
							for k := range sgrParams {
								delete(sgrParams, k)
							}
							sgrCodes = sgrCodes[:0]
						} else if p >= 1 && p <= 9 {
							sgrParams[byte(p)] = p
							sgrCodes = appendIfNotPresent(sgrCodes, p)
						} else if p >= 22 && p <= 29 {
							// Attribute off codes (bold off, faint off, etc).
							delete(sgrParams, byte(p-20))
							sgrCodes = removeCode(sgrCodes, p-20)
						} else if p >= 30 && p <= 37 {
							sgrParams[byte(p)] = p
							sgrCodes = appendColorCode(sgrCodes, p, 30, 37)
						} else if p == 39 {
							// Default foreground.
							delete(sgrParams, 38)
							sgrCodes = removeColorCode(sgrCodes, 30, 37)
						} else if p >= 40 && p <= 47 {
							sgrParams[byte(p)] = p
							sgrCodes = appendColorCode(sgrCodes, p, 40, 47)
						} else if p == 49 {
							// Default background.
							delete(sgrParams, 48)
							sgrCodes = removeColorCode(sgrCodes, 40, 47)
						} else if p >= 90 && p <= 97 {
							sgrParams[byte(p)] = p
							sgrCodes = appendColorCode(sgrCodes, p, 90, 97)
						} else if p >= 100 && p <= 107 {
							sgrParams[byte(p)] = p
							sgrCodes = appendColorCode(sgrCodes, p, 100, 107)
						}
					}
				}
				i = j + 1
				continue
			}
		}

		// Regular character — advance visual position.
		r, size := utf8Decode(line, i)
		w := runewidth.RuneWidth(r)
		if visualPos+w > visualCol {
			// Split falls within a double-width character.
			// Replace the character with a space to avoid half-character artifacts.
			currentSGR = reconstructSGR(sgrCodes)
			left = line[:i] + " "
			right = " " + line[i+size:]
			return
		}
		visualPos += w
		if visualPos == visualCol {
			// Found the split point after this character.
			currentSGR = reconstructSGR(sgrCodes)
			left = line[:i+size]
			right = line[i+size:]
			return
		}
		i += size
	}

	// visualCol is at or past the end of the line.
	currentSGR = reconstructSGR(sgrCodes)
	left = line
	right = ""
	return
}

// takeAfterVisual returns the ANSI-styled suffix of line starting at visual column visualCol.
func takeAfterVisual(line string, visualCol int) string {
	visualPos := 0
	i := 0
	for i < len(line) {
		if line[i] == '\x1b' && i+1 < len(line) && line[i+1] == '[' {
			j := i + 2
			for j < len(line) && ((line[j] >= '0' && line[j] <= '9') || line[j] == ';') {
				j++
			}
			if j < len(line) && line[j] == 'm' {
				i = j + 1
				continue
			}
		}
		r, size := utf8Decode(line, i)
		w := runewidth.RuneWidth(r)
		visualPos += w
		if visualPos > visualCol {
			return line[i:]
		}
		i += size
	}
	return ""
}

// utf8Decode decodes a UTF-8 rune at position i in s.
func utf8Decode(s string, i int) (rune, int) {
	r := rune(s[i])
	if r < utf8RuneSelf {
		return r, 1
	}
	size := 1
	for i+size < len(s) && (s[i+size]&0xC0) == 0x80 {
		size++
	}
	if i+size <= len(s) {
		r = rune(s[i])
		for j := 1; j < size; j++ {
			r = r<<6 | rune(s[i+j]&0x3F)
		}
	}
	return r, size
}

const utf8RuneSelf = 0x80

// parseSGRParams extracts the numeric parameters from an SGR escape sequence.
func parseSGRParams(seq string) []int {
	// seq is like "\x1b[1;31m"
	content := seq[2 : len(seq)-1] // strip ESC[ and m
	if content == "" {
		return nil
	}
	parts := strings.Split(content, ";")
	params := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		n := 0
		for _, c := range p {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
		params = append(params, n)
	}
	return params
}

// reconstructSGR builds an SGR escape sequence from a list of parameter codes.
func reconstructSGR(codes []int) string {
	if len(codes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\x1b[")
	for i, c := range codes {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(strings.TrimRight(strings.TrimRight(strings.TrimRight(
			strings.Replace(
				strings.Replace(
					strings.Replace(
						string(rune(c/100+'0')),
						"0", "", -1),
					"", "", -1),
				"", "", -1),
			"0"), "0"), "0"))
		// Simple int-to-string.
		s := ""
		n := c
		if n == 0 {
			s = "0"
		} else {
			for n > 0 {
				s = string(rune('0'+n%10)) + s
				n /= 10
			}
		}
		b.WriteString(s)
	}
	b.WriteByte('m')
	return b.String()
}

// appendIfNotPresent appends code to codes if not already present.
func appendIfNotPresent(codes []int, code int) []int {
	for _, c := range codes {
		if c == code {
			return codes
		}
	}
	return append(codes, code)
}

// appendColorCode appends a color code, removing any existing code in the same range.
func appendColorCode(codes []int, code, lo, hi int) []int {
	result := codes[:0]
	for _, c := range codes {
		if c < lo || c > hi {
			result = append(result, c)
		}
	}
	return append(result, code)
}

// removeCode removes a specific code from the list.
func removeCode(codes []int, code int) []int {
	result := codes[:0]
	for _, c := range codes {
		if c != code {
			result = append(result, c)
		}
	}
	return result
}

// removeColorCode removes any code in the range [lo, hi] from the list.
func removeColorCode(codes []int, lo, hi int) []int {
	result := codes[:0]
	for _, c := range codes {
		if c < lo || c > hi {
			result = append(result, c)
		}
	}
	return result
}

// dimLines wraps each line with ANSI faint/dim codes (\x1b[2m ... \x1b[22m).
// Line count and visual width are preserved.
func dimLines(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	result := make([]string, len(lines))
	for i, l := range lines {
		if l == "" {
			result[i] = ""
		} else {
			result[i] = "\x1b[2m" + l + "\x1b[22m"
		}
	}
	return result
}

// --- dimCache ---

// dimCache caches the dimmed version of backdrop lines, keyed by a
// content-version counter. It avoids recomputing the (potentially expensive)
// dimming on every render when content hasn't changed.
type dimCache struct {
	version   int
	lines     []string
	dimmed    []string
}

// newDimCache creates a new dimCache.
func newDimCache() *dimCache {
	return &dimCache{}
}

// DimIfChanged returns the dimmed lines. If the version matches the cache,
// the cached result is returned (same slice pointer). If version or content
// has changed, dimLines is called and the result cached.
func (c *dimCache) DimIfChanged(lines []string, version int) []string {
	if len(lines) == 0 {
		return nil
	}
	if c.version == version && c.matches(lines) {
		return c.dimmed
	}
	c.version = version
	c.lines = lines
	c.dimmed = dimLines(lines)
	return c.dimmed
}

// matches checks if lines are the same as the cached content.
func (c *dimCache) matches(lines []string) bool {
	if len(c.lines) != len(lines) {
		return false
	}
	for i := range lines {
		if c.lines[i] != lines[i] {
			return false
		}
	}
	return true
}
