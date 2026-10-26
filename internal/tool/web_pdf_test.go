package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// minimalPDF builds a one-page, uncompressed PDF whose single content stream
// draws text, with a correct xref table so the parser accepts it.
func minimalPDF(lines []string) []byte {
	var stream strings.Builder
	stream.WriteString("BT /F1 12 Tf 72 720 Td\n")
	for _, l := range lines {
		// An explicit Td per line: the row grouper keys on the text position.
		fmt.Fprintf(&stream, "(%s) Tj 0 -14 Td\n", l)
	}
	stream.WriteString("ET")
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", stream.Len(), stream.String()),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

func TestExtractPDFTextReturnsRowsAsLines(t *testing.T) {
	got, err := extractPDFText(minimalPDF([]string{"REVENUE MEMORANDUM CIRCULAR NO. 58-2023", "Second row of text"}))
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}
	want := "REVENUE MEMORANDUM CIRCULAR NO. 58-2023\nSecond row of text\n"
	if got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestExtractPDFTextRejectsMalformedInput(t *testing.T) {
	if _, err := extractPDFText([]byte("%PDF-1.4\ngarbage with no xref")); err == nil {
		t.Fatal("malformed PDF must return an error, not raw bytes")
	}
}

func TestExtractPDFTextRejectsTextlessPDF(t *testing.T) {
	_, err := extractPDFText(minimalPDF(nil))
	if err == nil || !strings.Contains(err.Error(), "no extractable text") {
		t.Fatalf("textless PDF must be reported as such, got %v", err)
	}
}

func TestIsPDFSniffsBodyWhenHeaderLies(t *testing.T) {
	if !isPDF("application/pdf", []byte("whatever")) {
		t.Error("Content-Type application/pdf must be recognised")
	}
	if !isPDF("text/html", []byte("%PDF-1.7 ...")) {
		t.Error("a %PDF- body must be recognised even under a wrong Content-Type")
	}
	if isPDF("text/html", []byte("<html>")) {
		t.Error("HTML must not be taken for a PDF")
	}
}

// The tool used to push PDF bytes through the HTML-to-markdown converter. The
// model must receive the document's text, never the compressed stream bytes.
func TestWebFetchReturnsPDFTextNotBytes(t *testing.T) {
	body := minimalPDF([]string{"Hello from a PDF"})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	prev := webFetchClient
	webFetchClient = srv.Client()
	t.Cleanup(func() { webFetchClient = prev })

	args, _ := json.Marshal(map[string]string{"url": srv.URL + "/doc.pdf"})
	got, err := WebFetchTool{}.Execute(args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.TrimSpace(got) != "Hello from a PDF" {
		t.Fatalf("result = %q, want the extracted text", got)
	}
	if strings.Contains(got, "%PDF") || strings.Contains(got, "endobj") {
		t.Fatalf("result leaks raw PDF syntax: %q", got)
	}
}
