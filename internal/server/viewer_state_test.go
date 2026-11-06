package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// viewerSampleJSON sets every field the tool reads, plus one unknown field that
// must be ignored.
const viewerSampleJSON = `{"path":"docs/report.pdf","kind":"pdf","location":{"page":3,"pageCount":12,"slide":2,"slideCount":8,"sheet":"Sheet1","range":"B2:D9","heading":"Results","lines":{"start":10,"end":24},"visibleLines":{"start":1,"end":40}},"selection":"the quick fox","visibleText":"line one\nline two\n","find":{"query":"fox","count":4,"current":2},"control":{"id":"c1","status":"ok","error":"stale"},"unknownField":true}`

func putViewerState(t *testing.T, h *Handler, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/sessions/"+id+"/viewer", strings.NewReader(body))
	h.HandlePutSessionViewer(rec, req, id)
	return rec
}

func runViewerStateTool(t *testing.T, h *Handler, id string) (string, error) {
	t.Helper()
	tl := &viewerStateTool{h: h, sessionID: id}
	return tl.Execute(json.RawMessage(`{}`))
}

func TestViewerStatePutStoresAndToolFormatsEveryField(t *testing.T) {
	h := NewHandler()
	if rec := putViewerState(t, h, "ses_a", viewerSampleJSON); rec.Code != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want 204; body %s", rec.Code, rec.Body.String())
	}

	out, err := runViewerStateTool(t, h, "ses_a")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	lines := strings.Split(out, "\n")
	want := []string{
		"Preview: docs/report.pdf (pdf)",
		"Location: p.3/12, slide 2/8, Sheet1!B2:D9, § Results, L10-L24, view L1-L40",
		`Selection: "the quick fox"`,
		"Visible text:",
		"line one",
		"line two",
		`Find: "fox" 4 matches, current 2`,
		"Last control: c1 ok (stale)",
	}
	if len(lines) != len(want)+1 {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(want)+1, out)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d = %q, want %q", i, lines[i], w)
		}
	}
	if !strings.HasPrefix(lines[len(lines)-1], "Updated: ") || !strings.HasSuffix(lines[len(lines)-1], "s ago") {
		t.Errorf("last line = %q, want an \"Updated: <n>s ago\" line", lines[len(lines)-1])
	}
}

func TestViewerStateToolOmitsAbsentFields(t *testing.T) {
	h := NewHandler()
	if rec := putViewerState(t, h, "ses_b", `{"path":"notes.md","location":{"page":0,"sheet":"","heading":"Intro"}}`); rec.Code != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want 204", rec.Code)
	}
	out, err := runViewerStateTool(t, h, "ses_b")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.HasPrefix(out, "Preview: notes.md\nLocation: § Intro\n") {
		t.Errorf("output = %q, want header and heading location only", out)
	}
	for _, absent := range []string{"Selection:", "Visible text:", "Find:", "Last control:"} {
		if strings.Contains(out, absent) {
			t.Errorf("output contains %q for an absent field:\n%s", absent, out)
		}
	}
}

func TestViewerStateToolNoPreviewBeforePut(t *testing.T) {
	h := NewHandler()
	out, err := runViewerStateTool(t, h, "ses_none")
	if err != nil {
		t.Fatalf("Execute with no preview returned error %v, want a normal result", err)
	}
	if out != "no preview open for this session" {
		t.Errorf("output = %q", out)
	}
}

func TestViewerStateDeleteClearsPreview(t *testing.T) {
	h := NewHandler()
	putViewerState(t, h, "ses_c", viewerSampleJSON)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/ses_c/viewer", nil)
	h.HandleDeleteSessionViewer(rec, req, "ses_c")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", rec.Code)
	}
	out, err := runViewerStateTool(t, h, "ses_c")
	if err != nil || out != "no preview open for this session" {
		t.Fatalf("after DELETE: out=%q err=%v, want no preview", out, err)
	}
}

func TestViewerStatePutRejectsOversizedBody(t *testing.T) {
	h := NewHandler()
	body := `{"path":"` + strings.Repeat("a", maxViewerStateBody) + `"}`
	if rec := putViewerState(t, h, "ses_d", body); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized PUT status = %d, want 413", rec.Code)
	}
	if out, _ := runViewerStateTool(t, h, "ses_d"); out != "no preview open for this session" {
		t.Errorf("oversized body was stored: %q", out)
	}
}

func TestViewerStatePutRejectsNonObjectJSON(t *testing.T) {
	h := NewHandler()
	for _, body := range []string{"not json", `[1,2]`, `"text"`, `null`, ``} {
		if rec := putViewerState(t, h, "ses_e", body); rec.Code != http.StatusBadRequest {
			t.Errorf("PUT body %q status = %d, want 400", body, rec.Code)
		}
	}
	if out, _ := runViewerStateTool(t, h, "ses_e"); out != "no preview open for this session" {
		t.Errorf("rejected body was stored: %q", out)
	}
}

func TestViewerStateToolErrorsOnFieldTypeMismatch(t *testing.T) {
	h := NewHandler()
	if rec := putViewerState(t, h, "ses_f", `{"location":{"page":"three"}}`); rec.Code != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want 204 (object is valid JSON)", rec.Code)
	}
	if _, err := runViewerStateTool(t, h, "ses_f"); err == nil {
		t.Fatal("Execute with a mistyped field returned no error")
	}
}

func TestViewerStateRekeyMovesRecord(t *testing.T) {
	h := NewHandler()
	putViewerState(t, h, "ses_old", viewerSampleJSON)

	h.rekeyViewerState("ses_old", "ses_new")

	if _, ok := h.viewerState("ses_old"); ok {
		t.Error("record still under the old id after rekey")
	}
	out, err := runViewerStateTool(t, h, "ses_new")
	if err != nil || !strings.HasPrefix(out, "Preview: docs/report.pdf") {
		t.Errorf("after rekey: out=%q err=%v, want the moved record", out, err)
	}
}

func TestViewerStateRekeyWithoutRecordIsNoop(t *testing.T) {
	h := NewHandler()
	h.rekeyViewerState("ses_missing", "ses_new")
	if _, ok := h.viewerState("ses_new"); ok {
		t.Error("rekey with no source record created one under the new id")
	}
}

func TestViewerStateDropForgetsRecord(t *testing.T) {
	h := NewHandler()
	putViewerState(t, h, "ses_g", viewerSampleJSON)
	h.dropViewerState("ses_g")
	if out, _ := runViewerStateTool(t, h, "ses_g"); out != "no preview open for this session" {
		t.Errorf("after drop: %q", out)
	}
}

func TestViewerStateToolRegisteredForBuiltSession(t *testing.T) {
	root := t.TempDir()
	h := newCronToolHandler(t, root)
	as := buildCronToolAgent(t, h, root)

	for _, def := range as.agent.GetToolDefinitions() {
		if def["name"] == "viewer_state" {
			return
		}
	}
	t.Fatal("viewer_state is not in the tool definitions of a session built by the normal path")
}
