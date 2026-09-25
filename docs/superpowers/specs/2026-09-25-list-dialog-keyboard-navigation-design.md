---
type: Design
title: List Dialog Keyboard Navigation — Design Spec
description: 'Approved design spec for shared Up/Down keyboard navigation across web list-based popup dialogs (ModelDialog and friends): shared reducer/hook, per-dialog contracts, a11y, testing, docs impact.'
tags:
  - design-spec
  - web
  - keyboard
  - accessibility
  - dialogs
timestamp: 2026-09-25T03:33:59Z
---
# List Dialog Keyboard Navigation — Design Spec

**Date:** 2026-09-25
**Status:** Approved for implementation
**Scope:** web/desktop React UI (not the TUI)

## 1. Problem

List-based popup dialogs in the web UI each hand-roll (or omit) Up/Down keyboard
navigation. ModelDialog is the worst offender: it is the primary model picker and
offers no consistent arrow-key movement among rows, so keyboard-only users must Tab
through every row (and through search input, section headers, and nested controls)
to reach an entry. Other list selectors repeat variations of the same missing
behavior, and each new dialog re-implements the pattern.

There is no existing general list-dialog keyboard-navigation doc in the knowledge
bundle; the closest references are the shortcuts doc, the model-picker gotcha, the
all-sessions dialog gotcha, and the web skill file (see §10).

## 2. Goal

Consistent **ArrowDown / ArrowUp / Home / End / Enter / Space** navigation in
list-based popup dialogs, achieved with a **shared helper/hook plus small
per-dialog integrations**.

### Non-goals / rejected alternatives

- **Rewriting dialogs onto cmdk** (the CommandPalette/FilePicker stack): too
  invasive, changes rendering and virtualization behavior, and risks the cached
  model-picker and 50-row session-windowing contracts (§5).
- **Per-dialog duplicate handlers**: exactly the drift we are fixing.
- **Global `useKeyboard` listeners**: dialogs must handle keys locally on their own
  elements so nested inputs, portals, and multiple stacked dialogs cannot collide.
- **TUI changes**: the TUI has its own list navigation; untouched here.

## 3. In scope

Hand-rolled list selectors in `web/src`:

| Dialog / component | Notes |
| --- | --- |
| `ModelDialog` | Primary target; most complex (search, sections, favorites, row cap) |
| `SessionDialog` | 50-row windowing + IntersectionObserver load-more |
| `DirectoryBrowser` | Directory listing rows |
| `QuestionDialog` | Existing `q.multiple` multi-select + explicit Submit |
| `ReasoningLevelSelector` | Selector popover |
| `ProfileSwitcher` | Selector popover |
| Other hand-rolled list selectors | Same hook, discovered during implementation |

### Out of scope (explicitly excluded)

- **cmdk surfaces**: `FilePicker`, `CommandPalette` (already have cmdk's own
  navigation).
- **Native Radix `Select`** popovers.
- **Context menus** (Radix `ContextMenu`/`DropdownMenu` keyboard handling).
- **Side-panel lists and table lists** (FileTree, GitPanel changes table, session
  lists in side panels) — these are not popup dialog listboxes.
- **Non-list forms** (settings forms, permission forms).

## 4. Shared behavior contract

This is the single behavioral spec; the shared hook implements it once.

1. **Real focus movement.** `ArrowDown`/`ArrowUp` move *actual DOM focus* between
   visible **primary rows**. No `aria-activedescendant`; focused rows are real
   `tabIndex={0}` (or naturally focusable) elements.
2. **Home/End** jump to the first/last visible primary row.
3. **Enter activates** the focused row (same as clicking it).
4. **Space** activates a single-select row; in an *explicit* multi-select list it
   toggles the focused row's checked state. In single-select dialogs Space must not
   be hijacked for anything else on a row (Space on the search input still inserts
   a space).
5. **Search-input bridge:** `ArrowDown` pressed while focus is in the search input
   enters the **first** visible row. `ArrowUp` pressed on the **first** row returns
   focus to the search input (when the dialog has one).
6. **Clamp at ends — no wrap.** ArrowDown on the last row and ArrowUp on the first
   row (with no search input above) do nothing.
7. **Reset/reconcile.** The navigation index resets on dialog open and *reconciles*
   when the filter text or the visible row set changes: if the previously focused
   row still exists (same identity), keep it; otherwise clamp to the nearest valid
   index (or search input). Never leave focus on an unmounted row.
8. **Scroll into view.** The focused row is scrolled into view within the dialog's
   scroll container (`scrollIntoView({ block: "nearest" })` or equivalent), without
   scrolling the page behind the dialog.
9. **Disabled/empty lists do nothing.** No rows visible, all rows disabled, or the
   list disabled → keys are ignored (except the dialog's own close keys).
10. **Nested actions are not hijacked.** Per-row nested controls (ModelDialog's
    favorite star, the dialog close **X**, per-row action buttons) remain
    independently reachable via Tab/click and receive keys when focused. The
    navigation hook only handles keys when focus is on a primary row or the search
    input, and ignores events whose target is a nested interactive element.
11. **No multi-select added to ModelDialog.** ModelDialog stays single-select.

### Scoping rule

Handlers are attached **locally** (dialog root `onKeyDown`, or the hook's
`props` spread on the list/search elements). No `window`/`document` key listeners.

## 5. Per-dialog preservation requirements

These are existing, tested contracts the implementation must not break.

### 5.1 ModelDialog

Preserve all of:

- **Cached-first loading** — render from the model snapshot/cache immediately;
  live refresh must not blank or reorder the list mid-interaction
  (see `docs/gotchas/web-model-picker-cached-not-live.md`).
- **Configured filtering** — configured/available model filtering unchanged.
- **Row cap** — the maximum rendered row count stays as-is; navigation operates
  only on *rendered* rows (the cap bounds what Up/Down can reach).
- **Section ordering** — section grouping and order unchanged; Home/End target the
  first/last row of the first/last *visible* section, skipping section headers.
- **Search** — search filtering and the search-input bridge (§4.5).
- **Favorites** — favorite flagging/star behavior unchanged; the star remains an
  independent control (§4.10).
- **Single-select semantics** — selecting a model commits and closes exactly as a
  click does; Space behaves as Enter (§4.4); no multi-select (§4.11).

### 5.2 SessionDialog

Preserve:

- **50-row windowing** with `IntersectionObserver`-driven **load-more** — arrow
  navigation across the window boundary must trigger/await the same load-more path
  the observer uses, then continue to the newly appended rows (or clamp at the
  current end until the page lands — pick clamp-until-loaded to avoid scroll
  races; never jump past unloaded rows).
- **Revalidation-clamp behavior** — the existing clamp on revalidated session
  lists (stale/renumbered rows) stays authoritative; navigation indices clamp to
  the current list, never to stale server indices.

### 5.3 QuestionDialog

- Preserve **`q.multiple`**: multi-select questions keep Space-as-toggle on rows
  and Enter-on-row does *not* submit.
- Preserve the **explicit Submit** button: Enter/Space never auto-submit the
  dialog; submission happens only via the Submit control.

## 6. Accessibility

- Apply **`role="listbox"`** on the list container and **`role="option"`** on rows
  *"where appropriate"* — i.e., where the dialog's semantics genuinely are a list
  of selectable options. Do not retrofit roles onto directories/files rows in
  `DirectoryBrowser` if it currently presents as a navigable list of links/buttons;
  prefer consistent focus behavior over a forced ARIA shape.
- **Real focus**, never `aria-activedescendant`.
- **Visible focus** on the focused row (must survive the dialog's styling; add a
  focus-visible ring only if the row currently lacks one).
- **Preserve the existing DialogContent initial-focus policy**: first text-entry
  input → `[data-dialog-default-action]` → Radix first-tabbable default (see the
  dialog focus convention; new/changed primary buttons keep
  `data-dialog-default-action`). Arrow navigation starts *after* this initial
  focus, from the search input or first row.
- **Preserve Radix's focus trap** — do not set `onOpenAutoFocus`/
  `onKeyDownCapture` overrides that break it; navigation must work *within* the
  trap, and Escape handling is unchanged.

## 7. Implementation shape

- **One shared module** (e.g. `web/src/lib/listNavigation.ts` + a thin
  `useListNavigation` hook): a **pure navigation reducer/helper** (testable without
  DOM) plus a hook that wires key handling, focus, and scroll-into-view to a
  concrete list.
- The reducer's inputs: current index, target row identities, event key, list
  enabled/empty state, multi-select flag, whether a search input exists above;
  outputs: next index / "focus search input" / "activate row" / "toggle row" /
  "no-op".
- **Small integrations**: each in-scope dialog passes its visible primary rows,
  identity keys, activate handler, and (where applicable) multi-select flag and
  search-input ref. Integration diffs should be small and local.
- Discover any remaining hand-rolled list selectors during implementation and give
  them the same treatment (in scope), or explicitly record them as out of scope.

## 8. Testing

1. **Pure reducer/helper tests** — every clause of §4: movement, Home/End,
   activate/toggle, search bridge both directions, no-wrap clamping,
   reset/reconcile on open/filter/count change, empty/disabled no-op, nested-target
   ignored.
2. **Focused integration tests** for:
   - `ModelDialog` (search bridge, section skipping, favorites star still
     reachable, single-select Enter/Space, cached-first list intact, row cap).
   - `SessionDialog` (window boundary behavior, revalidation clamp).
   - `DirectoryBrowser`.
   - `QuestionDialog` (`q.multiple` toggle + explicit Submit not triggered).
   - Selector popovers (`ReasoningLevelSelector`, `ProfileSwitcher`).
3. **Typecheck and build** (`tsgo --noEmit`, `vite build`) clean.
4. Mutation-verify the new tests where practical (temporarily revert the fix and
   confirm the test fails), per project habit.

## 9. Docs impact

- **A new concept doc will be added during implementation** (e.g.
  `docs/concepts/web-list-dialog-keyboard-navigation.md`) describing the shared
  contract and integration checklist.
- **Existing docs remain aligned** — verify, and adjust only if behavior wording
  conflicts:
  - `docs/concepts/web-keyboard-shortcuts.md`
  - `docs/gotchas/web-model-picker-cached-not-live.md`
  - `docs/gotchas/web-all-sessions-dialog-slow.md`
  - `skills/ocode-web/SKILL.md` (file map / gotchas entry for the new helper)

## 10. References

- `docs/concepts/web-keyboard-shortcuts.md` — existing shortcut inventory.
- `docs/gotchas/web-model-picker-cached-not-live.md` — ModelDialog cached-first
  contract.
- `docs/gotchas/web-all-sessions-dialog-slow.md` — SessionDialog windowing/
  performance contract.
- `skills/ocode-web/SKILL.md` — web file map and recurring gotchas.

**Note:** there is **no existing general list-dialog keyboard-navigation doc** in
the bundle; this spec is the source of truth until the §9 concept doc lands.

## 11. Self-review notes

- **Placeholders:** none — no TODO/TBD/`<...>` markers remain; component names
  (`web/src/lib/listNavigation.ts`, concept-doc filename) are illustrative
  proposals, not unresolved decisions, and are called out as such.
- **Contradictions:** none found. §4.4 (Space activates in single-select) and
  §4.10 (nested controls keep their own keys) are compatible because §4.10 scopes
  the hook to primary rows/search input and excludes nested targets; §4.5's
  search-input bridge is compatible with Space-on-search inserting a space (Space
  handling is row-scoped only).
- **Scope creep:** checked — exclusions (§3) are explicit and match the approved
  scope; "other hand-rolled list selectors" is bounded by the same list-popup
  definition, with the option to *record* deferrals rather than silently expand.
- **Ambiguity resolved:** SessionDialog boundary behavior pinned to
  "clamp until the page lands" (the only ambiguous clause); DirectoryBrowser ARIA
  explicitly allows skipping `listbox/option` when the existing semantics differ.
- **Remaining unknown:** the exact inventory of "other hand-rolled list selectors"
  is intentionally deferred to implementation discovery (§7), since broad
  exploration was out of scope for this spec.
