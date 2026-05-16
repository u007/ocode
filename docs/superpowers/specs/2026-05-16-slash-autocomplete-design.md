# Slash Command Autocomplete Popup

**Date:** 2026-05-16  
**Status:** Approved

## Overview

When the user types `/` in the chat input, a popup list of matching slash commands appears automatically, filtered in real time by what they've typed. Each entry shows the command name and its description. The user can navigate with arrow keys and select with Enter.

## State

Add to the `model` struct in `internal/tui/model.go`:

- `showSlashPopup bool` — whether the popup is visible
- `slashPopupIndex int` — currently highlighted row (0-based)
- `slashPopupItems []slashSuggestion` — filtered list of items to display

Add a new type in `internal/tui/picker.go` (or a new `slash_popup.go`):

```
type slashSuggestion struct {
    name string  // e.g. "/compact"
    desc string  // e.g. "Reduce context size by removing tool history"
}
```

## Trigger Logic

After every key press updates the textarea (`m.input.Update(msg)`), inspect `m.input.Value()`:

- If value starts with `/` and contains no space character → compute `slashPopupItems` by filtering all `commandSpecs` + `loadedCustomCommands` where `name` has the typed prefix; show popup (`showSlashPopup = true`)
- Otherwise → `showSlashPopup = false`

Filtering is case-insensitive prefix match on `spec.name`. Each result maps to `slashSuggestion{name: spec.name, desc: spec.help}`. Custom commands use their `Name` and `Description` fields.

## Key Handling

When `showSlashPopup` is true, intercept keys **before** passing to textarea:

| Key | Action |
|-----|--------|
| `↑` | Decrement `slashPopupIndex` (clamp to 0) |
| `↓` | Increment `slashPopupIndex` (clamp to len-1) |
| `Enter` | Set `m.input.SetValue(selected.name)`, close popup, then run normal enter handling |
| `Esc` | Close popup, leave input unchanged |
| All others | Pass through to textarea; popup re-filters on next render cycle |

"Close popup" means: `showSlashPopup = false`, `slashPopupIndex = 0`, `slashPopupItems = nil`.

## Layout

Insert the popup box between transcript and input when `showSlashPopup` is true.

Current layout (in `renderContent`):
```
header
transcript box
input box
status bar
```

New layout when popup is visible:
```
header
transcript box
slash popup box
input box
status bar
```

The popup box uses the same `borderStyle` as other panels, same `panelWidth`.

## Popup Rendering

Located in a new helper `renderSlashPopup() string` on `model`.

- Max 8 visible rows; scrolls to keep selected item in view
- Each row: `  /commandname   description text` 
- Selected row: blue background (`#7AA2F7`), dark foreground (`#1A1B26`) — matches existing picker style
- Command name: bold; description: dimmed (color `#565F89`)
- Hint line at bottom: `↑/↓ select · Enter confirm · Esc cancel`
- If no matches: show `(no matching commands)`

Example:
```
┌─────────────────────────────────────────────┐
│ /compact   Reduce context size...           │
│ /connect   Show/Set provider API keys       │  ← selected (blue bg)
│ /export    Save chat as Markdown            │
│                                             │
│ ↑/↓ select · Enter confirm · Esc cancel    │
└─────────────────────────────────────────────┘
```

## Files to Change

- `internal/tui/model.go` — add state fields; trigger logic after textarea update; key intercept when popup open; insert popup into layout
- `internal/tui/picker.go` — add `slashSuggestion` type, `renderSlashPopup()`, `closeSlashPopup()` helpers
- `internal/tui/commands.go` — add `slashSuggestions(m *model, prefix string) []slashSuggestion` replacing/wrapping `autocompleteSlashInput`

## Out of Scope

- Mouse click to select (BubbleTea mouse events are a separate concern)
- Fuzzy matching (prefix match is sufficient)
- Argument autocomplete (e.g. model names after `/model `) — existing Tab behavior handles this
