package ide

import (
	"encoding/json"
	"testing"
)

func TestParseSelection_RangesVariant(t *testing.T) {
	in := `{"filePath":"/a/b.go","ranges":[{"text":"foo","selection":{"start":{"line":9,"character":2},"end":{"line":14,"character":0}}}]}`
	sel, ok := parseSelectionParams(json.RawMessage(in))
	if !ok {
		t.Fatal("expected ok")
	}
	if sel.FilePath != "/a/b.go" || len(sel.Ranges) != 1 {
		t.Fatalf("unexpected selection: %+v", sel)
	}
	// 0-based on the wire -> 1-based for display.
	start, end, ok := sel.LineSpan()
	if !ok || start != 10 || end != 15 {
		t.Fatalf("LineSpan=%d-%d ok=%v, want 10-15", start, end, ok)
	}
}

func TestParseSelection_FlatVariant(t *testing.T) {
	in := `{"filePath":"/a/b.go","text":"bar","selection":{"start":{"line":0,"character":0},"end":{"line":0,"character":3}}}`
	sel, ok := parseSelectionParams(json.RawMessage(in))
	if !ok || len(sel.Ranges) != 1 || sel.Ranges[0].Text != "bar" {
		t.Fatalf("unexpected: %+v ok=%v", sel, ok)
	}
}

func TestParseSelection_SuccessFalse(t *testing.T) {
	in := `{"success":false,"message":"No active editor found"}`
	if _, ok := parseSelectionParams(json.RawMessage(in)); ok {
		t.Fatal("expected ok=false for success:false")
	}
}

func TestSelectionKey_Stable(t *testing.T) {
	a := &Selection{FilePath: "/x", Ranges: []Range{{StartLine: 1, EndLine: 3, Text: "t"}}}
	b := &Selection{FilePath: "/x", Ranges: []Range{{StartLine: 1, EndLine: 3, Text: "t"}}}
	c := &Selection{FilePath: "/x", Ranges: []Range{{StartLine: 1, EndLine: 4, Text: "t"}}}
	if SelectionKey(a) != SelectionKey(b) {
		t.Fatal("identical selections must share a key")
	}
	if SelectionKey(a) == SelectionKey(c) {
		t.Fatal("different selections must differ")
	}
	if SelectionKey(nil) != "" {
		t.Fatal("nil selection key must be empty")
	}
}

func TestToolText_And_ParseOpenEditors(t *testing.T) {
	result := `{"content":[{"type":"text","text":"{\"tabs\":[{\"fileName\":\"/a/x.ts\",\"label\":\"x.ts\",\"isActive\":true,\"isDirty\":false,\"isUntitled\":false},{\"fileName\":\"Untitled-1\",\"label\":\"Untitled-1\",\"isUntitled\":true}]}"}]}`
	inner := toolText(json.RawMessage(result))
	if len(inner) == 0 {
		t.Fatal("toolText returned empty")
	}
	eds, ok := parseOpenEditors(inner)
	if !ok {
		t.Fatal("parseOpenEditors not ok")
	}
	if len(eds) != 1 {
		t.Fatalf("expected 1 real editor (untitled skipped), got %d", len(eds))
	}
	if eds[0].FilePath != "/a/x.ts" || !eds[0].Active {
		t.Fatalf("unexpected editor: %+v", eds[0])
	}
}

func TestParseMention(t *testing.T) {
	in := `{"filePath":"/a/b.go","lineStart":5,"lineEnd":9}`
	m, ok := parseMentionParams(json.RawMessage(in))
	if !ok || m.FilePath != "/a/b.go" || m.LineStart != 5 || m.LineEnd != 9 {
		t.Fatalf("unexpected mention: %+v ok=%v", m, ok)
	}
}
