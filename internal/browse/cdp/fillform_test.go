package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// formPage is a fake page with a text input, a native select, a checkbox and a
// file input, each reachable by a selector.
func formPage() *fakePage {
	return &fakePage{
		els: map[string]*fakeEl{
			pathNm:   {backendID: 3, count: 1, probe: elementProbe{Tag: "input", Type: "text"}},
			pathSel:  {backendID: 5, count: 1, probe: elementProbe{Tag: "select"}, options: []selectOption{{Value: "my", Text: "Malaysia"}, {Value: "sg", Text: "Singapore"}}},
			pathBox:  {backendID: 6, count: 1, probe: elementProbe{Tag: "input", Type: "checkbox"}},
			pathFile: {backendID: 9, count: 1, probe: elementProbe{Tag: "input", Type: "file"}},
		},
		findAll: map[string][]string{"#nm": {pathNm}, "#country": {pathSel}, "#terms": {pathBox}, "#cv": {pathFile}},
		live:    map[int64]string{3: pathNm, 5: pathSel, 6: pathBox, 9: pathFile},
	}
}

func field(t *testing.T, selector, value string) FormField {
	t.Helper()
	return FormField{Selector: selector, Value: json.RawMessage(value)}
}

// allowAll is the caller policy for tests that are not about paths.
func allowAll(string) error { return nil }

func TestFillFormReportsEveryFieldAndContinuesPastFailure(t *testing.T) {
	page := formPage()
	tgt := newFakePage(t, page)
	fields := []FormField{
		field(t, "#missing", `"x"`),
		field(t, "#nm", `"Ada"`),
		field(t, "#country", `"Malaysia"`),
		field(t, "#terms", `true`),
		field(t, "#nm", `null`),
	}
	reports, err := tgt.FillForm(context.Background(), "", fields, nil, allowAll)
	if err != nil {
		t.Fatalf("FillForm: %v", err)
	}
	want := []struct{ status, kind string }{
		{FieldFailed, ""}, {FieldOK, kindText}, {FieldOK, kindSelect}, {FieldOK, kindCheckbox}, {FieldSkipped, ""},
	}
	for i, w := range want {
		if reports[i].Status != w.status || (w.kind != "" && reports[i].Kind != w.kind) {
			t.Errorf("report %d = %+v, want status %s kind %q", i, reports[i], w.status, w.kind)
		}
	}
	if !strings.Contains(reports[0].Error, "no element matches") {
		t.Errorf("FAILED row error = %q", reports[0].Error)
	}
	if reports[2].Actual != `Malaysia (value "my")` {
		t.Errorf("select actual = %q", reports[2].Actual)
	}
	if reports[3].Actual != "true" {
		t.Errorf("checkbox actual = %q", reports[3].Actual)
	}
}

func TestFillFormMismatchWhenWriteDoesNotLand(t *testing.T) {
	page := formPage()
	page.ignoreWrites = true
	tgt := newFakePage(t, page)
	reports, err := tgt.FillForm(context.Background(), "", []FormField{field(t, "#nm", `"Ada"`)}, nil, allowAll)
	if err != nil {
		t.Fatalf("FillForm: %v", err)
	}
	if reports[0].Status != FieldMismatch || reports[0].Actual != "" {
		t.Errorf("report = %+v, want MISMATCH with an empty actual", reports[0])
	}
}

func TestFillFormEmptyTextFieldMatchesEmptyRequest(t *testing.T) {
	page := formPage()
	page.els[pathNm].value = "old"
	tgt := newFakePage(t, page)
	reports, err := tgt.FillForm(context.Background(), "", []FormField{field(t, "#nm", `""`)}, nil, allowAll)
	if err != nil {
		t.Fatalf("FillForm: %v", err)
	}
	if reports[0].Status != FieldOK {
		t.Errorf("empty request on a cleared field = %+v, want OK", reports[0])
	}
}

func TestFillFormFieldCapRefusedBeforeAnyCDPCall(t *testing.T) {
	tgt, s := newPageTarget(t, formPage().handle)
	fields := make([]FormField, fieldsMax+1)
	for i := range fields {
		fields[i] = field(t, "#nm", `"x"`)
	}
	if _, err := tgt.FillForm(context.Background(), "", fields, nil, allowAll); err == nil || !strings.Contains(err.Error(), "exceeds the cap") {
		t.Fatalf("FillForm(%d fields) err = %v, want the cap error", len(fields), err)
	}
	if len(s.calls) != 0 {
		t.Errorf("an over-cap form made %d CDP calls", len(s.calls))
	}
}

func TestFillFormNilAllowedAndNoFieldsRefused(t *testing.T) {
	tgt, s := newPageTarget(t, formPage().handle)
	if _, err := tgt.FillForm(context.Background(), "", []FormField{field(t, "#nm", `"x"`)}, nil, nil); err == nil || !strings.Contains(err.Error(), "nil allowed") {
		t.Errorf("nil allowed err = %v", err)
	}
	if _, err := tgt.FillForm(context.Background(), "", nil, nil, allowAll); err == nil {
		t.Errorf("empty form was accepted")
	}
	if len(s.calls) != 0 {
		t.Errorf("refused forms made %d CDP calls", len(s.calls))
	}
}

// TestFillFormFileFieldPathPolicyBlocksBeforeWrite: a path the caller refuses
// never reaches DOM.setFileInputFiles; the row is FAILED.
func TestFillFormFileFieldPathPolicyBlocksBeforeWrite(t *testing.T) {
	page := formPage()
	tgt, s := newPageTarget(t, page.handle)
	file := writeFile(t, "cv.pdf")
	var seen []string
	deny := func(p string) error {
		seen = append(seen, p)
		return fmt.Errorf("not in the allowed roots")
	}
	reports, err := tgt.FillForm(context.Background(), "", []FormField{field(t, "#cv", `"`+file+`"`)}, nil, deny)
	if err != nil {
		t.Fatalf("FillForm: %v", err)
	}
	if reports[0].Status != FieldFailed || !strings.Contains(reports[0].Error, "upload refused") {
		t.Errorf("report = %+v, want FAILED with the refusal", reports[0])
	}
	if len(seen) != 1 || seen[0] != file {
		t.Errorf("policy saw %v, want the one path", seen)
	}
	if n := s.callCount("DOM.setFileInputFiles"); n != 0 {
		t.Errorf("a refused path reached DOM.setFileInputFiles %d times", n)
	}
}

func TestFillFormFileFieldAllowedPathIsSetAndReadBack(t *testing.T) {
	page := formPage()
	tgt, s := newPageTarget(t, page.handle)
	file := writeFile(t, "cv.pdf")
	reports, err := tgt.FillForm(context.Background(), "", []FormField{field(t, "#cv", `["`+file+`"]`)}, nil, allowAll)
	if err != nil {
		t.Fatalf("FillForm: %v", err)
	}
	if reports[0].Status != FieldOK || reports[0].Kind != kindFile {
		t.Errorf("report = %+v, want OK file", reports[0])
	}
	if n := s.callCount("DOM.setFileInputFiles"); n != 1 {
		t.Errorf("DOM.setFileInputFiles calls = %d, want 1", n)
	}
}

func TestFillFormRelativeFilePathIsFailedNotSent(t *testing.T) {
	page := formPage()
	tgt, s := newPageTarget(t, page.handle)
	reports, err := tgt.FillForm(context.Background(), "", []FormField{field(t, "#cv", `"cv.pdf"`)}, nil, allowAll)
	if err != nil {
		t.Fatalf("FillForm: %v", err)
	}
	if reports[0].Status != FieldFailed || !strings.Contains(reports[0].Error, "not an absolute path") {
		t.Errorf("report = %+v, want FAILED for a relative path", reports[0])
	}
	if n := s.callCount("DOM.setFileInputFiles"); n != 0 {
		t.Errorf("a relative path reached the browser")
	}
}

func TestFillFormClipsEchoedValues(t *testing.T) {
	page := formPage()
	tgt := newFakePage(t, page)
	long := strings.Repeat("y", 1000)
	reports, err := tgt.FillForm(context.Background(), "", []FormField{field(t, "#nm", `"`+long+`"`)}, nil, allowAll)
	if err != nil {
		t.Fatalf("FillForm: %v", err)
	}
	if n := len([]rune(reports[0].Requested)); n > interactTextMax+1 {
		t.Errorf("requested echoed %d runes, cap is %d", n, interactTextMax)
	}
}

// TestFillFormRefFromSnapshot: a ref Snapshot minted is a fill-form selector. In
// one call a stale ref and an unknown ref are each a FAILED row carrying the
// reason, and the fields around them are still applied and read back OK.
func TestFillFormRefFromSnapshot(t *testing.T) {
	page := formPage()
	handle := func(method string, params json.RawMessage) (any, string) {
		if method == "Accessibility.getFullAXTree" {
			return map[string]any{"nodes": []any{
				map[string]any{"nodeId": "0", "role": map[string]any{"value": "RootWebArea"}, "name": map[string]any{"value": "Form"}, "childIds": []string{"1", "2", "3"}},
				map[string]any{"nodeId": "1", "backendDOMNodeId": 3, "role": map[string]any{"value": "textbox"}, "name": map[string]any{"value": "Name"}},
				map[string]any{"nodeId": "2", "backendDOMNodeId": 5, "role": map[string]any{"value": "combobox"}, "name": map[string]any{"value": "Country"}},
				map[string]any{"nodeId": "3", "backendDOMNodeId": 6, "role": map[string]any{"value": "checkbox"}, "name": map[string]any{"value": "Terms"}},
			}}, ""
		}
		return page.handle(method, params)
	}
	tgt, _ := newPageTarget(t, handle)
	ctx := context.Background()
	var refs RefTable
	_, minted, err := tgt.Snapshot(ctx, "", &refs)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	refOf := map[string]string{}
	for _, e := range minted {
		refOf[e.Name] = e.Ref
	}
	nameRef, countryRef, termsRef := refOf["Name"], refOf["Country"], refOf["Terms"]
	if nameRef == "" || countryRef == "" || termsRef == "" {
		t.Fatalf("snapshot minted %v, want refs for Name, Country and Terms", minted)
	}

	// The select's node leaves the document after the snapshot: its ref is stale.
	page.mu.Lock()
	delete(page.live, 5)
	page.mu.Unlock()

	reports, err := tgt.FillForm(ctx, "", []FormField{
		{Selector: nameRef, Value: []byte(`"Ada"`)},
		{Selector: countryRef, Value: []byte(`"sg"`)},
		{Selector: "@e99", Value: []byte(`"x"`)},
		{Selector: termsRef, Value: []byte(`true`)},
	}, &refs, allowAll)
	if err != nil {
		t.Fatalf("FillForm: %v", err)
	}
	want := []string{FieldOK, FieldFailed, FieldFailed, FieldOK}
	for i, w := range want {
		if reports[i].Status != w {
			t.Errorf("row %d (%s) = %s (%s), want %s", i, reports[i].Selector, reports[i].Status, reports[i].Error, w)
		}
	}
	if !strings.Contains(reports[1].Error, "stale ref") {
		t.Errorf("stale row error = %q, want the stale-ref text", reports[1].Error)
	}
	if !strings.Contains(reports[2].Error, "unknown ref") {
		t.Errorf("unknown row error = %q, want the unknown-ref text", reports[2].Error)
	}
	page.mu.Lock()
	defer page.mu.Unlock()
	if v := page.els[pathNm].value; v != "Ada" {
		t.Errorf("text field holds %q, want Ada", v)
	}
	if !page.els[pathBox].checked {
		t.Errorf("checkbox not checked")
	}
}
