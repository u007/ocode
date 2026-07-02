package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestLooksLikeURL(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"https://example.com", true},
		{"http://example.com/path", true},
		{"https://sub.dom.ain/path/to/page?q=1", true},
		{"http://localhost:8080", true},
		{"http://localhost", true},
		{"https://example.com:443", true},
		{"https://x.y", true},
		{"https://127.0.0.1:3000", true},
		// Invalid
		{"httpx://example.com", false},
		{"http://", false},
		{"https://a", false}, // no dot and not "localhost"
		{"", false},
		{"ftp://example.com", false},
		{"mailto:test@example.com", false},
		{"just text", false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := looksLikeURL(tc.input)
			if got != tc.want {
				t.Errorf("looksLikeURL(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestStripURLTrailingPunct(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"https://example.com.", "https://example.com"},
		{"https://example.com,", "https://example.com"},
		{"https://example.com;", "https://example.com"},
		{"https://example.com!", "https://example.com"},
		{"https://example.com?", "https://example.com"},
		{"https://example.com\"", "https://example.com"},
		{"https://example.com", "https://example.com"},
		// Parentheses are NOT stripped (they're rarely unbalanced in prose)
		{"https://example.com)", "https://example.com)"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := stripURLTrailingPunct(tc.input)
			if got != tc.want {
				t.Errorf("stripURLTrailingPunct(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestUrlLinkAtCol_markdownLink(t *testing.T) {
	line := `Click [here](https://example.com) for docs.`
	// [here](url): visible text "here" at bytes [7,11)
	r, ok := urlLinkAtCol(line, 8) // cursor on 'e' of 'here'
	if !ok {
		t.Fatalf("expected markdown link hit at col 8")
	}
	if r.url != "https://example.com" {
		t.Errorf("url = %q, want %q", r.url, "https://example.com")
	}
	if r.startCol != 7 || r.endCol != 11 {
		t.Errorf("cols = [%d,%d), want [7,11)", r.startCol, r.endCol)
	}
	if !r.markdown {
		t.Error("expected markdown=true")
	}
}

func TestUrlLinkAtCol_markdownLinkOutside(t *testing.T) {
	line := `Click [here](https://example.com) for docs.`
	// Before the markdown link text (col 0 = 'C')
	if _, ok := urlLinkAtCol(line, 0); ok {
		t.Error("expected no hit before markdown link text")
	}
	// The markdown `[` at col 6 — between plain text and link text
	if _, ok := urlLinkAtCol(line, 6); ok {
		t.Error("expected no hit on '[' before markdown link text")
	}
	// The `]` at col 11 — after link text, before URL
	if _, ok := urlLinkAtCol(line, 11); ok {
		t.Error("expected no hit on ']' between markdown text and URL")
	}
}

func TestUrlLinkAtCol_rawURL(t *testing.T) {
	line := `Visit https://example.com/page for more.`
	// https://example.com/page starts at byte 6
	r, ok := urlLinkAtCol(line, 10) // cursor inside the URL
	if !ok {
		t.Fatalf("expected raw URL hit at col 10")
	}
	if r.url != "https://example.com/page" {
		t.Errorf("url = %q, want %q", r.url, "https://example.com/page")
	}
	if r.markdown {
		t.Error("expected markdown=false for raw URL")
	}
}

func TestUrlLinkAtCol_rawURLWithTrailingPunct(t *testing.T) {
	line := `See https://example.com. Continue here.`
	r, ok := urlLinkAtCol(line, 8) // cursor on ':'
	if !ok {
		t.Fatalf("expected raw URL hit")
	}
	if r.url != "https://example.com" {
		t.Errorf("url = %q, want %q", r.url, "https://example.com")
	}
}

func TestUrlLinkAtCol_markdownOverRaw(t *testing.T) {
	line := `Go [home](https://example.com) and see https://other.com`
	// [home](url) has visible text "home" at bytes [3,7)
	r, ok := urlLinkAtCol(line, 5) // inside "home"
	if !ok {
		t.Fatalf("expected markdown link hit at col 5")
	}
	if r.url != "https://example.com" {
		t.Errorf("markdown link url = %q, want %q", r.url, "https://example.com")
	}
	if !r.markdown {
		t.Error("expected markdown=true for markdown link at [text](url)")
	}

	// On the raw URL https://other.com. Counted manually: the leading
	// "Go [home](https://example.com) and see " is 39 bytes, so the raw
	// URL starts at col 39 (the 'h' of https://other.com). col 35 is the
	// 's' in "see" and would (correctly) not match — pin a column that
	// lands inside the URL.
	r, ok = urlLinkAtCol(line, 40) // cursor on the second 't' of 'https'
	if !ok {
		t.Fatalf("expected raw URL hit at col 35")
	}
	if !strings.HasPrefix(r.url, "https://other.com") {
		t.Errorf("raw url = %q, want prefix %q", r.url, "https://other.com")
	}
	if r.markdown {
		t.Error("expected markdown=false for raw URL")
	}
}

func TestUrlLinkAtCol_noMatch(t *testing.T) {
	line := `No URLs here, just plain text with /some/path.`
	if _, ok := urlLinkAtCol(line, 10); ok {
		t.Error("expected no match on plain text")
	}
}

func TestUrlLinkAtCol_filePathNotURL(t *testing.T) {
	line := `The path is /etc/passwd or ./foo.go:12`
	if _, ok := urlLinkAtCol(line, 15); ok {
		t.Error("expected no match on file path")
	}
}

// --- Wrapped URL detection ---

func TestUrlLinkAtColWrapped_singleLineFallback(t *testing.T) {
	// When the URL doesn't cross lines, Wrapped should behave like urlLinkAtCol
	currentLine := `Visit https://example.com/page now`
	r, ok := urlLinkAtColWrapped(currentLine, "", "", 10)
	if !ok {
		t.Fatalf("expected single-line URL hit")
	}
	if r.url != "https://example.com/page" {
		t.Errorf("url = %q, want %q", r.url, "https://example.com/page")
	}
}

func TestUrlLinkAtColWrapped_markdownLinkNotWrapped(t *testing.T) {
	currentLine := `Click [here](https://example.com) now`
	r, ok := urlLinkAtColWrapped(currentLine, "", "", 8) // on 'e' of 'here'
	if !ok {
		t.Fatalf("expected markdown link hit on single line")
	}
	if r.url != "https://example.com" {
		t.Errorf("url = %q, want %q", r.url, "https://example.com")
	}
	if !r.markdown {
		t.Error("expected markdown=true")
	}
}

func TestUrlLinkAtColWrapped_noAmbiguousMatch(t *testing.T) {
	// Plain text with no URLs should not match
	currentLine := `Just plain words here`
	if _, ok := urlLinkAtColWrapped(currentLine, "", "", 3); ok {
		t.Error("expected no match on plain text")
	}
}

// --- Probe cache ---

func TestUrlLinkProbeCache_hit(t *testing.T) {
	var c urlLinkProbeCache
	line := `Visit https://example.com now`

	// First probe: miss, run detection
	r, ok := c.probe(line, 10)
	if !ok {
		t.Fatalf("first probe should hit")
	}
	if r.url != "https://example.com" {
		t.Errorf("url = %q", r.url)
	}

	// Second probe: same line, same token span -> cache hit
	r2, ok2 := c.probe(line, 12) // still inside the URL
	if !ok2 {
		t.Fatal("second probe within same span should hit cache")
	}
	if r2.url != r.url {
		t.Errorf("cached url = %q, want %q", r2.url, r.url)
	}
}

func TestUrlLinkProbeCache_missThenNewLine(t *testing.T) {
	var c urlLinkProbeCache
	line1 := `Visit https://example.com now`
	line2 := `No URLs here`

	// First probe on line1 -> hit
	_, ok := c.probe(line1, 10)
	if !ok {
		t.Fatal("first probe should hit")
	}

	// Probe on line2 -> should re-evaluate (different line)
	_, ok2 := c.probe(line2, 3)
	if ok2 {
		t.Fatal("second probe (different line, no URL) should miss")
	}
}

func TestUrlLinkProbeCache_missThenSameCol(t *testing.T) {
	var c urlLinkProbeCache
	plainLine := `Just plain text words everywhere`

	// First probe at col 10 -> miss, cached as miss at exact col
	_, ok := c.probe(plainLine, 10)
	if ok {
		t.Fatal("expected miss on plain line")
	}

	// Second probe at same col on same line -> cached miss
	_, ok2 := c.probe(plainLine, 10)
	if ok2 {
		t.Fatal("expected cached miss on same line+col")
	}
}

// --- renderMarkdownInLine ---

func TestRenderMarkdownInLine_plain(t *testing.T) {
	normal := lipgloss.NewStyle()
	got := renderMarkdownInLine("Hello world", normal)
	if got == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(got, "Hello world") {
		t.Errorf("expected 'Hello world' in output, got %q", got)
	}
}

func TestRenderMarkdownInLine_heading(t *testing.T) {
	normal := lipgloss.NewStyle()
	got := renderMarkdownInLine("# My Title", normal)
	if !strings.Contains(got, "My Title") {
		t.Error("expected heading text in output")
	}
	// Subheadings
	got2 := renderMarkdownInLine("## Sub Title", normal)
	if !strings.Contains(got2, "Sub Title") {
		t.Error("expected sub-heading text in output")
	}
	got3 := renderMarkdownInLine("### Sub3 Title", normal)
	if !strings.Contains(got3, "Sub3 Title") {
		t.Error("expected sub3-heading text in output")
	}
}

func TestRenderMarkdownInLine_markdownLink(t *testing.T) {
	normal := lipgloss.NewStyle()
	got := renderMarkdownInLine("Visit [my site](https://example.com) here", normal)
	// The markdown link text "my site" should be present
	if !strings.Contains(got, "my site") {
		t.Error("expected 'my site' (link text) in output")
	}
	// The URL should NOT appear in the output (it's stripped)
	if strings.Contains(got, "example.com") {
		t.Error("expected URL to be stripped from output")
	}
}

func TestRenderMarkdownInLine_rawURL(t *testing.T) {
	normal := lipgloss.NewStyle()
	got := renderMarkdownInLine("See https://example.com for info", normal)
	// lipgloss v2 wraps each rune in its own SGR sequence, so substring
	// checks against the raw URL need the ANSI-stripped form. See
	// selection.go::stripANSI for the helper.
	if !strings.Contains(stripANSI(got), "https://example.com") {
		t.Errorf("expected raw URL in output, got %q", got)
	}
}

func TestRenderMarkdownInLine_boldAndURL(t *testing.T) {
	normal := lipgloss.NewStyle()
	// Bold text and a raw URL on the same line
	got := renderMarkdownInLine("Read **docs** at https://docs.example.com", normal)
	if !strings.Contains(stripANSI(got), "docs") {
		t.Error("expected 'docs' in output")
	}
	if !strings.Contains(stripANSI(got), "https://docs.example.com") {
		t.Error("expected URL in output")
	}
}

func TestRenderMarkdownInLine_boldContainingURL(t *testing.T) {
	normal := lipgloss.NewStyle()
	// Bold text containing a raw URL: the URL should survive bold stripping
	got := renderMarkdownInLine("Check **https://example.com** for info", normal)
	if !strings.Contains(stripANSI(got), "https://example.com") {
		t.Errorf("expected URL in output (inside bold), got %q", got)
	}
}

func TestRenderMarkdownInLine_complex(t *testing.T) {
	normal := lipgloss.NewStyle()
	// Markdown link, bold, and raw URL all on one line
	got := renderMarkdownInLine("See [docs](https://x.com) and **note** https://y.com", normal)
	stripped := stripANSI(got)
	if !strings.Contains(stripped, "docs") {
		t.Error("expected 'docs' (markdown link text)")
	}
	if !strings.Contains(stripped, "note") {
		t.Error("expected 'note' (bold text)")
	}
	if !strings.Contains(stripped, "https://y.com") {
		t.Error("expected raw URL")
	}
	// The markdown link URL should be stripped
	if strings.Contains(stripped, "x.com") {
		t.Error("expected markdown URL to be stripped")
	}
}

func TestRenderMarkdown_blank(t *testing.T) {
	if got := renderMarkdown("", lipgloss.NewStyle()); got != "" {
		t.Errorf("expected empty string for empty input, got %q", got)
	}
}

func TestRenderMarkdown_multiline(t *testing.T) {
	normal := lipgloss.NewStyle()
	input := "Line one\nLine two with https://example.com\n# Heading\n**bold line**"
	got := renderMarkdown(input, normal)
	stripped := stripANSI(got)
	if !strings.Contains(stripped, "Line one") || !strings.Contains(stripped, "Line two") {
		t.Error("expected both lines in output")
	}
	if !strings.Contains(stripped, "https://example.com") {
		t.Error("expected URL in output")
	}
	if !strings.Contains(stripped, "bold line") {
		t.Error("expected bold text in output")
	}
	if !strings.Contains(stripped, "Heading") {
		t.Error("expected heading text in output")
	}
	// Verify newlines are preserved
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != 4 {
		t.Errorf("expected 4 lines, got %d", len(gotLines))
	}
}

// --- Table rendering ---

func TestIsTableLine(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"| a | b |", true},
		{"  | a | b |  ", true},
		{"| --- | --- |", true},
		{"not a table", false},
		{"", false},
		{"|", true}, // degenerate but starts with |
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := isTableLine(tc.input)
			if got != tc.want {
				t.Errorf("isTableLine(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsTableSeparator(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"| --- | --- |", true},
		{"|:---|:---:|---:|", true},
		{"| a | b |", false},
		{"not a separator", false},
		{"", false},
		{"|---|", true},
		{"| - |", true},
		{"|-", false}, // doesn't end with |
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := isTableSeparator(tc.input)
			if got != tc.want {
				t.Errorf("isTableSeparator(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseTableRow(t *testing.T) {
	input := "| a | b | c |"
	got := parseTableRow(input)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cell[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseTableRowNoLeadingPipe(t *testing.T) {
	// Standard markdown allows omitting leading/trailing pipes in some parsers,
	// but our parser only handles fully-piped rows.
	input := "a | b | c"
	got := parseTableRow(input)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cell[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRenderTable_basic(t *testing.T) {
	lines := []string{
		"| Feature | Status |",
		"|---------|--------|",
		"| Login   | ✅ Done |",
		"| Logout  | ✅ Done |",
	}
	got := renderTable(lines, textStyle, markdownBoldStyle)
	if got == "" {
		t.Fatal("expected non-empty table output")
	}
	stripped := stripANSI(got)
	// Verify header present
	if !strings.Contains(stripped, "Feature") {
		t.Error("expected 'Feature' in output")
	}
	if !strings.Contains(stripped, "Status") {
		t.Error("expected 'Status' in output")
	}
	// Verify data present
	if !strings.Contains(stripped, "Login") {
		t.Error("expected 'Login' in output")
	}
	// Verify box-drawing characters present
	if !strings.Contains(stripped, "│") {
		t.Error("expected box-drawing vertical bar in output")
	}
	if !strings.Contains(stripped, "├") || !strings.Contains(stripped, "┤") {
		t.Error("expected separator junctions in output")
	}
	// Verify all lines same width (columns aligned)
	gotLines := strings.Split(stripped, "\n")
	if len(gotLines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(gotLines))
	}
	widths := make([]int, len(gotLines))
	for i, ln := range gotLines {
		widths[i] = ansi.StringWidth(ln)
	}
	for i := 1; i < len(widths); i++ {
		if widths[i] != widths[0] {
			t.Errorf("line %d width %d differs from line 0 width %d", i, widths[i], widths[0])
		}
	}
}

func TestRenderTable_noSeparator(t *testing.T) {
	// Table without a separator row — should still render as data rows.
	lines := []string{
		"| A | B |",
		"| C | D |",
	}
	got := renderTable(lines, textStyle, markdownBoldStyle)
	if got == "" {
		t.Fatal("expected non-empty table output")
	}
	stripped := stripANSI(got)
	if !strings.Contains(stripped, "A") || !strings.Contains(stripped, "B") {
		t.Error("expected header cells in output")
	}
	if !strings.Contains(stripped, "C") || !strings.Contains(stripped, "D") {
		t.Error("expected data cells in output")
	}
}

func TestRenderTable_singleRow(t *testing.T) {
	lines := []string{
		"| Only | Row |",
	}
	got := renderTable(lines, textStyle, markdownBoldStyle)
	if got != "" {
		t.Error("expected empty output for single row (needs at least 2 lines)")
	}
}

func TestRenderMarkdown_tableEmbedded(t *testing.T) {
	normal := lipgloss.NewStyle()
	input := "Here is a table:\n\n| Col1 | Col2 |\n|------|------|\n| A    | B    |\n| C    | D    |\n\nAnd some text after."
	got := renderMarkdown(input, normal)
	if got == "" {
		t.Fatal("expected non-empty output")
	}
	stripped := stripANSI(got)
	// Verify the surrounding text is preserved
	if !strings.Contains(stripped, "Here is a table") {
		t.Error("expected pre-table text")
	}
	if !strings.Contains(stripped, "And some text after") {
		t.Error("expected post-table text")
	}
	// Verify table content
	if !strings.Contains(stripped, "Col1") || !strings.Contains(stripped, "Col2") {
		t.Error("expected table header cells")
	}
	// Verify box-drawing characters
	if !strings.Contains(stripped, "│") {
		t.Error("expected box-drawing vertical bar")
	}
}

func TestRenderTable_withInlineMarkdown(t *testing.T) {
	lines := []string{
		"| **Feature** | **Description** |",
		"|-------------|-----------------|",
		"| Login       | User **login**  |",
		"| [Docs](https://example.com) | See link |",
	}
	got := renderTable(lines, textStyle, markdownBoldStyle)
	if got == "" {
		t.Fatal("expected non-empty table output")
	}
	stripped := stripANSI(got)
	if !strings.Contains(stripped, "Feature") {
		t.Error("expected 'Feature' in output")
	}
	if !strings.Contains(stripped, "login") {
		t.Error("expected 'login' in output")
	}
	if !strings.Contains(stripped, "Docs") {
		t.Error("expected 'Docs' in output")
	}
}

func TestRenderTable_wideTableFitsWidth(t *testing.T) {
	// The user's example table from the issue.
	lines := []string{
		"| `RecapModelEnabled` | `RecapModel` set | Auto-recap | Manual recap uses | Title gen uses |",
		"|---|---|---|---|---|",
		"| `true` | yes | ✅ runs | recap model | recap model |",
		"| `true` | no | ✅ runs | small → main | small → main |",
		"| `false` | yes | ❌ skipped | recap model | recap model |",
		"| `false` | no | ❌ skipped | small → main | small → main |",
	}
	got := renderTable(lines, textStyle, markdownBoldStyle)
	if got == "" {
		t.Fatal("expected non-empty table output")
	}
	stripped := stripANSI(got)
	// Verify header row (backtick-code markers are not stripped by
	// renderMarkdownInLine, so check for raw substrings).
	for _, h := range []string{"RecapModelEnabled", "RecapModel", "Auto-recap", "Manual recap uses", "Title gen uses"} {
		if !strings.Contains(stripped, h) {
			t.Errorf("expected header %q in output", h)
		}
	}
	// Verify data cells
	if !strings.Contains(stripped, "✅ runs") {
		t.Error("expected checkmark in output")
	}
	// Verify all lines are same width
	gotLines := strings.Split(stripped, "\n")
	if len(gotLines) < 5 {
		t.Fatalf("expected at least 5 lines, got %d", len(gotLines))
	}
	widths := make([]int, len(gotLines))
	for i, ln := range gotLines {
		widths[i] = ansi.StringWidth(ln)
	}
	for i := 1; i < len(widths); i++ {
		if widths[i] != widths[0] {
			t.Errorf("line %d width %d differs from line 0 width %d", i, widths[i], widths[0])
		}
	}
}
