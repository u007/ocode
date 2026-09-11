package cdp

import (
	"strings"
	"testing"
)

func TestFindExpr_EscapesQuery(t *testing.T) {
	queries := []string{
		`hello`,
		`he"llo`,
		"line1\nline2",
		`back\slash`,
		`<script>alert(1)</script>`,
		"tab\tsep",
		"unicode \u2315 \u2715",
		"",
	}
	for _, q := range queries {
		expr, err := findExpr(q, false, false)
		if err != nil {
			t.Fatalf("findExpr(%q): %v", q, err)
		}
		// The raw query must never appear unescaped when it contains
		// JS-breaking characters; the JSON-encoded form must appear.
		if strings.Contains(q, `"`) && strings.Contains(expr, `const q = "`+q+`"`) {
			t.Fatalf("findExpr(%q) interpolates raw quotes", q)
		}
		if strings.Contains(q, "\n") && strings.Contains(expr, "\nline2") {
			t.Fatalf("findExpr(%q) interpolates a raw newline", q)
		}
		if !strings.Contains(expr, "window.find(q, caseSensitive, backwards") {
			t.Fatalf("findExpr(%q) missing window.find call", q)
		}
		if !strings.Contains(expr, "window.__ocodeFind") {
			t.Fatalf("findExpr(%q) missing find state", q)
		}
	}
}

func TestFindExpr_Flags(t *testing.T) {
	fwd, err := findExpr("x", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fwd, "const caseSensitive = false") || !strings.Contains(fwd, "const backwards = false") {
		t.Fatalf("forward/insensitive flags wrong:\n%s", fwd)
	}
	back, err := findExpr("x", true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(back, "const caseSensitive = true") || !strings.Contains(back, "const backwards = true") {
		t.Fatalf("backward/sensitive flags wrong:\n%s", back)
	}
}

func TestFindClearExpr_ClearsSelection(t *testing.T) {
	if !strings.Contains(findClearExpr, "removeAllRanges") {
		t.Fatal("findClearExpr must clear the selection")
	}
	if !strings.Contains(findClearExpr, "window.__ocodeFind = null") {
		t.Fatal("findClearExpr must drop find state")
	}
}
