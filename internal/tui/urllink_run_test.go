package tui

import (
	"strings"
	"testing"
)

// A long OAuth-style URL hard-wrapped by wordWrap onto three rows must be
// clickable from any row with the FULL url, and must copy as one token.
func TestWrappedURL_threeRows_clickAndCopy(t *testing.T) {
	url := "https://claude.com/cai/oauth/authorize?code=true&client_id=9d1c250a-e61b-44d9-88ed-5944d1962f5e&response_type=code&redirect_uri=https%3A%2F%2Fplatform.claude.com%2Foauth%2Fcode%2Fcallback&scope=org%3Acreate_api_key+user%3Aprofile&code_challenge=QGMO3uzazhn&state=Lbv_ZqZchTvEz5Zyxdt"
	lines, cont := wordWrapMarked("Open "+url, 100)
	if len(lines) < 4 {
		t.Fatalf("expected the URL to wrap onto 3+ rows, got %d: %q", len(lines), lines)
	}
	if cont[0] || cont[1] || !cont[2] || !cont[3] {
		t.Fatalf("cont flags = %v", cont)
	}
	for idx := 1; idx < len(lines); idx++ {
		first, last := contRunBounds(cont, idx)
		if first != 1 || last != len(lines)-1 {
			t.Fatalf("run bounds for %d = [%d,%d]", idx, first, last)
		}
		r, ok := urlLinkInRun(lines[first:last+1], idx-first, 3)
		if !ok {
			t.Fatalf("row %d: no url found", idx)
		}
		if r.url != url {
			t.Errorf("row %d: url truncated:\n got %q\nwant %q", idx, r.url, url)
		}
	}
	got := extractSelectionTextCont(lines, cont, 1, 0, len(lines)-1, len(lines[len(lines)-1]))
	if got != url {
		t.Errorf("copied selection:\n got %q\nwant %q", got, url)
	}
	// Word-wrapped prose still copies with newlines.
	prose, pcont := wordWrapMarked(strings.Repeat("word ", 30), 20)
	if len(prose) < 2 {
		t.Fatalf("prose did not wrap: %q", prose)
	}
	if c := extractSelectionTextCont(prose, pcont, 0, 0, 1, 4); !strings.Contains(c, "\n") {
		t.Errorf("prose copy lost its newline: %q", c)
	}
}
