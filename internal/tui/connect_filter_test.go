package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/auth"
)

// connectFilterModel builds a model with the connect dialog freshly opened, so
// the filter input is constructed and focused exactly the way /connect does it.
func connectFilterModel(t *testing.T) model {
	t.Helper()
	m := model{
		ready:  true,
		width:  100,
		height: 30,
		input:  textarea.New(),
		styles: ApplyThemeColors("tokyonight"),
	}
	m.openConnectDialog()
	if m.connect == nil || !m.showConnect {
		t.Fatal("openConnectDialog did not open the provider stage")
	}
	return m
}

// typeConnectFilter feeds printable runes into whatever input the dialog has
// focused, mimicking real typing.
func typeConnectFilter(t *testing.T, m model, s string) model {
	t.Helper()
	for _, r := range s {
		next, _ := m.updateConnectDialog(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = derefTestModel(t, next)
	}
	return m
}

func pressConnect(t *testing.T, m model, code rune, s string) model {
	t.Helper()
	next, _ := m.updateConnectDialog(tea.KeyPressMsg{Code: code, Text: s})
	return derefTestModel(t, next)
}

// rowIdxs reduces matched rows to the auth.Providers indices the tests assert
// against.
func rowIdxs(rows []connectProviderRow) []int {
	out := make([]int, len(rows))
	for i, r := range rows {
		out[i] = r.idx
	}
	return out
}

// connectProviderIdxs is the auth.Providers indices the provider stage is
// currently showing, which is what the tests reason about.
func connectProviderIdxs(d *connectDialog) []int {
	return rowIdxs(d.visibleProviderRows())
}

func TestConnectProviderFilterMatchesLabelIDAndStatus(t *testing.T) {
	d := &connectDialog{}

	if got := rowIdxs(d.visibleProviderRows()); len(got) != len(auth.Providers) {
		t.Fatalf("empty query should list every provider, got %d of %d", len(got), len(auth.Providers))
	}

	// A provider id is always matchable even when its label differs.
	byID := rowIdxs(d.filteredProviderRows("openrouter"))
	if len(byID) != 1 || auth.Providers[byID[0]].ID != "openrouter" {
		t.Fatalf("expected exactly the openrouter provider, got %v", byID)
	}

	// Label matches are case-insensitive and fuzzy, so "OpenAI" also matches
	// "OpenAI Codex" but nothing unrelated.
	byLabel := rowIdxs(d.filteredProviderRows("  OPENAI  "))
	if !containsInt(byLabel, 0) || !containsInt(byLabel, 29) {
		t.Fatalf("expected openai and its Codex sibling to match, got %v", byLabel)
	}
	if containsInt(byLabel, 2) { // google
		t.Fatalf("google should not match an openai query, got %v", byLabel)
	}

	// The status detail is searchable so a user can list configured providers.
	if _, detail := auth.Status("openai"); detail != "" {
		if got := rowIdxs(d.filteredProviderRows(detail)); len(got) == 0 {
			t.Fatalf("status detail %q should be searchable", detail)
		}
	}

	if got := rowIdxs(d.filteredProviderRows("definitely-not-a-provider")); len(got) != 0 {
		t.Fatalf("expected no matches, got %v", got)
	}
}

func TestConnectProviderFilterTypingNarrowsListAndEnterSelectsMatch(t *testing.T) {
	m := connectFilterModel(t)

	m = typeConnectFilter(t, m, "openrouter")
	if got := connectProviderIdxs(m.connect); len(got) != 1 {
		t.Fatalf("expected the list to narrow to one provider, got %d", len(got))
	}

	m = pressConnect(t, m, tea.KeyEnter, "")
	if m.connect.stage != connectStageMethod {
		t.Fatalf("expected enter to open the method stage, got stage %d", m.connect.stage)
	}
	if m.connect.provider == nil || m.connect.provider.ID != "openrouter" {
		t.Fatalf("expected openrouter to be selected, got %#v", m.connect.provider)
	}
}

func TestConnectProviderFilterNavigationStaysWithinMatches(t *testing.T) {
	m := connectFilterModel(t)
	// "a" matches several providers; assert we never leave the match set.
	m = typeConnectFilter(t, m, "a")
	matches := connectProviderIdxs(m.connect)
	if len(matches) < 2 {
		t.Fatalf("test needs a filter matching at least two providers, got %v", matches)
	}

	for i := 0; i < len(matches)+3; i++ {
		m = pressConnect(t, m, tea.KeyDown, "")
		if !containsInt(connectProviderIdxs(m.connect), m.connect.providerIdx) {
			t.Fatalf("down arrow moved the selection outside the filtered set (idx %d)", m.connect.providerIdx)
		}
	}
	// Walking down past the last match pins to the last one, never further.
	if m.connect.providerIdx != matches[len(matches)-1] {
		t.Fatalf("expected selection pinned to last match %d, got %d", matches[len(matches)-1], m.connect.providerIdx)
	}

	for i := 0; i < len(matches)+3; i++ {
		m = pressConnect(t, m, tea.KeyUp, "")
	}
	if m.connect.providerIdx != matches[0] {
		t.Fatalf("expected selection pinned to first match %d, got %d", matches[0], m.connect.providerIdx)
	}
}

func TestConnectProviderFilterMouseRowMapsThroughFilteredList(t *testing.T) {
	m := connectFilterModel(t)
	m = typeConnectFilter(t, m, "a")
	matches := connectProviderIdxs(m.connect)
	if len(matches) < 2 {
		t.Fatalf("test needs a filter matching at least two providers, got %v", matches)
	}

	// Rows start right below the filter row: y=3 is the first provider.
	idx, ok := m.connectRowForY(4)
	if !ok || idx != 1 {
		t.Fatalf("expected filtered row 1, got idx %d ok %v", idx, ok)
	}

	updated, cmd := m.selectConnectRow(idx)
	if cmd != nil {
		t.Fatal("expected provider selection to stay in the dialog without a command")
	}
	got := derefTestModel(t, updated)
	if got.connect.provider == nil || got.connect.provider != &auth.Providers[matches[1]] {
		t.Fatalf("expected click to select filtered provider %d, got %#v", matches[1], got.connect.provider)
	}
}

func TestConnectProviderFilterRowHitTestStopsAtLastMatch(t *testing.T) {
	m := connectFilterModel(t)
	m = typeConnectFilter(t, m, "openrouter")
	last := len(connectProviderIdxs(m.connect)) // exactly one match
	if last != 1 {
		t.Fatalf("expected exactly one match, got %d", last)
	}
	if _, ok := m.connectRowForY(3 + last); ok {
		t.Fatal("a row past the last match must not be selectable")
	}
	if _, ok := m.connectRowForY(2); ok {
		t.Fatal("the filter input row must not be selectable as a provider")
	}
}

func TestConnectProviderFilterEscClearsThenCloses(t *testing.T) {
	m := connectFilterModel(t)
	m = typeConnectFilter(t, m, "openrouter")

	m = pressConnect(t, m, tea.KeyEsc, "")
	if !m.showConnect {
		t.Fatal("esc with a filter set should clear the filter, not close the dialog")
	}
	if m.connect.filterInput.Value() != "" {
		t.Fatalf("expected the filter to be cleared, got %q", m.connect.filterInput.Value())
	}
	if got := len(connectProviderIdxs(m.connect)); got != len(auth.Providers) {
		t.Fatalf("expected the full provider list back, got %d", got)
	}

	m = pressConnect(t, m, tea.KeyEsc, "")
	if m.showConnect {
		t.Fatal("esc on an empty filter should close the dialog")
	}
}

func TestConnectProviderFilterBackspaceWidensResults(t *testing.T) {
	m := connectFilterModel(t)
	m = typeConnectFilter(t, m, "grok")
	before := connectProviderIdxs(m.connect)
	if len(before) == 0 {
		t.Fatal("precondition: grok should match something")
	}

	m = pressConnect(t, m, tea.KeyBackspace, "")
	if got := m.connect.filterInput.Value(); got != "gro" {
		t.Fatalf("expected backspace to edit the filter, got %q", got)
	}
	after := connectProviderIdxs(m.connect)
	if len(after) <= len(before) {
		t.Fatalf("expected the list to widen after backspace, got %v (was %v)", after, before)
	}
	if !containsInt(after, m.connect.providerIdx) {
		t.Fatalf("selection %d escaped the match set %v", m.connect.providerIdx, after)
	}
}

func TestConnectProviderFilterNoMatchesEnterIsNoOp(t *testing.T) {
	m := connectFilterModel(t)
	m = typeConnectFilter(t, m, "zzz-nothing")

	if got := len(connectProviderIdxs(m.connect)); got != 0 {
		t.Fatalf("expected no matches, got %v", got)
	}
	out := m.renderConnect()
	if !strings.Contains(stripANSI(out), "No providers match") {
		t.Fatalf("expected a no-match message, got:\n%s", stripANSI(out))
	}
	// Pin the dialog's shape: the message must occupy exactly one body row. A
	// stray newline inside the styled message used to add a second blank row.
	lines := strings.Split(stripANSI(out), "\n")
	if len(lines) != 7 {
		t.Fatalf("expected 7 rendered rows (border, header, filter, message, spacer, hint, border), got %d:\n%s", len(lines), stripANSI(out))
	}
	if !strings.Contains(lines[3], "No providers match") {
		t.Fatalf("expected the message on row 3, got %q", lines[3])
	}
	if inner := strings.Trim(strings.TrimSpace(lines[4]), "│ "); inner != "" {
		t.Fatalf("expected a blank spacer row before the hint, got %q", inner)
	}
	if !strings.Contains(lines[5], "type to filter") {
		t.Fatalf("expected the hint on row 5, got %q", lines[5])
	}

	// Enter with no matches must not advance the stage or index-crash.
	before := m.connect.stage
	m = pressConnect(t, m, tea.KeyEnter, "")
	if m.connect.stage != before {
		t.Fatalf("enter with no matches advanced the stage to %d", m.connect.stage)
	}
	if _, ok := m.connectRowForY(3); ok {
		t.Fatal("no row is selectable when nothing matches")
	}
	// Up/down with no matches must be inert too.
	m = pressConnect(t, m, tea.KeyDown, "")
	m = pressConnect(t, m, tea.KeyUp, "")
}

func TestConnectProviderFilterSnapsSelectionWhenItStopsMatching(t *testing.T) {
	m := connectFilterModel(t)
	m = typeConnectFilter(t, m, "openrouter")
	m = pressConnect(t, m, tea.KeyEnter, "")
	if m.connect.providerIdx == 0 {
		t.Fatal("precondition: openrouter should not be the first catalog provider")
	}

	// Back to the provider stage and retype a query that excludes it.
	m = pressConnect(t, m, tea.KeyEsc, "")
	m = typeConnectFilter(t, m, "anthropic")
	if got := len(connectProviderIdxs(m.connect)); got == 0 {
		t.Fatal("expected anthropic to match something")
	}
	if !containsInt(connectProviderIdxs(m.connect), m.connect.providerIdx) {
		t.Fatalf("selection %d stayed outside the new match set %v", m.connect.providerIdx, connectProviderIdxs(m.connect))
	}
}

func TestConnectProviderFilterClearsOnMethodBack(t *testing.T) {
	m := connectFilterModel(t)
	m = typeConnectFilter(t, m, "openrouter")
	m = pressConnect(t, m, tea.KeyEnter, "")
	if m.connect.stage != connectStageMethod {
		t.Fatalf("expected method stage, got %d", m.connect.stage)
	}

	m = pressConnect(t, m, tea.KeyEsc, "")
	if m.connect.stage != connectStageProvider {
		t.Fatalf("expected to return to the provider stage, got %d", m.connect.stage)
	}
	// Leaving the provider stage resets the filter, otherwise the next
	// character typed appends to the stale query.
	if m.connect.filterInput.Value() != "" {
		t.Fatalf("expected the filter to be cleared on leaving the provider stage, got %q", m.connect.filterInput.Value())
	}
	if got := len(connectProviderIdxs(m.connect)); got != len(auth.Providers) {
		t.Fatalf("expected the full provider list back, got %d", got)
	}
	// The provider that was chosen stays highlighted.
	if m.connect.provider == nil || m.connect.provider.ID != "openrouter" {
		t.Fatalf("expected openrouter to stay selected, got %#v", m.connect.provider)
	}
}

func TestConnectProviderFilterRenderedRowKeepsProviderListOnRow3(t *testing.T) {
	m := connectFilterModel(t)
	out := stripANSI(m.renderConnect())
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("rendered dialog too short:\n%s", out)
	}
	// Row 1 header, row 2 filter input, row 3 first provider — the same offset
	// the provider list had before the filter row existed.
	if !strings.Contains(lines[1], "Connect provider") {
		t.Fatalf("expected header on row 1, got %q", lines[1])
	}
	if !strings.Contains(lines[2], "openrouter") && !strings.Contains(lines[2], "filter") {
		t.Fatalf("expected the filter input on row 2, got %q", lines[2])
	}
	if strings.TrimSpace(lines[3]) == "" {
		t.Fatalf("expected the first provider on row 3, got %q", lines[3])
	}
	if !strings.Contains(out, auth.Providers[0].Label) {
		t.Fatalf("expected the unfiltered provider list, got:\n%s", out)
	}
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
