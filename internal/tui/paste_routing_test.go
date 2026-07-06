package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/tui/fastviewport"
)

// TestPasteMsgRoutesToPickerFilter verifies that a paste while a picker is
// open lands in pickerFilterPending and is debounced via a tick cmd.
func TestPasteMsgRoutesToPickerFilter(t *testing.T) {
	m := model{
		showPicker:          true,
		pickerKind:          "model",
		pickerItems:         []string{"openai/gpt-4o-mini"},
		pickerValues:        []string{"openai/gpt-4o-mini"},
		pickerFilterSeq:     7,
		pickerFilterPending: "",
	}

	updated, cmd := m.Update(tea.PasteMsg{Content: "gpt"})
	got := derefTestModel(t, updated)

	if got.pickerFilterPending != "gpt" {
		t.Fatalf("expected pickerFilterPending=%q, got %q", "gpt", got.pickerFilterPending)
	}
	if got.pickerFilterSeq != 8 {
		t.Fatalf("expected pickerFilterSeq to increment, got %d", got.pickerFilterSeq)
	}
	if cmd == nil {
		t.Fatal("expected debounce tick cmd after paste, got nil")
	}
	// Filter must remain empty until the debounce fires.
	if got.pickerFilter != "" {
		t.Fatalf("expected pickerFilter to remain empty before debounce, got %q", got.pickerFilter)
	}
}

// TestPasteMsgRoutesToSessionPickerTriggersLoad verifies the session-picker
// paste path: when the picker is paginated and the filter is empty, the
// first paste triggers loadAllSessions and is batched with the debounce
// tick. The pasted text is kept (not dropped like the old keypress path).
func TestPasteMsgRoutesToSessionPickerTriggersLoad(t *testing.T) {
	m := model{
		showPicker:          true,
		pickerKind:          "session",
		pickerSessionMore:   true,
		pickerSessionTotal:  100,
		pickerFilterSeq:     3,
		pickerFilterPending: "",
	}

	updated, cmd := m.Update(tea.PasteMsg{Content: "foo"})
	got := derefTestModel(t, updated)

	if got.pickerFilterPending != "foo" {
		t.Fatalf("expected pasted content to be kept in pickerFilterPending, got %q", got.pickerFilterPending)
	}
	if got.pickerFilterSeq != 4 {
		t.Fatalf("expected pickerFilterSeq to increment, got %d", got.pickerFilterSeq)
	}
	if cmd == nil {
		t.Fatal("expected batched load+tick cmd after session paste, got nil")
	}
}

// TestPasteMsgRoutesToFileSearch verifies that a paste while the ctrl+P
// file search is open is appended to fileSearchInput and the results are
// re-filtered.
func TestPasteMsgRoutesToFileSearch(t *testing.T) {
	m := model{
		showFileSearch:    true,
		fileSearchCache:   []fileSearchResult{{path: "main.go", fileName: "main.go"}, {path: "test.go", fileName: "test.go"}},
		fileSearchResults: []fileSearchResult{{path: "main.go", fileName: "main.go"}, {path: "test.go", fileName: "test.go"}},
		fileSearchIndex:   0,
	}

	updated, _ := m.Update(tea.PasteMsg{Content: "main"})
	got := derefTestModel(t, updated)

	if got.fileSearchInput != "main" {
		t.Fatalf("expected fileSearchInput=%q, got %q", "main", got.fileSearchInput)
	}
	if len(got.fileSearchResults) == 0 {
		t.Fatal("expected at least one result after pasting 'main'")
	}
}

// TestPasteMsgRoutesToChatSearch verifies that a paste while the ctrl+F
// chat find bar is open is forwarded to the chatSearchInput.
func TestPasteMsgRoutesToChatSearch(t *testing.T) {
	vp := fastviewport.New(80, 20)
	ta := newTestTextarea()
	ta.SetWidth(40)
	ti := textinput.New()
	ti.Prompt = ""
	// openChatSearch focuses the textinput once the model is ready; mirror
	// that here so textinput.Update() is not a no-op on the un-focused input.
	ti.Focus()

	m := model{
		ready:                  true,
		width:                  120,
		height:                 40,
		activeTab:              tabChat,
		input:                  ta,
		viewport:               vp,
		chatSearchInput:        ti,
		chatSearchActive:       true,
		transcriptMsgStartLine: []int{},
	}

	updated, _ := m.Update(tea.PasteMsg{Content: "secret"})
	got := derefTestModel(t, updated)

	if got.chatSearchInput.Value() != "secret" {
		t.Fatalf("expected chatSearchInput=%q, got %q", "secret", got.chatSearchInput.Value())
	}
}

// TestPasteMsgRoutesToDetailSearch verifies that a paste into the in-view
// find bar on a detail view is forwarded to the top of the detail stack.
func TestPasteMsgRoutesToDetailSearch(t *testing.T) {
	vp := fastviewport.New(80, 20)
	ta := newTestTextarea()
	ta.SetWidth(40)
	si := textinput.New()
	si.Prompt = ""
	// Mirror the focus state the detail-view find bar has in production
	// (openDetailSearch calls Focus once the model is ready). Without
	// this, textinput.Update is a no-op for an un-focused input.
	si.Focus()

	m := model{
		ready:     true,
		width:     120,
		height:    40,
		activeTab: tabChat,
		input:     ta,
		viewport:  vp,
		detail:    detailStack{{searchInput: si, searchActive: true}},
	}

	updated, _ := m.Update(tea.PasteMsg{Content: "query"})
	got := derefTestModel(t, updated)

	if got.detail[len(got.detail)-1].searchInput.Value() != "query" {
		t.Fatalf("expected top.detail.searchInput=%q, got %q", "query", got.detail[len(got.detail)-1].searchInput.Value())
	}
}

// TestPasteMsgRoutesToLogSearch verifies that a paste on the log tab is
// appended to logSearch and the log viewport is refreshed.
func TestPasteMsgRoutesToLogSearch(t *testing.T) {
	m := model{
		activeTab: tabLog,
		logSearch: "tool",
	}

	updated, _ := m.Update(tea.PasteMsg{Content: "error"})
	got := derefTestModel(t, updated)

	if got.logSearch != "toolerror" {
		t.Fatalf("expected logSearch=%q, got %q", "toolerror", got.logSearch)
	}
}

// TestPasteMsgSanitisesNewlinesForPlainFilters verifies the three plain-
// string filter inputs (file search, log search, picker filter) collapse
// newlines to spaces so a multi-line paste still produces a usable single-
// line filter.
func TestPasteMsgSanitisesNewlinesForPlainFilters(t *testing.T) {
	t.Run("file search", func(t *testing.T) {
		m := model{showFileSearch: true}
		got := derefTestModel(t, mustUpdate(t, m, tea.PasteMsg{Content: "foo\nbar"}))
		if got.fileSearchInput != "foo bar" {
			t.Fatalf("expected newlines collapsed to space, got %q", got.fileSearchInput)
		}
	})
	t.Run("log search", func(t *testing.T) {
		m := model{activeTab: tabLog}
		got := derefTestModel(t, mustUpdate(t, m, tea.PasteMsg{Content: "foo\nbar"}))
		if got.logSearch != "foo bar" {
			t.Fatalf("expected newlines collapsed to space, got %q", got.logSearch)
		}
	})
	t.Run("picker filter", func(t *testing.T) {
		m := model{showPicker: true, pickerKind: "model", pickerFilterSeq: 1}
		got := derefTestModel(t, mustUpdate(t, m, tea.PasteMsg{Content: "foo\nbar"}))
		if got.pickerFilterPending != "foo bar" {
			t.Fatalf("expected newlines collapsed to space, got %q", got.pickerFilterPending)
		}
	})
}

// TestPasteMsgCapsFilterLength verifies the cap on paste length for the
// plain-string filter inputs. A paste of 2x the cap must be truncated to
// maxPasteFilterLen runes so the title line does not overflow the panel.
func TestPasteMsgCapsFilterLength(t *testing.T) {
	huge := strings.Repeat("x", maxPasteFilterLen*2)
	m := model{showFileSearch: true}
	got := derefTestModel(t, mustUpdate(t, m, tea.PasteMsg{Content: huge}))
	want := strings.Repeat("x", maxPasteFilterLen)
	if got.fileSearchInput != want {
		t.Fatalf("expected fileSearchInput to be capped to %d runes, got %d",
			maxPasteFilterLen, len([]rune(got.fileSearchInput)))
	}
}

// TestKeyPressKeepsFirstCharInSessionPickerFilter verifies the keypress
// path for the session picker is consistent with the paste path: when
// loadAllSessions is triggered, the typed character is kept and a
// debounce tick is scheduled, not dropped.
func TestKeyPressKeepsFirstCharInSessionPickerFilter(t *testing.T) {
	m := model{
		showPicker:          true,
		pickerKind:          "session",
		pickerSessionMore:   true,
		pickerSessionTotal:  100,
		pickerFilterSeq:     0,
		pickerFilterPending: "",
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got := derefTestModel(t, updated)

	if got.pickerFilterPending != "f" {
		t.Fatalf("expected first keystroke to be kept in pickerFilterPending, got %q", got.pickerFilterPending)
	}
	if got.pickerFilterSeq != 1 {
		t.Fatalf("expected pickerFilterSeq to increment, got %d", got.pickerFilterSeq)
	}
	if cmd == nil {
		t.Fatal("expected batched load+tick cmd after first session keystroke, got nil")
	}
}

// TestPasteMsgEmptyDoesNotTouchAnyFilter verifies that an empty paste
// never writes to any filter field, even when the surface is open.
func TestPasteMsgEmptyDoesNotTouchAnyFilter(t *testing.T) {
	cases := []struct {
		name string
		m    model
		get  func(*model) string
	}{
		{"picker", model{showPicker: true, pickerKind: "model", pickerFilterPending: "keep"},
			func(m *model) string { return m.pickerFilterPending }},
		{"file search", model{showFileSearch: true, fileSearchInput: "keep"},
			func(m *model) string { return m.fileSearchInput }},
		{"log search", model{activeTab: tabLog, logSearch: "keep"},
			func(m *model) string { return m.logSearch }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := derefTestModel(t, mustUpdate(t, tc.m, tea.PasteMsg{Content: ""}))
			if tc.get(&got) != "keep" {
				t.Fatalf("expected filter to remain %q, got %q", "keep", tc.get(&got))
			}
		})
	}
}

// mustUpdate is a small helper that runs Update and returns the typed
// tea.Model result so the test can chain derefTestModel without an
// intermediate variable.
func mustUpdate(t *testing.T, m model, msg tea.Msg) tea.Model {
	t.Helper()
	updated, _ := m.Update(msg)
	return updated
}
