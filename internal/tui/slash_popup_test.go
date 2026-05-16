package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

func TestSlashSuggestionsEmptyPrefixReturnsAll(t *testing.T) {
	got := slashSuggestions("/")
	if len(got) == 0 {
		t.Fatal("expected all commands returned for bare /")
	}
}

func TestSlashSuggestionsFiltersByPrefix(t *testing.T) {
	got := slashSuggestions("/co")
	if len(got) == 0 {
		t.Fatal("expected at least one /co command")
	}
	for _, item := range got {
		if !strings.HasPrefix(item.name, "/co") {
			t.Errorf("unexpected suggestion %q does not start with /co", item.name)
		}
	}
}

func TestSlashSuggestionsNoMatchReturnsEmpty(t *testing.T) {
	got := slashSuggestions("/zzznomatch")
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestSlashSuggestionHasNameAndDesc(t *testing.T) {
	got := slashSuggestions("/help")
	if len(got) == 0 {
		t.Fatal("expected /help suggestion")
	}
	if got[0].name != "/help" {
		t.Errorf("expected name=/help, got %q", got[0].name)
	}
	if got[0].desc == "" {
		t.Error("expected non-empty desc for /help")
	}
}

func TestSlashPopupStateDefaults(t *testing.T) {
	m := model{}
	if m.showSlashPopup {
		t.Error("showSlashPopup should default false")
	}
	if m.slashPopupIndex != 0 {
		t.Error("slashPopupIndex should default 0")
	}
	if m.slashPopupItems != nil {
		t.Error("slashPopupItems should default nil")
	}
}

func TestSlashPopupShowsWhenInputStartsWithSlash(t *testing.T) {
	m := model{input: newTestTextarea()}
	m.input.SetValue("/co")
	m = m.updateSlashPopupState()
	if !m.showSlashPopup {
		t.Fatal("expected popup to show for /co input")
	}
	if len(m.slashPopupItems) == 0 {
		t.Fatal("expected items populated for /co")
	}
}

func TestSlashPopupHidesWhenInputHasSpace(t *testing.T) {
	m := model{input: newTestTextarea(), showSlashPopup: true}
	m.input.SetValue("/compact ")
	m = m.updateSlashPopupState()
	if m.showSlashPopup {
		t.Fatal("expected popup to hide when input contains space")
	}
}

func TestSlashPopupHidesWhenInputNotSlash(t *testing.T) {
	m := model{input: newTestTextarea(), showSlashPopup: true}
	m.input.SetValue("hello")
	m = m.updateSlashPopupState()
	if m.showSlashPopup {
		t.Fatal("expected popup to hide for non-slash input")
	}
}

func TestSlashPopupHidesWhenOtherModalOpen(t *testing.T) {
	m := model{input: newTestTextarea(), showPicker: true}
	m.input.SetValue("/co")
	m = m.updateSlashPopupState()
	if m.showSlashPopup {
		t.Fatal("expected popup to hide when showPicker is true")
	}
}

func TestSlashPopupDownArrowMovesIndex(t *testing.T) {
	m := model{
		input:           newTestTextarea(),
		showSlashPopup:  true,
		slashPopupIndex: 0,
		slashPopupItems: []slashSuggestion{{name: "/a", desc: "first"}, {name: "/b", desc: "second"}},
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	got := updated.(model)
	if got.slashPopupIndex != 1 {
		t.Errorf("expected index 1, got %d", got.slashPopupIndex)
	}
}

func TestSlashPopupUpArrowClampsAtZero(t *testing.T) {
	m := model{
		input:           newTestTextarea(),
		showSlashPopup:  true,
		slashPopupIndex: 0,
		slashPopupItems: []slashSuggestion{{name: "/a", desc: "first"}},
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	got := updated.(model)
	if got.slashPopupIndex != 0 {
		t.Errorf("expected index clamped at 0, got %d", got.slashPopupIndex)
	}
}

func TestSlashPopupEscClosesPopup(t *testing.T) {
	m := model{input: newTestTextarea(), showSlashPopup: true, slashPopupItems: []slashSuggestion{{name: "/a", desc: "x"}}}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got := updated.(model)
	if got.showSlashPopup {
		t.Fatal("expected popup closed after Esc")
	}
}

func TestSlashPopupEnterInsertsCommandAndClosesPopup(t *testing.T) {
	m := model{
		input:           newTestTextarea(),
		showSlashPopup:  true,
		slashPopupIndex: 1,
		slashPopupItems: []slashSuggestion{{name: "/a", desc: "first"}, {name: "/compact", desc: "Reduce context"}},
	}
	m.input.SetValue("/co")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(model)
	if got.showSlashPopup {
		t.Fatal("expected popup closed after Enter")
	}
	if got.input.Value() != "/compact " {
		t.Errorf("expected input '/compact ', got %q", got.input.Value())
	}
}

func TestSlashPopupEnterRunsExactCommand(t *testing.T) {
	m := model{
		input:           newTestTextarea(),
		messages:        []message{{role: roleUser, text: "keep"}},
		showSlashPopup:  true,
		slashPopupItems: []slashSuggestion{{name: "/compact", desc: "Reduce context"}},
	}
	m.input.SetValue("/compact")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := derefTestModel(t, updated)
	if got.showSlashPopup {
		t.Fatal("expected popup closed after exact command runs")
	}
	if len(got.messages) != 2 {
		t.Fatalf("expected compact command to run, got %d messages", len(got.messages))
	}
	if !strings.Contains(got.messages[1].text, "Conversation compacted") {
		t.Fatalf("expected compact status message, got %q", got.messages[1].text)
	}
}

func TestSlashPopupEnterRunsAliasWithoutSuggestions(t *testing.T) {
	m := model{input: newTestTextarea(), showSlashPopup: true}
	m.input.SetValue("/clear")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := derefTestModel(t, updated)
	if len(got.messages) == 0 || !strings.Contains(got.messages[len(got.messages)-1].text, "Started new session") {
		t.Fatalf("expected /clear alias to run /new, got %#v", got.messages)
	}
}

func TestSlashPopupMouseClickSelectsRow(t *testing.T) {
	m := model{
		input:           newTestTextarea(),
		showSlashPopup:  true,
		slashPopupItems: []slashSuggestion{{name: "/compact", desc: "Reduce context"}, {name: "/connect", desc: "Show API keys"}},
	}
	idx, ok := m.slashPopupRowForY(m.viewport.Height() + 5)
	if !ok {
		t.Fatal("expected row hit")
	}
	if idx != 1 {
		t.Errorf("expected row index 1, got %d", idx)
	}
}

func TestSlashPopupMouseClickUsesVisibleWindow(t *testing.T) {
	m := model{
		input:           newTestTextarea(),
		showSlashPopup:  true,
		slashPopupIndex: 8,
		slashPopupItems: []slashSuggestion{
			{name: "/0", desc: "zero"},
			{name: "/1", desc: "one"},
			{name: "/2", desc: "two"},
			{name: "/3", desc: "three"},
			{name: "/4", desc: "four"},
			{name: "/5", desc: "five"},
			{name: "/6", desc: "six"},
			{name: "/7", desc: "seven"},
			{name: "/8", desc: "eight"},
		},
	}
	idx, ok := m.slashPopupRowForY(m.viewport.Height() + 4)
	if !ok {
		t.Fatal("expected first visible row hit")
	}
	if idx != 1 {
		t.Errorf("expected first visible row to map to index 1, got %d", idx)
	}
}

func TestSlashPopupAppearsInLayout(t *testing.T) {
	m := model{
		input:           newTestTextarea(),
		viewport:        viewport.New(viewport.WithWidth(80), viewport.WithHeight(10)),
		width:           80,
		height:          30,
		ready:           true,
		showSlashPopup:  true,
		slashPopupItems: []slashSuggestion{{name: "/compact", desc: "Reduce context"}},
	}
	content := m.renderContent()
	if !strings.Contains(content, "/compact") {
		t.Error("expected /compact to appear in rendered content when popup is shown")
	}
}

func newTestTextarea() textarea.Model {
	ta := textarea.New()
	ta.Focus()
	return ta
}

func derefTestModel(t *testing.T, value tea.Model) model {
	t.Helper()
	switch got := value.(type) {
	case model:
		return got
	case *model:
		return *got
	default:
		t.Fatalf("expected model, got %T", value)
		return model{}
	}
}
