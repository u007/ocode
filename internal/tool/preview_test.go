package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

func previewArgs(t *testing.T, path string, page int) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]interface{}{"path": path, "page": page})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPreviewOpenToolAcceptsPreviewable(t *testing.T) {
	tool := &PreviewOpenTool{}
	for _, p := range []string{"docs/deck.pptx", "spec.docx", "report.pdf", "flow.mmd", "notes.md", "main.go"} {
		got, err := tool.Execute(previewArgs(t, p, 0))
		if err != nil {
			t.Errorf("path %q: unexpected error %v", p, err)
			continue
		}
		if !strings.HasPrefix(got, PreviewOpenSentinel) {
			t.Errorf("path %q: result %q missing sentinel", p, got)
		}
		if !strings.Contains(got, "page=1") {
			t.Errorf("path %q: default page not applied: %q", p, got)
		}
	}
}

func TestPreviewOpenToolRespectsPage(t *testing.T) {
	tool := &PreviewOpenTool{}
	got, err := tool.Execute(previewArgs(t, "report.pdf", 4))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "page=4") {
		t.Errorf("page not carried: %q", got)
	}
}

func TestPreviewOpenToolRejects(t *testing.T) {
	tool := &PreviewOpenTool{}
	cases := []struct {
		name string
		path string
		page int
	}{
		{"empty", "", 0},
		{"absolute", "/etc/passwd", 0},
		{"traversal", "../secret.txt", 0},
		{"dotdot", "a/../../etc/passwd", 0},
		{"unknown ext", "movie.mkv", 0},
		{"binary", "app.exe", 0},
		{"negative page", "report.pdf", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tool.Execute(previewArgs(t, tc.path, tc.page)); err == nil {
				t.Errorf("expected error for path %q page %d", tc.path, tc.page)
			}
		})
	}
}

func TestPreviewOpenToolName(t *testing.T) {
	if (&PreviewOpenTool{}).Name() != "preview_open" {
		t.Error("tool name must be preview_open")
	}
}
