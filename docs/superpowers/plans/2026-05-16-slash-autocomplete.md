# Slash Command Autocomplete Popup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a live filtered popup list of slash commands (name + description) when the user types `/` in the chat input, navigable with arrow keys, selectable with Enter or mouse click.

**Architecture:** New file `slash_popup.go` handles the `slashSuggestion` type, filtering, rendering, and close helper. `model.go` gains three new state fields, trigger logic after every keystroke, key/mouse intercepts, and a layout insertion. No changes to `commands.go` — all new code is isolated in the new file.

**Tech Stack:** Go, BubbleTea v2 (`charm.land/bubbletea/v2`), Lipgloss v2 (`charm.land/lipgloss/v2`), existing `borderStyle`/`hintStyle` from `theme.go`.

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/tui/slash_popup.go` | **Create** | `slashSuggestion` type, `slashSuggestions()` filter, `renderSlashPopup()`, `closeSlashPopup()` |
| `internal/tui/model.go` | **Modify** | Add 3 state fields; trigger logic; key/mouse intercepts; layout insertion |
| `internal/tui/slash_popup_test.go` | **Create** | Unit tests for filtering, key nav, selection, mouse click |

---

## Task 1: `slashSuggestion` type and `slashSuggestions()` filter

**Files:**
- Create: `internal/tui/slash_popup.go`
- Create: `internal/tui/slash_popup_test.go`

- [ ] **Step 1: Write failing tests for `slashSuggestions`**

Create `internal/tui/slash_popup_test.go`:

```go
package tui

import (
	"testing"
)

func TestSlashSuggestionsEmptyPrefixReturnsAll(t *testing.T) {
	got := slashSuggestions("/")
	if len(got) == 0 {
		t.Fatal("expected all commands returned for bare /")
	}
}

func TestSlashSuggestionsFiltersByPrefix(t *testing.T) {
	got := slashSuggestions("/co")
	for _, s := range got {
		if len(s.name) < 3 || s.name[:3] != "/co" {
			t.Errorf("unexpected suggestion %q does not start with /co", s.name)
		}
	}
	if len(got) == 0 {
		t.Fatal("expected at least /compact and /connect for prefix /co")
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
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/tui/... -run TestSlashSuggestions -v
```

Expected: `undefined: slashSuggestions`

- [ ] **Step 3: Create `internal/tui/slash_popup.go` with `slashSuggestion` type and `slashSuggestions()` function**

```go
package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/jamesmercstudio/ocode/internal/commands"
)

type slashSuggestion struct {
	name string
	desc string
}

// slashSuggestions returns all slash commands whose name starts with prefix.
// prefix must start with "/". Passing "/" returns all commands.
func slashSuggestions(prefix string) []slashSuggestion {
	lower := strings.ToLower(prefix)
	var out []slashSuggestion
	seen := make(map[string]struct{})

	for _, spec := range commandSpecs {
		if strings.HasPrefix(spec.name, lower) {
			if _, ok := seen[spec.name]; !ok {
				out = append(out, slashSuggestion{name: spec.name, desc: spec.help})
				seen[spec.name] = struct{}{}
			}
		}
	}

	for _, cmd := range loadedCustomCommands {
		name := "/" + cmd.Name
		if strings.HasPrefix(strings.ToLower(name), lower) {
			if _, ok := seen[name]; !ok {
				out = append(out, slashSuggestion{name: name, desc: cmd.Description})
				seen[name] = struct{}{}
			}
		}
	}

	return out
}

func (m *model) closeSlashPopup() {
	m.showSlashPopup = false
	m.slashPopupIndex = 0
	m.slashPopupItems = nil
}

func (m model) renderSlashPopup() string {
	nameStyle := lipgloss.NewStyle().Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89"))
	selectedBg := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#1A1B26")).
		Background(lipgloss.Color("#7AA2F7"))

	hint := hintStyle.Render("↑/↓/Tab select · Enter confirm · Esc cancel")

	items := m.slashPopupItems
	const maxRows = 8
	start := 0
	if m.slashPopupIndex >= maxRows {
		start = m.slashPopupIndex - maxRows + 1
	}
	end := start + maxRows
	if end > len(items) {
		end = len(items)
	}

	var body strings.Builder
	if len(items) == 0 {
		body.WriteString(hintStyle.Render("(no matching commands)"))
		body.WriteString("\n")
	} else {
		// compute column width for alignment
		maxNameLen := 0
		for _, s := range items[start:end] {
			if len(s.name) > maxNameLen {
				maxNameLen = len(s.name)
			}
		}
		for i := start; i < end; i++ {
			s := items[i]
			padded := fmt.Sprintf("%-*s", maxNameLen, s.name)
			line := nameStyle.Render(padded) + "  " + descStyle.Render(s.desc)
			if i == m.slashPopupIndex {
				line = selectedBg.Render(" " + padded + "  " + s.desc + " ")
			}
			body.WriteString(line)
			body.WriteString("\n")
		}
	}

	width := m.panelWidth() - 2
	if width < 40 {
		width = 40
	}
	return borderStyle.Width(width).Render(body.String() + hint)
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/tui/... -run TestSlashSuggestions -v
```

Expected: all 4 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/slash_popup.go internal/tui/slash_popup_test.go
git commit -m "feat: add slashSuggestion type, filter, render, and close helpers"
```

---

## Task 2: Add state fields to `model` struct

**Files:**
- Modify: `internal/tui/model.go` (struct definition ~line 82, initializer ~line 261)

- [ ] **Step 1: Write a failing test that checks popup state fields exist**

Add to `internal/tui/slash_popup_test.go`:

```go
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
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/tui/... -run TestSlashPopupStateDefaults -v
```

Expected: compile error — `m.showSlashPopup undefined`

- [ ] **Step 3: Add the three fields to `model` struct**

In `internal/tui/model.go`, find the block with `showPicker`, `pickerItems`, etc. (around line 95). Add after `pickerFilter string`:

```go
	showSlashPopup  bool
	slashPopupIndex int
	slashPopupItems []slashSuggestion
```

- [ ] **Step 4: Run test to confirm it passes**

```bash
go test ./internal/tui/... -run TestSlashPopupStateDefaults -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/slash_popup_test.go
git commit -m "feat: add slash popup state fields to model struct"
```

---

## Task 3: Trigger logic — show/hide popup on every keystroke

**Files:**
- Modify: `internal/tui/model.go` (`Update` function, after `m.input.Update(msg)` at ~line 334)

- [ ] **Step 1: Write failing tests for trigger logic**

Add to `internal/tui/slash_popup_test.go`:

```go
func TestSlashPopupShowsWhenInputStartsWithSlash(t *testing.T) {
	m := model{input: newTestTextarea()}
	m.input.SetValue("/co")
	m2, _ := m.updateSlashPopupState()
	if !m2.showSlashPopup {
		t.Fatal("expected popup to show for /co input")
	}
	if len(m2.slashPopupItems) == 0 {
		t.Fatal("expected items populated for /co")
	}
}

func TestSlashPopupHidesWhenInputHasSpace(t *testing.T) {
	m := model{input: newTestTextarea(), showSlashPopup: true}
	m.input.SetValue("/compact ")
	m2, _ := m.updateSlashPopupState()
	if m2.showSlashPopup {
		t.Fatal("expected popup to hide when input contains space")
	}
}

func TestSlashPopupHidesWhenInputNotSlash(t *testing.T) {
	m := model{input: newTestTextarea(), showSlashPopup: true}
	m.input.SetValue("hello")
	m2, _ := m.updateSlashPopupState()
	if m2.showSlashPopup {
		t.Fatal("expected popup to hide for non-slash input")
	}
}

func TestSlashPopupHidesWhenOtherModalOpen(t *testing.T) {
	m := model{input: newTestTextarea(), showPicker: true}
	m.input.SetValue("/co")
	m2, _ := m.updateSlashPopupState()
	if m2.showSlashPopup {
		t.Fatal("expected popup to hide when showPicker is true")
	}
}
```

Add helper at bottom of test file:

```go
func newTestTextarea() textarea.Model {
	ta := textarea.New()
	return ta
}
```

Add import for textarea to test file imports:

```go
import (
	"testing"

	"charm.land/bubbles/v2/textarea"
)
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/tui/... -run TestSlashPopup -v
```

Expected: `undefined: updateSlashPopupState`

- [ ] **Step 3: Add `updateSlashPopupState` method to `slash_popup.go`**

Add this function at the bottom of `internal/tui/slash_popup.go`:

```go
// updateSlashPopupState recalculates whether the slash popup should be shown
// and what items it contains, based on the current input value.
// Returns the updated model and nil cmd (for chaining in Update).
func (m model) updateSlashPopupState() (model, tea.Cmd) {
	val := m.input.Value()
	if strings.HasPrefix(val, "/") && !strings.Contains(val, " ") &&
		!m.showPicker && !m.showConnect && !m.showPalette {
		m.slashPopupItems = slashSuggestions(val)
		m.showSlashPopup = true
		// clamp index in case items shrunk
		if m.slashPopupIndex >= len(m.slashPopupItems) {
			m.slashPopupIndex = 0
		}
	} else {
		m.showSlashPopup = false
		m.slashPopupIndex = 0
		m.slashPopupItems = nil
	}
	return m, nil
}
```

Add `tea "charm.land/bubbletea/v2"` to the imports in `slash_popup.go` (it already imports strings/fmt/lipgloss — add tea).

- [ ] **Step 4: Wire `updateSlashPopupState` into `Update()` in `model.go`**

In `model.go`, the textarea update happens at ~line 334:
```go
m.input, tiCmd = m.input.Update(msg)
m.viewport, vpCmd = m.viewport.Update(msg)
```

Immediately after those two lines, add:

```go
	m, _ = m.updateSlashPopupState()
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/tui/... -run TestSlashPopup -v
```

Expected: all 4 trigger tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tui/slash_popup.go internal/tui/slash_popup_test.go internal/tui/model.go
git commit -m "feat: auto-show slash popup on keystroke when input starts with /"
```

---

## Task 4: Key handling — ↑/↓/Tab/Enter/Esc intercepts

**Files:**
- Modify: `internal/tui/model.go` (`Update` key handling, ~line 345)

- [ ] **Step 1: Write failing tests for key navigation**

Add to `internal/tui/slash_popup_test.go`:

```go
func TestSlashPopupDownArrowMovesIndex(t *testing.T) {
	m := model{
		showSlashPopup:  true,
		slashPopupIndex: 0,
		slashPopupItems: []slashSuggestion{
			{name: "/a", desc: "first"},
			{name: "/b", desc: "second"},
		},
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	got := updated.(model)
	if got.slashPopupIndex != 1 {
		t.Errorf("expected index 1, got %d", got.slashPopupIndex)
	}
}

func TestSlashPopupUpArrowClampsAtZero(t *testing.T) {
	m := model{
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
	m := model{
		input:           newTestTextarea(),
		showSlashPopup:  true,
		slashPopupItems: []slashSuggestion{{name: "/a", desc: "x"}},
	}
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
		slashPopupItems: []slashSuggestion{
			{name: "/a", desc: "first"},
			{name: "/compact", desc: "Reduce context"},
		},
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(model)
	if got.showSlashPopup {
		t.Fatal("expected popup closed after Enter")
	}
	if got.input.Value() != "/compact " {
		t.Errorf("expected input '/compact ', got %q", got.input.Value())
	}
}
```

Add `tea "charm.land/bubbletea/v2"` to test file imports.

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/tui/... -run TestSlashPopupDown|TestSlashPopupUp|TestSlashPopupEsc|TestSlashPopupEnter -v
```

Expected: FAIL (key handling not yet wired)

- [ ] **Step 3: Add slash popup key intercept block in `Update()`**

In `model.go`, find the `case tea.KeyPressMsg:` switch. The existing `if m.showPicker {` block starts at ~line 345. Insert a new `if m.showSlashPopup {` block **before** the `if m.showPicker {` block:

```go
		if m.showSlashPopup {
			switch msg.Code {
			case tea.KeyEscape:
				m.closeSlashPopup()
				return m, nil
			case tea.KeyUp:
				if m.slashPopupIndex > 0 {
					m.slashPopupIndex--
				}
				return m, nil
			case tea.KeyDown, tea.KeyTab:
				if m.slashPopupIndex < len(m.slashPopupItems)-1 {
					m.slashPopupIndex++
				}
				return m, nil
			case tea.KeyEnter:
				if len(m.slashPopupItems) > 0 && m.slashPopupIndex < len(m.slashPopupItems) {
					selected := m.slashPopupItems[m.slashPopupIndex]
					m.closeSlashPopup()
					m.input.SetValue(selected.name + " ")
					if selected.name == "/model" {
						m.openModelPicker()
					}
				}
				return m, nil
			}
			// other keys fall through to textarea so user can keep typing
		}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/tui/... -run TestSlashPopupDown|TestSlashPopupUp|TestSlashPopupEsc|TestSlashPopupEnter -v
```

Expected: all PASS

- [ ] **Step 5: Run full test suite to check no regressions**

```bash
go test ./internal/tui/... -v 2>&1 | tail -20
```

Expected: all existing tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/slash_popup_test.go
git commit -m "feat: key handling for slash popup — up/down/tab/enter/esc"
```

---

## Task 5: Mouse click support

**Files:**
- Modify: `internal/tui/model.go` (`MouseClickMsg` case, ~line 317)

- [ ] **Step 1: Write failing test for mouse click**

Add to `internal/tui/slash_popup_test.go`:

```go
func TestSlashPopupMouseClickSelectsRow(t *testing.T) {
	// Popup rows start at Y = viewportHeight + 4
	// viewportHeight is 0 for a zero-size model, so rows at Y=4,5,6...
	m := model{
		input:           newTestTextarea(),
		showSlashPopup:  true,
		slashPopupIndex: 0,
		slashPopupItems: []slashSuggestion{
			{name: "/compact", desc: "Reduce context"},
			{name: "/connect", desc: "Show API keys"},
		},
	}
	// click row index 1 (Y = popupTopY + 1 + 1 = 0+3+1+1 = 5... but
	// viewport.Height() is 0 here, so popupTopY = 0+3 = 3, row0=Y4, row1=Y5)
	popupTopY := m.viewport.Height() + 3
	click := tea.MouseClickMsg{Button: tea.MouseLeft}
	// BubbleTea v2: set X/Y via the Mouse() embedded struct isn't directly possible
	// in tests — use the slashPopupRowForY helper instead
	_ = click
	_ = popupTopY

	// Test the helper function directly instead
	idx, ok := m.slashPopupRowForY(popupTopY + 1 + 1) // border top + row 1
	if !ok {
		t.Fatal("expected row hit")
	}
	if idx != 1 {
		t.Errorf("expected row index 1, got %d", idx)
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/tui/... -run TestSlashPopupMouseClickSelectsRow -v
```

Expected: `undefined: slashPopupRowForY`

- [ ] **Step 3: Add `slashPopupRowForY` helper and mouse click handling**

Add to `internal/tui/slash_popup.go`:

```go
// slashPopupRowForY returns the popup item index for an absolute Y coordinate,
// and whether the click landed on a valid row.
// Popup rows start at: viewport.Height() + 3 (header) + 1 (popup border top) = viewport.Height() + 4
func (m model) slashPopupRowForY(y int) (int, bool) {
	if !m.showSlashPopup || len(m.slashPopupItems) == 0 {
		return 0, false
	}
	popupItemsStartY := m.viewport.Height() + 4
	idx := y - popupItemsStartY
	if idx < 0 || idx >= len(m.slashPopupItems) {
		return 0, false
	}
	return idx, true
}
```

- [ ] **Step 4: Wire mouse click into `Update()` in `model.go`**

Find the existing `case tea.MouseClickMsg:` block (~line 317):

```go
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if path, ok := m.sidebarFileForClick(msg); ok {
				return m, openSidebarFileInEditor(path)
			}
		}
```

Extend it to handle slash popup clicks (add before the sidebar check):

```go
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if m.showSlashPopup {
				mouse := msg.Mouse()
				if idx, ok := m.slashPopupRowForY(mouse.Y); ok {
					selected := m.slashPopupItems[idx]
					m.closeSlashPopup()
					m.input.SetValue(selected.name + " ")
					if selected.name == "/model" {
						m.openModelPicker()
					}
					return m, nil
				}
			}
			if path, ok := m.sidebarFileForClick(msg); ok {
				return m, openSidebarFileInEditor(path)
			}
		}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/tui/... -run TestSlashPopupMouseClick -v
```

Expected: PASS

- [ ] **Step 6: Run full suite**

```bash
go test ./internal/tui/... 2>&1 | tail -5
```

Expected: ok

- [ ] **Step 7: Commit**

```bash
git add internal/tui/slash_popup.go internal/tui/model.go internal/tui/slash_popup_test.go
git commit -m "feat: mouse click to select slash popup item"
```

---

## Task 6: Layout — insert popup between transcript and input

**Files:**
- Modify: `internal/tui/model.go` (`renderContent` ~line 1375, `layout` ~line 1295)

- [ ] **Step 1: Write failing test for layout with popup**

Add to `internal/tui/slash_popup_test.go`:

```go
func TestSlashPopupAppearsInLayout(t *testing.T) {
	m := model{
		input:    newTestTextarea(),
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(10)),
		width:    80,
		height:   30,
		ready:    true,
		showSlashPopup: true,
		slashPopupItems: []slashSuggestion{
			{name: "/compact", desc: "Reduce context"},
		},
	}
	content := m.renderContent()
	if !strings.Contains(content, "/compact") {
		t.Error("expected /compact to appear in rendered content when popup is shown")
	}
}
```

Add `"charm.land/bubbles/v2/viewport"` to test imports.

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/tui/... -run TestSlashPopupAppearsInLayout -v
```

Expected: FAIL (`/compact` not in content)

- [ ] **Step 3: Insert popup into `renderContent()` layout in `model.go`**

Find the `renderContent()` function. The layout building section looks like:

```go
	transcript := borderStyle.Width(panelWidth - 2).Render(m.viewport.View())
	input := borderStyle.Width(panelWidth - 2).Render(m.input.View())
	left := lipgloss.JoinVertical(lipgloss.Left,
		header,
		transcript,
		input,
		status,
	)
```

Replace with:

```go
	transcript := borderStyle.Width(panelWidth - 2).Render(m.viewport.View())
	input := borderStyle.Width(panelWidth - 2).Render(m.input.View())

	leftParts := []string{header, transcript}
	if m.showSlashPopup {
		leftParts = append(leftParts, m.renderSlashPopup())
	}
	leftParts = append(leftParts, input, status)
	left := lipgloss.JoinVertical(lipgloss.Left, leftParts...)
```

- [ ] **Step 4: Run test**

```bash
go test ./internal/tui/... -run TestSlashPopupAppearsInLayout -v
```

Expected: PASS

- [ ] **Step 5: Run full suite**

```bash
go test ./internal/tui/... 2>&1 | tail -5
```

Expected: ok

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/slash_popup_test.go
git commit -m "feat: insert slash popup into TUI layout between transcript and input"
```

---

## Task 7: Smoke test the feature end-to-end

**Files:** none (manual verification)

- [ ] **Step 1: Build the binary**

```bash
go build -o ocode . && echo "build OK"
```

Expected: `build OK`

- [ ] **Step 2: Run the app and verify popup behavior**

```bash
./ocode
```

Manual checks:
1. Type `/` — popup should appear showing all commands with name + description
2. Type `/co` — popup should filter to `/compact`, `/connect`, `/commands`
3. Press `↓` — selection should move down (highlighted row changes)
4. Press `↑` — selection moves back up
5. Press `Tab` — same as `↓`
6. Press `Enter` — selected command inserted into input with trailing space; popup closes
7. Press `Esc` — popup closes, input unchanged
8. Type `/model` then `Enter` in popup — model picker opens
9. Type `/` then `Esc` then continue typing normally — no interference
10. Mouse click on a popup row — that command is selected

- [ ] **Step 3: Run full test suite one final time**

```bash
go test ./internal/tui/... -v 2>&1 | grep -E "PASS|FAIL|ok"
```

Expected: all PASS, final line `ok github.com/jamesmercstudio/ocode/internal/tui`

- [ ] **Step 4: Final commit if any fixes were needed**

```bash
git add -p
git commit -m "fix: slash popup smoke test corrections"
```
