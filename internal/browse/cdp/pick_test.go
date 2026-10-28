package cdp

import (
	"context"
	"strings"
	"testing"
	"time"
)

// comboPage is a fake page with one custom combobox at pathCombo whose menu
// shows the given option lists in turn.
func comboPage(combo [][]string) *fakePage {
	return &fakePage{
		els: map[string]*fakeEl{
			pathCombo: {backendID: 8, count: 1, probe: elementProbe{Tag: "input", Type: "text", Role: "combobox"}, combo: combo, comboText: ""},
		},
		findAll: map[string][]string{"#combo": {pathCombo}},
		live:    map[int64]string{8: pathCombo},
	}
}

func TestPickComboboxClicksExactOptionAndVerifiesDisplay(t *testing.T) {
	page := comboPage([][]string{{"Malaysia", "Singapore"}})
	page.els[pathCombo].comboText = "Malaysia"
	tgt := newFakePage(t, page)

	res, err := tgt.Pick(context.Background(), "", "#combo", "Malaysia", nil)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if res.Mode != "combobox" || res.Selected != "Malaysia" {
		t.Errorf("Pick = %+v, want combobox mode showing Malaysia", res)
	}
	if got := strings.Join(page.inserted, "|"); got != "Malaysia" {
		t.Errorf("typed %q, want the requested text", got)
	}
	if got := strings.Join(page.clickLog(), "|"); got != "mouseMoved 10,20|mousePressed 10,20|mouseReleased 10,20" {
		t.Errorf("clicks = %q, want the option's centre", got)
	}
}

func TestPickComboboxAmbiguousListsOptionsAndClosesMenu(t *testing.T) {
	page := comboPage([][]string{{"Malaysia", "Malay"}})
	tgt := newFakePage(t, page)

	_, err := tgt.Pick(context.Background(), "", "#combo", "mal", nil)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("Pick err = %v, want an ambiguous error", err)
	}
	if !strings.Contains(err.Error(), `"Malaysia"`) || !strings.Contains(err.Error(), `"Malay"`) {
		t.Errorf("ambiguous error does not list the tied options: %v", err)
	}
	if !hasOption(page.keys, "Escape") {
		t.Errorf("menu was not closed with Escape after the failure: keys=%v", page.keys)
	}
	if n := len(page.clickLog()); n != 0 {
		t.Errorf("an ambiguous pick clicked %d times", n)
	}
}

func TestPickComboboxNoMatchListsAvailableOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("no-match waits out pickOptionsWait (3s)")
	}
	page := comboPage([][]string{{"Singapore", "Japan"}})
	tgt := newFakePage(t, page)

	start := time.Now()
	_, err := tgt.Pick(context.Background(), "", "#combo", "Malaysia", nil)
	if err == nil || !strings.Contains(err.Error(), `no option matching "Malaysia"`) {
		t.Fatalf("Pick err = %v, want a no-match error", err)
	}
	if !strings.Contains(err.Error(), `"Singapore", "Japan"`) {
		t.Errorf("no-match error does not list the available options: %v", err)
	}
	if time.Since(start) < pickOptionsWait {
		t.Errorf("no-match returned after %s, before the %s poll window", time.Since(start), pickOptionsWait)
	}
	if !hasOption(page.keys, "Escape") {
		t.Errorf("menu was not closed with Escape: keys=%v", page.keys)
	}
}

func TestPickSelectReturnsSelectMode(t *testing.T) {
	page := &fakePage{
		els: map[string]*fakeEl{pathSel: {backendID: 5, count: 1, probe: elementProbe{Tag: "select"}, options: []selectOption{
			{Value: "", Text: "Choose"},
			{Value: "my", Text: "Malaysia"},
		}}},
		findAll: map[string][]string{"#country": {pathSel}},
		live:    map[int64]string{5: pathSel},
	}
	tgt := newFakePage(t, page)
	res, err := tgt.Pick(context.Background(), "", "#country", "malay", nil)
	if err != nil {
		t.Fatalf("Pick(select): %v", err)
	}
	if res.Mode != "select" || res.Selected != "Malaysia" || page.els[pathSel].selValue != "my" {
		t.Errorf("Pick(select) = %+v, value %q", res, page.els[pathSel].selValue)
	}
}

// TestPickCrossOriginFrameIsAClearError: a frame whose document is not reachable
// fails with resolveFrame's error, and no element is touched.
func TestPickCrossOriginFrameIsAClearError(t *testing.T) {
	page := comboPage([][]string{{"Malaysia"}})
	page.crossFrame = true
	tgt, s := newPageTarget(t, page.handle)

	_, err := tgt.Pick(context.Background(), "#cross", "#combo", "Malaysia", nil)
	if err == nil || !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("Pick(cross-origin frame) err = %v, want the cross-origin error", err)
	}
	if n := len(page.inserted) + len(page.keys) + len(page.clickLog()); n != 0 {
		t.Errorf("a cross-origin frame still reached the page: %d actions", n)
	}
	if s.callCount("Input.insertText") != 0 {
		t.Errorf("text was typed into a cross-origin frame's target")
	}
}

func TestPickRejectsEmptyText(t *testing.T) {
	tgt, s := newPageTarget(t, comboPage(nil).handle)
	if _, err := tgt.Pick(context.Background(), "", "#combo", "   ", nil); err == nil {
		t.Fatalf("Pick with blank text was accepted")
	}
	if len(s.calls) != 0 {
		t.Errorf("blank pick made %d CDP calls", len(s.calls))
	}
}

func TestPickSettledWaitsForExactOptionOnWeakMatch(t *testing.T) {
	// A prefix match on a changing list is not final: the exact option may still load.
	choice, _, settled := pickSettled(nil, []string{"Malay"}, "mal")
	if choice != 0 || settled {
		t.Errorf("first poll of a weak match: choice=%d settled=%v, want 0 and not settled", choice, settled)
	}
	choice, _, settled = pickSettled([]string{"Malay"}, []string{"Malay"}, "mal")
	if choice != 0 || !settled {
		t.Errorf("unchanged weak match: choice=%d settled=%v, want settled", choice, settled)
	}
	choice, _, settled = pickSettled(nil, []string{"Malaysia", "Malay"}, "malaysia")
	if choice != 0 || !settled {
		t.Errorf("exact match: choice=%d settled=%v, want settled at once", choice, settled)
	}
}

func TestPickListOptionsCapsCountAndTextLength(t *testing.T) {
	many := make([]string, pickListMax+5)
	for i := range many {
		many[i] = "opt"
	}
	out := listOptions(many)
	if !strings.Contains(out, "and 5 more") {
		t.Errorf("listOptions did not report the 5 omitted options: %s", out)
	}
	long := strings.Repeat("x", 500)
	if got := listOptions([]string{long}); len([]rune(got)) > pickOptionTextMax+10 {
		t.Errorf("listOptions echoed %d runes of one option; cap is %d", len([]rune(got)), pickOptionTextMax)
	}
}

func hasOption(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
