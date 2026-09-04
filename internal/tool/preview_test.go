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
	for _, p := range []string{"docs/deck.pptx", "spec.docx", "report.pdf", "flow.mmd", "notes.md", "main.go", "budget.xlsx", "legacy.xls", "data.csv"} {
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
		{"legacy doc (OS-open fallback, never previewed)", "old.doc", 0},
		{"legacy ppt (OS-open fallback, never previewed)", "old.ppt", 0},
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

// TestPreviewOpenCoversOfficeSet pins the office formats the sidebar can
// preview. It must stay synchronized with HandleFileRaw.previewRawTypes in
// internal/server (raw bytes) and kindByExt in web/src/lib/previewKind.ts
// (renderer routing): a format accepted here but missing there would be an
// AI-promised preview that can never render.
func TestPreviewOpenCoversOfficeSet(t *testing.T) {
	office := map[string]string{
		".pdf": "pdf", ".docx": "docx", ".pptx": "pptx",
		".xlsx": "excel", ".xls": "excel", ".csv": "excel",
	}
	for ext, want := range office {
		got, ok := previewOpenKinds[ext]
		if !ok {
			t.Errorf("previewOpenKinds missing %q", ext)
			continue
		}
		if got != want {
			t.Errorf("previewOpenKinds[%q] = %q, want %q", ext, got, want)
		}
	}
}
