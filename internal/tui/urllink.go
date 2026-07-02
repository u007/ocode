package tui

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// urlLinkRegion marks a clickable URL span on one visual (wrapped) line.
// Columns are visual columns (matching mouse.X / selection coordinates), not
// byte offsets. url is the raw URL (markdown link target or detected URL).
// markdown is true when the region came from a [text](url) markdown link
// (so the click target is the URL inside the parentheses) and false when it
// came from a raw URL detected in plain text. Both are clickable; the flag
// is kept so future rendering passes can distinguish them.
type urlLinkRegion struct {
	line     int
	startCol int
	endCol   int
	url      string
	markdown bool
}

// urlCandidateRe matches a single URL-like token in plain text. Deliberately
// permissive — the candidate is then validated by looksLikeURL below. We
// avoid the path-token character class (no /+/~- here) so we don't
// double-match file paths that already get linked by pathlink.go. Common URL
// schemes only. RE2 has no \u escape, so the «» quote marks are spelled as
// literal runes in the character class.
var urlCandidateRe = regexp.MustCompile("https?://[^\\s<>\\[\\]()\\\\'\"\u00ab\u00bb]+")

// markdownLinkRe matches [text](url). text may not contain ']' or '['; url
// may not contain ')' or whitespace. The regex is intentionally greedy on
// the inside but excludes ']'/')' in the respective halves so a nested or
// broken markdown link fails to match and falls through to raw URL
// detection. RE2 supports these negated character classes natively.
var markdownLinkRe = regexp.MustCompile(`\[([^\[\]]+)\]\((https?://[^)\s]+)\)`)

// looksLikeURL does a final cheap sanity check on a candidate URL: it must
// be http(s) and the host (path of the URL up to the first '/', '?', or
// '#') must contain a dot, or be "localhost" (a common in-dev URL). This
// keeps the click target small and reliable — the cost of a stray match is
// a small visual underline over a non-URL, not a broken browser launch.
func looksLikeURL(s string) bool {
	if len(s) < 8 {
		// shortest plausible http://x.y
		return false
	}
	scheme := ""
	rest := s
	if i := strings.Index(rest, "://"); i >= 0 {
		scheme = rest[:i]
		rest = rest[i+3:]
	}
	if scheme != "http" && scheme != "https" {
		return false
	}
	if rest == "" {
		return false
	}
	// Host ends at first '/', '?', '#', or ':'. The ':' is included so
	// localhost:8080 and example.com:443 work.
	hostEnd := strings.IndexAny(rest, "/?#:")
	host := rest
	if hostEnd >= 0 {
		host = rest[:hostEnd]
	}
	if host == "localhost" {
		return true
	}
	return strings.Contains(host, ".")
}

// stripURLTrailingPunct trims sentence punctuation that the regex may have
// greedily included. It deliberately does NOT trim a trailing ')' because
// unbalanced parens are rare in plain prose and treating the URL with the
// paren included is the more useful click target (matches what the user
// visually sees).
func stripURLTrailingPunct(s string) string {
	return strings.TrimRight(s, ".,;:!?\u2026\"'")
}

// urlLinkAtCol returns the URL link (markdown or raw) under visualCol on an
// ANSI-stripped line, if any. Markdown links take priority over raw URL
// detection so a markdown-formatted link never "loses" its click target to
// the underlying raw URL match.
func urlLinkAtCol(rawLine string, visualCol int) (urlLinkRegion, bool) {
	// 1. Markdown link pass: scan for [text](url).
	for _, loc := range markdownLinkRe.FindAllStringSubmatchIndex(rawLine, -1) {
		textStart, textEnd := loc[2], loc[3]
		startCol := byteIdxToVisualCol(rawLine, textStart)
		endCol := byteIdxToVisualCol(rawLine, textEnd)
		if visualCol >= startCol && visualCol < endCol {
			url := rawLine[loc[4]:loc[5]]
			return urlLinkRegion{
				startCol: startCol,
				endCol:   endCol,
				url:      url,
				markdown: true,
			}, true
		}
	}
	// 2. Raw URL pass: scan the line and pick the candidate whose
	// [startCol, endCol) contains visualCol.
	for _, loc := range urlCandidateRe.FindAllStringIndex(rawLine, -1) {
		tok := rawLine[loc[0]:loc[1]]
		trimmed := stripURLTrailingPunct(tok)
		if trimmed == "" {
			continue
		}
		if !looksLikeURL(trimmed) {
			continue
		}
		startCol := byteIdxToVisualCol(rawLine, loc[0])
		endCol := byteIdxToVisualCol(rawLine, loc[0]+len(trimmed))
		if visualCol >= startCol && visualCol < endCol {
			return urlLinkRegion{
				startCol: startCol,
				endCol:   endCol,
				url:      trimmed,
				markdown: false,
			}, true
		}
	}
	return urlLinkRegion{}, false
}

// urlLinkAtColWrapped extends urlLinkAtCol to handle URLs that wrap across
// line boundaries. It tries the current line alone first, then combines with
// prevLine (URL continues from previous line onto this line) and nextLine
// (URL starts on this line and wraps to next). The returned region covers
// only the portion of the URL that sits on the current line.
//
// When matching across lines the combined text is prevLine+rawLine or
// rawLine+nextLine. Both markdown links and raw URLs are attempted on each
// combination (markdown first, then raw).
func urlLinkAtColWrapped(rawLine, prevLine, nextLine string, visualCol int) (urlLinkRegion, bool) {
	// 1. Single line (most common).
	if r, ok := urlLinkAtCol(rawLine, visualCol); ok {
		return r, true
	}

	lineLen := len(rawLine)

	// 2. URL wraps FROM the previous line INTO this line.
	if prevLine != "" {
		combined := prevLine + rawLine
		// 2a. Markdown link across boundary.
		for _, loc := range markdownLinkRe.FindAllStringSubmatchIndex(combined, -1) {
			textStart, textEnd := loc[2], loc[3]
			if textStart < lineLen && textEnd > lineLen {
				// The visible text crosses from prevLine into rawLine.
				// Region on this line starts at col 0 and ends at the
				// visual width of the portion on this line.
				onThisLine := rawLine[:textEnd-lineLen]
				startCol := 0
				endCol := byteIdxToVisualCol(rawLine, len(onThisLine))
				if visualCol >= startCol && visualCol < endCol {
					url := combined[loc[4]:loc[5]]
					return urlLinkRegion{
						startCol: startCol,
						endCol:   endCol,
						url:      url,
						markdown: true,
					}, true
				}
			}
		}
		// 2b. Raw URL across boundary.
		for _, loc := range urlCandidateRe.FindAllStringIndex(combined, -1) {
			tok := combined[loc[0]:loc[1]]
			trimmed := stripURLTrailingPunct(tok)
			if trimmed == "" || !looksLikeURL(trimmed) {
				continue
			}
			if loc[0] < lineLen && loc[0]+len(trimmed) > lineLen {
				// URL continues from prevLine into this line.
				onThisLine := len(trimmed) - (lineLen - loc[0])
				startCol := 0
				endCol := byteIdxToVisualCol(rawLine, 0+onThisLine)
				if visualCol >= startCol && visualCol < endCol {
					return urlLinkRegion{
						startCol: startCol,
						endCol:   endCol,
						url:      trimmed,
						markdown: false,
					}, true
				}
			}
		}
	}

	// 3. URL starts on this line and wraps INTO the next line.
	if nextLine != "" {
		combined := rawLine + nextLine
		// 3a. Markdown link across boundary.
		for _, loc := range markdownLinkRe.FindAllStringSubmatchIndex(combined, -1) {
			textStart, textEnd := loc[2], loc[3]
			if textStart < lineLen && textEnd > lineLen {
				startCol := byteIdxToVisualCol(rawLine, textStart)
				// The visible part on this line: rawLine[textStart:]
				endCol := byteIdxToVisualCol(rawLine, lineLen)
				if visualCol >= startCol && visualCol < endCol {
					url := combined[loc[4]:loc[5]]
					return urlLinkRegion{
						startCol: startCol,
						endCol:   endCol,
						url:      url,
						markdown: true,
					}, true
				}
			}
		}
		// 3b. Raw URL across boundary.
		for _, loc := range urlCandidateRe.FindAllStringIndex(combined, -1) {
			tok := combined[loc[0]:loc[1]]
			trimmed := stripURLTrailingPunct(tok)
			if trimmed == "" || !looksLikeURL(trimmed) {
				continue
			}
			if loc[0] < lineLen && loc[0]+len(trimmed) > lineLen {
				// URL starts on this line and wraps to next line.
				startCol := byteIdxToVisualCol(rawLine, loc[0])
				endCol := byteIdxToVisualCol(rawLine, lineLen)
				if visualCol >= startCol && visualCol < endCol {
					return urlLinkRegion{
						startCol: startCol,
						endCol:   endCol,
						url:      trimmed,
						markdown: false,
					}, true
				}
			}
		}
	}

	return urlLinkRegion{}, false
}

// urlLinkProbeCache memoizes the last urlLinkAtCol probe. Same rationale as
// pathLinkProbeCache: mouse motion fires 20-60Hz and we don't want to
// re-scan the line (and re-allocate the regex matches) on every event. Keyed
// by line content + exact probe column so the cache self-invalidates when
// the transcript re-renders or the cursor moves to a new position.
type urlLinkProbeCache struct {
	rawLine    string
	startCol   int
	endCol     int
	probeCol   int
	r          urlLinkRegion
	ok         bool
	cachedMiss bool
}

func (c *urlLinkProbeCache) probe(rawLine string, visualCol int) (urlLinkRegion, bool) {
	// Hit: cursor still inside the previously probed token span.
	if c.endCol > c.startCol && visualCol >= c.startCol && visualCol < c.endCol && rawLine == c.rawLine {
		return c.r, c.ok
	}
	// Miss: same exact column on same line — return cached miss.
	if c.cachedMiss && c.probeCol == visualCol && rawLine == c.rawLine {
		return c.r, false
	}
	r, ok := urlLinkAtCol(rawLine, visualCol)
	if ok {
		c.rawLine, c.startCol, c.endCol, c.r, c.ok = rawLine, r.startCol, r.endCol, r, ok
		c.cachedMiss = false
	} else if r.endCol > r.startCol {
		// Token found but didn't validate (e.g., a URL candidate without
		// a host dot): cache the span so motion within the token doesn't
		// re-scan.
		c.rawLine, c.startCol, c.endCol, c.r, c.ok = rawLine, r.startCol, r.endCol, r, false
		c.cachedMiss = false
	} else {
		c.rawLine, c.probeCol = rawLine, visualCol
		c.cachedMiss = true
		c.r, c.ok = r, false
		c.startCol, c.endCol = 0, 0
	}
	return r, ok
}

// applyUrlLinkUnderline returns a copy of lines with the URL link region's
// span underlined, mirroring applyPathLinkUnderline. rawLines provides plain
// text for visual-column → byte mapping.
func applyUrlLinkUnderline(lines, rawLines []string, r urlLinkRegion) []string {
	if r.line < 0 || r.line >= len(lines) {
		return lines
	}
	out := make([]string, len(lines))
	copy(out, lines)
	raw := ""
	if r.line < len(rawLines) {
		raw = rawLines[r.line]
	}
	cs := visualColToRuneIdx(raw, r.startCol)
	ce := visualColToRuneIdx(raw, r.endCol)
	out[r.line] = insertSGRSpan(out[r.line], raw, cs, ce, "\x1b[4m", "\x1b[24m")
	return out
}

// urlLinkStyle is the canonical link style: underlined with the theme's header
// foreground color. Updated by ApplyThemeColors via setMarkdownStyles.
var urlLinkStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#7da9f0")).
	Underline(true)

// markdownBoldStyle is the style applied to **bold** runs. Updated by
// ApplyThemeColors via setMarkdownStyles.
var markdownBoldStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#da702c")).
	Bold(true)

// markdownTitleStyle is applied to "# ", "## ", and "### " headings.
// Updated by ApplyThemeColors via setMarkdownStyles.
var markdownTitleStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#3aa99f")).
	Bold(true)

// --- Table rendering for markdown tables ---

// isTableLine reports whether the line looks like part of a markdown table
// (starts with "|" after trimming whitespace).
func isTableLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "|")
}

// isTableSeparator reports whether line is a markdown table separator row
// of the form |---|---| (possibly with : for alignment).
func isTableSeparator(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
		return false
	}
	inner := trimmed[1 : len(trimmed)-1]
	hasDash := false
	for _, r := range inner {
		if r == '-' {
			hasDash = true
		} else if r != ':' && r != ' ' && r != '|' {
			return false
		}
	}
	return hasDash
}

// parseTableRow splits a markdown table row (e.g. "| a | b | c |") into its
// cell contents with surrounding whitespace trimmed.
func parseTableRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	parts := strings.Split(trimmed, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}

// renderTable renders a block of markdown table lines with proper column
// alignment, padded cells, box-drawing separators, and a bold header row.
// normalStyle is the base text style for data cells; boldStyle is used for
// the header row (typically markdownBoldStyle).
func renderTable(lines []string, normalStyle, boldStyle lipgloss.Style) string {
	if len(lines) < 2 {
		return "" // not a valid table
	}

	// Phase 1: parse out header, separator, and data rows.
	var headerCells []string
	var dataRows [][]string
	seenSeparator := false

	for _, line := range lines {
		if !isTableLine(line) {
			continue
		}
		if !seenSeparator && isTableSeparator(line) {
			seenSeparator = true
			continue
		}
		if isTableSeparator(line) {
			continue // skip duplicate/intra separators
		}
		if headerCells == nil {
			headerCells = parseTableRow(line)
		} else {
			dataRows = append(dataRows, parseTableRow(line))
		}
	}
	if len(headerCells) == 0 {
		return ""
	}

	// Phase 2: determine column count and widths.
	// A colWidth entry is the visual width of the widest cell in that column.
	numCols := len(headerCells)
	for _, r := range dataRows {
		if len(r) > numCols {
			numCols = len(r)
		}
	}
	// Extend headerCells if data rows have more columns.
	for len(headerCells) < numCols {
		headerCells = append(headerCells, "")
	}

	colWidths := make([]int, numCols)
	// Measure header cells.
	for j, cell := range headerCells {
		rendered := renderMarkdownInLine(cell, boldStyle)
		w := ansi.StringWidth(stripANSI(rendered))
		if w > colWidths[j] {
			colWidths[j] = w
		}
	}
	// Measure data cells.
	for _, r := range dataRows {
		for j, cell := range r {
			if j >= numCols {
				break
			}
			rendered := renderMarkdownInLine(cell, normalStyle)
			w := ansi.StringWidth(stripANSI(rendered))
			if w > colWidths[j] {
				colWidths[j] = w
			}
		}
	}

	// Phase 3: build the rendered output.
	var b strings.Builder

	// --- Header row ---
	renderTableRow(&b, headerCells, colWidths, boldStyle, true)
	b.WriteString("\n")

	// --- Header separator ---
	b.WriteString(hintStyle.Render(renderTableSeparator(colWidths)))
	b.WriteString("\n")

	// --- Data rows ---
	for i, r := range dataRows {
		if i > 0 && i < len(dataRows) {
			// Optional: row separator for multi-row tables adds visual structure.
			// Skipping for cleaner look.
		}
		renderTableRow(&b, r, colWidths, normalStyle, false)
		if i < len(dataRows)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// renderTableRow writes a single styled table row to b, with cells padded to
// colWidths and separated by box-drawing vertical bars.
func renderTableRow(b *strings.Builder, cells []string, colWidths []int, cellStyle lipgloss.Style, isHeader bool) {
	b.WriteString("│ ")
	for j := 0; j < len(colWidths); j++ {
		var cellText string
		if j < len(cells) {
			cellText = renderMarkdownInLine(cells[j], cellStyle)
		}
		if isHeader {
			cellText = markdownBoldStyle.Render(stripANSI(renderMarkdownInLine(cells[j], cellStyle)))
		}
		// Pad to column width.
		padded := lipgloss.NewStyle().Width(colWidths[j]).Render(cellText)
		b.WriteString(padded)
		if j < len(colWidths)-1 {
			b.WriteString(" │ ")
		}
	}
	b.WriteString(" │")
}

// renderTableSeparator builds the horizontal rule between header and data
// rows, e.g. "├──────┼──────┼──────┤".
func renderTableSeparator(colWidths []int) string {
	var b strings.Builder
	b.WriteString("├")
	for j := 0; j < len(colWidths); j++ {
		b.WriteString(strings.Repeat("─", colWidths[j]+2))
		if j < len(colWidths)-1 {
			b.WriteString("┼")
		}
	}
	b.WriteString("┤")
	return b.String()
}

// renderMarkdown renders a possibly multi-line string (chat text) with
// bold, headings, markdown links, raw URL styling, and — new — markdown
// tables. It detects contiguous table blocks (groups of lines starting with
// "|") and renders them through renderTable, routing everything else through
// the existing renderMarkdownInLine.
//
// The table block is rendered with markdownBoldStyle for the header row and
// with the table-border characters styled via hintStyle.
func renderMarkdown(text string, normalStyle lipgloss.Style) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	var b strings.Builder
	i := 0
	for i < len(lines) {
		line := lines[i]
		// Detect start of a table block (line starts with "|").
		if isTableLine(line) {
			// Collect all contiguous table lines.
			tableLines := []string{line}
			i++
			for i < len(lines) && isTableLine(lines[i]) {
				tableLines = append(tableLines, lines[i])
				i++
			}
			// Render the table block.
			tableRendered := renderTable(tableLines, normalStyle, markdownBoldStyle)
			if tableRendered != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(tableRendered)
			} else {
				// If not a valid table, fall through to inline rendering.
				for j, tl := range tableLines {
					if j > 0 || b.Len() > 0 {
						b.WriteString("\n")
					}
					b.WriteString(renderMarkdownInLine(tl, normalStyle))
				}
			}
			continue
		}
		// Non-table line: render as before.
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(renderMarkdownInLine(line, normalStyle))
		i++
	}
	return b.String()
}

// renderMarkdownInLine renders a single non-heading line with bold,
// markdown links, and raw URL styling layered in that order. The order
// matters: markdown links rewrite "[text](url)" to just the visible text
// (dropping the URL part), then the remaining text is split on "**" to
// produce alternating normal/bold segments. Within each segment, any raw
// https?://... URLs are picked up and styled with urlLinkStyle.
func renderMarkdownInLine(line string, normalStyle lipgloss.Style) string {
	// 1. Headings: "# " / "## " / "### ". Heading text isn't bold-styled
	//    and we don't run link styling on it — headings are short labels,
	//    not prose.
	switch {
	case strings.HasPrefix(line, "# "):
		return markdownTitleStyle.Render(strings.TrimPrefix(line, "# "))
	case strings.HasPrefix(line, "## "):
		return markdownTitleStyle.Render(strings.TrimPrefix(line, "## "))
	case strings.HasPrefix(line, "### "):
		return markdownTitleStyle.Render(strings.TrimPrefix(line, "### "))
	}

	// 2. Strip markdown links: rewrite "[text](url)" → "text". We do
	//    this stripping on the raw text (before any styling) so the URL
	//    part is removed from the visible string. The text of the link
	//    is left in place and gets styled like a raw URL later.
	plain := line
	if strings.Contains(line, "](") {
		var sb strings.Builder
		last := 0
		for _, loc := range markdownLinkRe.FindAllStringSubmatchIndex(line, -1) {
			if loc[0] > last {
				sb.WriteString(line[last:loc[0]])
			}
			sb.WriteString(line[loc[2]:loc[3]]) // insert only the visible [text]
			last = loc[1]
		}
		if last < len(line) {
			sb.WriteString(line[last:])
		}
		plain = sb.String()
	}

	// 3. Split on "**" and render each segment. Even indices are normal
	//    text, odd indices are bold. Within each segment, raw https?://...
	//    URLs are rendered in urlLinkStyle.
	var out strings.Builder
	parts := strings.Split(plain, "**")
	for i, seg := range parts {
		if seg == "" {
			continue
		}
		if i%2 == 0 {
			// Normal segment: URL detection within normalStyle.
			renderPlainSegment(&out, seg, normalStyle, urlLinkStyle)
		} else {
			// Bold segment: URL detection within markdownBoldStyle.
			renderPlainSegment(&out, seg, markdownBoldStyle, urlLinkStyle)
		}
	}
	return out.String()
}

// renderPlainSegment writes the styled text of one non-** segment to out.
// textStyle is the base style (normal or bold); any raw https?://... URL
// found inside the segment is rendered with linkStyle instead.
func renderPlainSegment(out *strings.Builder, text string, textStyle, linkStyle lipgloss.Style) {
	if !strings.Contains(text, "://") {
		out.WriteString(textStyle.Render(text))
		return
	}
	i := 0
	for i < len(text) {
		loc := urlCandidateRe.FindStringIndex(text[i:])
		if loc == nil {
			out.WriteString(textStyle.Render(text[i:]))
			break
		}
		urlStart := i + loc[0]
		urlEnd := i + loc[1]
		trimmed := stripURLTrailingPunct(text[urlStart:urlEnd])
		if !looksLikeURL(trimmed) {
			// Not a real URL — render as plain text and keep scanning.
			out.WriteString(textStyle.Render(text[i:urlEnd]))
			i = urlEnd
			continue
		}
		// Text before the URL.
		if urlStart > i {
			out.WriteString(textStyle.Render(text[i:urlStart]))
		}
		// The URL itself.
		out.WriteString(linkStyle.Render(trimmed))
		i = urlStart + len(trimmed)
	}
}
