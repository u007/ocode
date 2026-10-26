package tool

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
)

// pdfMagic is the signature every PDF starts with. Servers mislabel PDFs often
// enough (application/octet-stream, even text/html) that the body is sniffed
// alongside the Content-Type header.
var pdfMagic = []byte("%PDF-")

func isPDF(contentType string, body []byte) bool {
	return strings.Contains(contentType, "application/pdf") || bytes.HasPrefix(body, pdfMagic)
}

// extractPDFText returns the text of every page, one text row per line, pages
// separated by a blank line.
//
// It fails loudly on a malformed file and on a file with no extractable text
// (a scanned image), rather than handing raw bytes on. Before this, a PDF fell
// through to the HTML-to-markdown converter, and the content guardrail then
// judged 50 KB of compressed stream bytes as "obfuscation".
func extractPDFText(body []byte) (_ string, err error) {
	// ledongthuc/pdf (rsc.io/pdf lineage) panics on malformed input instead of
	// returning an error. The recover turns the panic into the error the caller
	// surfaces; the panic value is kept as the error text, nothing is dropped.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("pdf parse: %v", r)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", fmt.Errorf("pdf parse: %w", err)
	}
	var sb strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		if i > 1 {
			sb.WriteByte('\n')
		}
		sb.WriteString(pdfPageText(p))
	}
	text := sb.String()
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("pdf has no extractable text (%d pages; scanned image?)", r.NumPage())
	}
	return text, nil
}

// pdfPageText groups a page's glyph runs into rows by baseline and orders the
// rows top to bottom. The library's own GetTextByRow moves the
// baseline only on Tm, so a file that positions lines with Td or T* (LaTeX,
// many report generators) collapses into one row there; Content() tracks every
// positioning operator.
func pdfPageText(p pdf.Page) string {
	rows := map[int][]pdf.Text{}
	for _, t := range p.Content().Text {
		// The library emits a zero-width "\n" glyph at the end of each text
		// object; it is not page content and would split a row mid-word.
		// U+FFFD marks a glyph with no Unicode mapping (Symbol-font bullets'
		// trailing byte): there is nothing to show for it.
		t.S = strings.ReplaceAll(strings.ReplaceAll(t.S, "\n", ""), "\uFFFD", "")
		if t.S == "" {
			continue
		}
		y := int(math.Round(t.Y))
		rows[y] = append(rows[y], t)
	}
	ys := make([]int, 0, len(rows))
	for y := range rows {
		ys = append(ys, y)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ys)))
	var sb strings.Builder
	for _, y := range ys {
		// Glyphs stay in stream order within a row. Sorting by X looked safer
		// but the library's advance after a glyph from a CID font can be wrong,
		// which interleaves the characters of neighbouring words.
		row := rows[y]
		var line strings.Builder
		for i, t := range row {
			// Words are usually separated by position, not a space glyph: a
			// horizontal gap wider than a fraction of the font size is a space.
			if i > 0 {
				prev := row[i-1]
				if gap := t.X - (prev.X + prev.W); gap > 0.15*t.FontSize && !strings.HasSuffix(prev.S, " ") && !strings.HasPrefix(t.S, " ") {
					line.WriteByte(' ')
				}
			}
			line.WriteString(t.S)
		}
		sb.WriteString(strings.TrimRight(line.String(), " "))
		sb.WriteByte('\n')
	}
	return sb.String()
}
