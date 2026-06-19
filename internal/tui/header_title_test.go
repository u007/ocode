package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// makeTestHeaderModel builds a minimal model wired with theme styles so the
// header helper can render against real styles without depending on agent /
// session / viewport state. ApplyThemeColors also pushes styles into the
// package-level singletons (headerStyle, hintStyle, ...) used by
// renderAppHeader, so a fresh call here is required for hermetic results.
func makeTestHeaderModel() model {
	return model{ready: true, width: 120, height: 40, styles: ApplyThemeColors("tokyonight")}
}

// headerSecondLine returns the rendered title row (row index 1) of the
// 2-line app header. The top pad (row 0) is always blank.
func headerSecondLine(t *testing.T, m model, title, hint string) string {
	t.Helper()
	tabBar := renderTabBar(tabChat, false)
	exitBtn := hintStyle.Padding(0, 1).Render("✕ exit")
	header := m.renderAppHeader(title, hint, tabBar, exitBtn, m.width)
	lines := strings.Split(header, "\n")
	if len(lines) != appHeaderHeight {
		t.Fatalf("expected %d header rows, got %d:\n%q", appHeaderHeight, len(lines), header)
	}
	return lines[1]
}

// TestRenderAppHeaderShortTitleLeavesTitleIntact verifies that a title
// shorter than the available budget is rendered verbatim and the header
// stays exactly appHeaderHeight rows tall.
func TestRenderAppHeaderShortTitleLeavesTitleIntact(t *testing.T) {
	m := makeTestHeaderModel()
	title := "◆ ocode quick chat"
	hint := "·  opencode clone v0.0.0"

	row := headerSecondLine(t, m, title, hint)
	if !strings.Contains(row, "quick chat") {
		t.Fatalf("short title should be rendered verbatim, got row:\n%q", row)
	}
	if lipgloss.Height(row) != 1 {
		t.Fatalf("title row should be exactly 1 line, got %d", lipgloss.Height(row))
	}
}

// TestRenderAppHeaderLongTitleClampsToOneLine is the regression test for the
// user-reported bug: a long session title (e.g. the first user prompt) must
// be visually truncated with an ellipsis and must not soft-wrap into a
// second visual row, which would push the bottom chrome past
// appHeaderHeight.
func TestRenderAppHeaderLongTitleClampsToOneLine(t *testing.T) {
	m := makeTestHeaderModel()
	// 8 chars of leading prefix ("◆ ocode "), then a deliberately long title
	// that vastly exceeds the available header budget.
	title := "◆ ocode " + strings.Repeat("a very long session title ", 20)
	hint := "·  opencode clone v0.0.0"

	tabBar := renderTabBar(tabChat, false)
	exitBtn := hintStyle.Padding(0, 1).Render("✕ exit")
	header := m.renderAppHeader(title, hint, tabBar, exitBtn, m.width)

	if got := strings.Count(header, "\n") + 1; got != appHeaderHeight {
		t.Fatalf("header should stay %d rows, got %d:\n%q", appHeaderHeight, got, header)
	}

	row := headerSecondLine(t, m, title, hint)
	if lipgloss.Height(row) != 1 {
		t.Fatalf("title row should be exactly 1 line, got %d:\n%q", lipgloss.Height(row), row)
	}
	if !strings.Contains(row, "…") {
		t.Fatalf("long title should be truncated with ellipsis, got row:\n%q", row)
	}
	// The rendered title substring (bold blue prefix) must fit within the
	// title budget we compute. The header as a whole can still exceed
	// `m.width` if right-side chrome overflows on tight terminals — that is
	// a separate concern from the title-line clamp this helper owns.
	titleBudget := m.width -
		lipgloss.Width(tabBar) -
		lipgloss.Width(exitBtn) -
		len(appHeaderLeftPad) -
		len(appHeaderHintGap) -
		lipgloss.Width(hintStyle.Render(hint))
	if titleBudget < 1 {
		t.Fatalf("test setup error: titleBudget should be positive for the default width")
	}
	renderedTitle := m.styles.Header.Render(truncateToWidth(title, titleBudget))
	if got := lipgloss.Width(renderedTitle); got > titleBudget {
		t.Fatalf("rendered title width %d exceeds budget %d:\n%q", got, titleBudget, renderedTitle)
	}
}

// TestRenderAppHeaderShrinksTitleOnNarrowTerminal verifies that on a narrow
// terminal the title gives up width to the right-side chrome and shrinks
// first (with an ellipsis) before the hint or tab bar get truncated.
//
// The assertion is on the rendered title substring, not the full header
// row: the right-side chrome (tab bar + exit button) is laid out by the
// caller and can independently exceed the terminal width on very narrow
// terminals — that is a separate concern from the title-line clamp this
// helper is responsible for.
func TestRenderAppHeaderShrinksTitleOnNarrowTerminal(t *testing.T) {
	m := makeTestHeaderModel()
	m.width = 30 // tight: 1 col pad + title + hint gap + hint + tab bar + exit
	title := "◆ ocode " + strings.Repeat("a", 200)
	hint := "·  opencode clone v0.0.0"

	tabBar := renderTabBar(tabChat, false)
	exitBtn := hintStyle.Padding(0, 1).Render("✕ exit")
	header := m.renderAppHeader(title, hint, tabBar, exitBtn, m.width)

	if got := strings.Count(header, "\n") + 1; got != appHeaderHeight {
		t.Fatalf("header should stay %d rows on a narrow terminal, got %d:\n%q", appHeaderHeight, got, header)
	}
	row := headerSecondLine(t, m, title, hint)
	if lipgloss.Height(row) != 1 {
		t.Fatalf("title row should be exactly 1 line on a narrow terminal, got %d:\n%q", lipgloss.Height(row), row)
	}
	// The rendered title substring (bold blue prefix) must not exceed the
	// budget: the helper must always produce a single-line title regardless
	// of how much room the right-side chrome consumes.
	titleBudget := m.width -
		lipgloss.Width(tabBar) -
		lipgloss.Width(exitBtn) -
		len(appHeaderLeftPad) -
		len(appHeaderHintGap) -
		lipgloss.Width(hintStyle.Render(hint))
	renderedTitle := m.styles.Header.Render(truncateToWidth(title, maxInt(0, titleBudget)))
	if got := lipgloss.Width(renderedTitle); got > maxInt(0, titleBudget) {
		t.Fatalf("rendered title width %d exceeds budget %d:\n%q", got, titleBudget, renderedTitle)
	}
}

// TestRenderAppHeaderExtremeNarrowKeepsOneLine verifies the edge case where
// the right-side chrome consumes the entire width — the title should be
// fully elided (to "…" or "") but the header must still be a single row.
func TestRenderAppHeaderExtremeNarrowKeepsOneLine(t *testing.T) {
	m := makeTestHeaderModel()
	m.width = 12 // barely enough for tab bar + exit; no room for a long title
	title := "◆ ocode " + strings.Repeat("x", 200)
	hint := "·  opencode clone v0.0.0"

	tabBar := renderTabBar(tabChat, false)
	exitBtn := hintStyle.Padding(0, 1).Render("✕ exit")
	header := m.renderAppHeader(title, hint, tabBar, exitBtn, m.width)

	if got := strings.Count(header, "\n") + 1; got != appHeaderHeight {
		t.Fatalf("header should stay %d rows even on an extreme-narrow terminal, got %d:\n%q", appHeaderHeight, got, header)
	}
	row := headerSecondLine(t, m, title, hint)
	if lipgloss.Height(row) != 1 {
		t.Fatalf("title row should be exactly 1 line, got %d:\n%q", lipgloss.Height(row), row)
	}
}

// TestRenderAppHeaderNewlineTitleClampsToOneLine verifies that a title
// containing newlines (e.g. from a multi-line user prompt) is collapsed to a
// single row and does not break the header layout.
func TestRenderAppHeaderNewlineTitleClampsToOneLine(t *testing.T) {
	m := makeTestHeaderModel()
	title := "◆ ocode first line\nsecond line\nthird line"
	hint := "·  opencode clone v0.0.0"

	tabBar := renderTabBar(tabChat, false)
	exitBtn := hintStyle.Padding(0, 1).Render("✕ exit")
	header := m.renderAppHeader(title, hint, tabBar, exitBtn, m.width)

	if got := strings.Count(header, "\n") + 1; got != appHeaderHeight {
		t.Fatalf("header should stay %d rows with newline title, got %d:\n%q", appHeaderHeight, got, header)
	}
	row := headerSecondLine(t, m, title, hint)
	if lipgloss.Height(row) != 1 {
		t.Fatalf("title row should be exactly 1 line with newline title, got %d:\n%q", lipgloss.Height(row), row)
	}
}

// TestTruncateTitleStripsNewlines verifies that truncateTitle collapses
// newlines to spaces so multi-line prompts produce single-line titles.
func TestTruncateTitleStripsNewlines(t *testing.T) {
	title := "hello\nworld\nfoo"
	got := truncateTitle(title, 80)
	if strings.Contains(got, "\n") {
		t.Fatalf("truncateTitle should collapse newlines, got %q", got)
	}
	if got != "hello world foo" {
		t.Fatalf("truncateTitle should join lines with spaces, got %q", got)
	}
}
