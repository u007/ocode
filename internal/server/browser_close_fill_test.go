package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/tool"
)

// TestBrowserTool_FillFormUsesTheSnapshotRefTable pins that fill_form hands the
// surface the same ref table the snapshot minted its @eN refs into, so an @eN
// selector from the snapshot resolves there.
func TestBrowserTool_FillFormUsesTheSnapshotRefTable(t *testing.T) {
	surf := &fakeSurface{
		snapshotText: `- textbox "Name" [ref=@e1]`,
		reports:      []cdp.FieldReport{{Selector: "@e1", Kind: "text", Requested: "Ann", Actual: "Ann", Status: "OK"}},
	}
	b, _ := newTestBrowserTool(t, surf, 1568)
	if _, err := browserCall(t, b, `{"action":"snapshot"}`); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if _, err := browserCall(t, b, `{"action":"fill_form","fields":[{"selector":"@e1","value":"Ann"}]}`); err != nil {
		t.Fatalf("fill_form: %v", err)
	}
	refs := surf.refTables()
	if len(refs) != 2 || refs[0] == nil || refs[0] != refs[1] {
		t.Fatalf("snapshot and fill_form got different ref tables: %v", refs)
	}
	if got := surf.got(); !slices.Contains(got, `field @e1="Ann"`) {
		t.Fatalf("fill_form did not pass the @eN selector: %v", got)
	}
}

// TestBrowserTool_FillFormArrayPathBecomesAbsoluteUnderProjectRoot pins that a
// relative file in an array value is sent as its absolute, symlink-resolved path,
// and that a plain string value is passed through unchanged (it may be typed text).
func TestBrowserTool_FillFormArrayPathBecomesAbsoluteUnderProjectRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "report.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	surf := &fakeSurface{}
	b, _ := newTestBrowserTool(t, surf, 1568)
	ctx := tool.WithWorkDir(context.Background(), root)
	args := `{"action":"fill_form","fields":[{"selector":"#file","value":["report.pdf"]},{"selector":"#name","value":"report.pdf"}]}`
	if _, err := b.ExecuteCtx(ctx, json.RawMessage(args)); err != nil {
		t.Fatalf("fill_form: %v", err)
	}
	wantList, err := json.Marshal([]string{resolved})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := []string{
		"field #file=" + string(wantList),
		`field #name="report.pdf"`,
	}
	for _, w := range want {
		if !slices.Contains(surf.got(), w) {
			t.Errorf("fill_form did not pass %q: %v", w, surf.got())
		}
	}
}

// TestBrowserTool_FillFormRefusesOutsidePathBeforeAnySurfaceCall pins that a file
// in an array value outside the allowed roots is refused before the tab is
// attached and before the form is sent to Chrome.
func TestBrowserTool_FillFormRefusesOutsidePathBeforeAnySurfaceCall(t *testing.T) {
	surf := &fakeSurface{}
	b, _ := newTestBrowserTool(t, surf, 1568)
	fs := b.surfaces.(*fakeSurfaces)
	ctx := tool.WithWorkDir(context.Background(), t.TempDir())
	_, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"fill_form","fields":[{"selector":"#file","value":["/etc/hosts"]}]}`))
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("fill_form with /etc/hosts = %v, want an outside-the-workspace refusal", err)
	}
	if len(fs.surfaceCalls) != 0 || len(surf.got()) != 0 {
		t.Fatalf("refused fill_form reached Chrome: attach %v, calls %v", fs.surfaceCalls, surf.got())
	}
}

// TestBrowserTool_RefusesEncodedMetadataURLsBeforeAnyCall pins that navigate and
// open refuse every encoded metadata form before a tab is attached or Chrome is
// called, and that an https URL still goes through.
func TestBrowserTool_RefusesEncodedMetadataURLsBeforeAnyCall(t *testing.T) {
	forms := []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://2852039166/",
		"http://0xa9fea9fe/",
		"http://0251.0376.0251.0376/",
		"http://169.254.43518/",
		"http://[::ffff:a9fe:a9fe]/",
		"http://169.254.169.254./",
		"http://[fe80::1]/",
		"http://metadata.google.internal./",
		"file:///etc/passwd",
	}
	for _, u := range forms {
		for _, action := range []string{"navigate", "open"} {
			surf := &fakeSurface{}
			b, _ := newTestBrowserTool(t, surf, 1568)
			fs := b.surfaces.(*fakeSurfaces)
			args := fmt.Sprintf(`{"action":%q,"url":%q}`, action, u)
			if _, err := browserCall(t, b, args); err == nil {
				t.Errorf("%s accepted %s", action, u)
			}
			if len(fs.surfaceCalls) != 0 || len(fs.openURLs) != 0 || len(surf.got()) != 0 {
				t.Errorf("%s %s reached Chrome: attach %v, open %v, calls %v", action, u, fs.surfaceCalls, fs.openURLs, surf.got())
			}
		}
	}

	surf := &fakeSurface{}
	b, _ := newTestBrowserTool(t, surf, 1568)
	if _, err := browserCall(t, b, `{"action":"navigate","tab":"b1","url":"https://example.com/"}`); err != nil {
		t.Fatalf("https navigate refused: %v", err)
	}
	if got := surf.got(); !slices.Contains(got, "navigate https://example.com/") {
		t.Fatalf("https navigate did not reach the surface: %v", got)
	}
}

// TestBrowserTool_LoadNoteReachesTheResult pins that a page-load note from the
// browse layer is part of a successful action's result, so the model sees it.
func TestBrowserTool_LoadNoteReachesTheResult(t *testing.T) {
	surf := &fakeSurface{text: "hello"}
	b, _ := newTestBrowserTool(t, surf, 1568)
	fs := b.surfaces.(*fakeSurfaces)
	fs.loadNote = "page did not finish loading within 10s"
	out, err := browserCall(t, b, `{"action":"text"}`)
	if err != nil {
		t.Fatalf("text: %v", err)
	}
	if !strings.Contains(out, "note: page did not finish loading within 10s") {
		t.Fatalf("load note missing from result: %q", out)
	}
}

// TestBrowserTool_CloseClosesTheTabAndForgetsIt pins the close action on a tab the
// agent opened: the surface closes it, and it leaves the agent's opened list.
func TestBrowserTool_CloseClosesTheTabAndForgetsIt(t *testing.T) {
	surfaces := &fakeSurfaces{surf: &fakeSurface{}, openKey: "tab:agent1"}
	b := newBrowserTool(testProjectKey, 1568, emptyTabStore(t), surfaces)
	if _, err := browserCall(t, b, `{"action":"open"}`); err != nil {
		t.Fatalf("open: %v", err)
	}
	out, err := browserCall(t, b, `{"action":"close","tab":"agent1"}`)
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if out != "closed tab agent1" {
		t.Errorf("close output = %q", out)
	}
	if !slices.Equal(surfaces.closeKeys, []string{"tab:agent1"}) {
		t.Fatalf("Close calls = %v, want [tab:agent1]", surfaces.closeKeys)
	}
	if got := b.openedTabs(); len(got) != 0 {
		t.Fatalf("opened tabs after close = %v, want none", got)
	}
}

// TestBrowserTool_CloseNeedsTabAndDoesNotAttachIt pins that close requires an
// explicit tab, and that closing a strip tab closes it without attaching it first.
func TestBrowserTool_CloseNeedsTabAndDoesNotAttachIt(t *testing.T) {
	surf := &fakeSurface{}
	b, _ := newTestBrowserTool(t, surf, 1568)
	fs := b.surfaces.(*fakeSurfaces)
	if _, err := browserCall(t, b, `{"action":"close"}`); err == nil {
		t.Fatal("close without tab accepted")
	}
	if len(fs.closeKeys) != 0 {
		t.Fatalf("close without tab reached Close: %v", fs.closeKeys)
	}
	if _, err := browserCall(t, b, `{"action":"close","tab":"b2"}`); err != nil {
		t.Fatalf("close of strip tab: %v", err)
	}
	if !slices.Equal(fs.closeKeys, []string{"tab:b2"}) {
		t.Fatalf("Close calls = %v, want [tab:b2]", fs.closeKeys)
	}
	if len(fs.surfaceCalls) != 0 {
		t.Fatalf("close attached the tab first: %v", fs.surfaceCalls)
	}
}

// TestBrowserTool_CloseFailureKeepsTabListed pins that a tab whose close failed
// stays in the opened list, so the agent sees it and teardown tries again.
func TestBrowserTool_CloseFailureKeepsTabListed(t *testing.T) {
	surfaces := &fakeSurfaces{
		surf:      &fakeSurface{},
		openKey:   "tab:stuck",
		closeErrs: map[string]error{"tab:stuck": errors.New("no such target")},
	}
	b := newBrowserTool(testProjectKey, 1568, emptyTabStore(t), surfaces)
	if _, err := browserCall(t, b, `{"action":"open"}`); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := browserCall(t, b, `{"action":"close","tab":"stuck"}`); err == nil || !strings.Contains(err.Error(), "no such target") {
		t.Fatalf("close error = %v, want the surface failure", err)
	}
	if got := b.openedTabs(); !slices.Equal(got, []string{"stuck"}) {
		t.Fatalf("opened tabs after failed close = %v, want [stuck]", got)
	}
}

// TestBrowserTool_CloseOpenedTabsClosesEveryOwnTab pins the teardown: every tab
// the agent opened is closed, oldest first, and one failure does not stop the rest.
func TestBrowserTool_CloseOpenedTabsClosesEveryOwnTab(t *testing.T) {
	surfaces := &fakeSurfaces{
		surf:      &fakeSurface{},
		closeErrs: map[string]error{"tab:one": errors.New("gone")},
	}
	b := newBrowserTool(testProjectKey, 1568, emptyTabStore(t), surfaces)
	for _, key := range []string{"tab:one", "tab:two"} {
		surfaces.openKey = key
		if _, err := browserCall(t, b, `{"action":"open"}`); err != nil {
			t.Fatalf("open %s: %v", key, err)
		}
	}
	err := b.CloseOpenedTabs(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gone") {
		t.Fatalf("CloseOpenedTabs = %v, want the failed close joined in", err)
	}
	if !slices.Equal(surfaces.closeKeys, []string{"tab:one", "tab:two"}) {
		t.Fatalf("Close calls = %v, want both tabs attempted in order", surfaces.closeKeys)
	}
	if got := b.openedTabs(); !slices.Equal(got, []string{"one"}) {
		t.Fatalf("opened tabs after teardown = %v, want [one] (its close failed)", got)
	}
}
