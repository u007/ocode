---
type: Design
title: List Dialog Keyboard Navigation — Design Spec
description: 'Approved design spec for shared Up/Down keyboard navigation across web list-based popup dialogs (ModelDialog, SessionDialog, DirectoryBrowser, QuestionDialog, and phase-2 selector popovers): shared reducer/hook, per-dialog contracts, a11y, testing, docs impact.'
tags:
  - design-spec
  - web
  - keyboard
  - accessibility
  - dialogs
timestamp: 2026-09-25T10:51:23Z
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

Hand-rolled list selectors in `web/src`, sequenced in two phases:

| Phase | Dialog / component | Notes |
| --- | --- | --- |
| 1 | `ModelDialog` | Primary target; most complex (search, sections, favorites, row cap) |
| 1 | `SessionDialog` | 50-row windowing + IntersectionObserver load-more |
| 1 | `DirectoryBrowser` | Directory listing rows |
| 1 | `QuestionDialog` | Existing `q.multiple` multi-select + explicit Submit |
| 2 | `ReasoningLevelSelector` | Non-Radix selector popover; separate focus contract (§5.5) |
| 2 | `ProfileSwitcher` | Non-Radix selector popover; separate focus contract (§5.5) |

Phase 1 (the four dialogs) ships first; the two phase-2 popovers are integrated
only after every phase-1 dialog lands, because their focus contract differs
(§5.5).

**This surface list is closed.** Any other hand-rolled list popup discovered
during implementation is a **deferred surface**: record it by name in the §9
concept doc's integration checklist as deferred, and leave it unchanged in this
change. There is deliberately no open-ended "other selectors" bucket.

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
   row (with no search input above) do nothing; navigation never wraps. The one
   sanctioned exception is a lazy list with more items to show: ArrowDown on the
   last *rendered* row may extend the list through its per-dialog load-more
   contract (§5.2) — and even then navigation never wraps to the opposite end.
7. **Reset/reconcile.** The navigation index resets on dialog open and *reconciles*
   when the filter text or the visible row set changes: if the previously focused
   row still exists (same identity), keep it; otherwise clamp to the nearest valid
   index (or search input). Never leave focus on an unmounted row. **Row identity
   is composite — `section:name`** (e.g. `Favorites:gpt-5`), never the bare name:
   ModelDialog can render the same model in Recently Used, Favorites, and its
   provider group, so a bare name would let reconcile or activation land on the
   wrong row.
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
    **Primary rows must carry the marker attribute `data-list-nav-row`**, and the
    hook resolves rows exclusively through that marker (or the search input) —
    never by guessing at row structure. Nested favorite/close actions must either
    `stopPropagation()` on their key events or be excluded by the hook's target
    check.
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
  The same model rendered in multiple sections (Recently Used, Favorites, provider
  group) counts as distinct navigation targets — composite `section:name`
  identity (§4.7).
- **Search** — search filtering and the search-input bridge (§4.5).
- **Favorites** — favorite flagging/star behavior unchanged; the star remains an
  independent control (§4.10).
- **Single-select semantics** — selecting a model commits and closes exactly as a
  click does; Space behaves as Enter (§4.4); no multi-select (§4.11).

### 5.2 SessionDialog

Preserve:

- **`visibleCount` is a synchronous client-side slice, not a server page.** The
  50-row window is a client-side slice of the already-loaded session list; there
  is no server page behind it. Navigation bounds come from
  **`visibleCountClamped`** (the slice length clamped to the actual list length),
  never from the raw `visibleCount`.
- **ArrowDown across the window boundary loads more.** ArrowDown on the last
  rendered row while more items exist calls the same `loadMore()` the
  `IntersectionObserver` drives, then focuses the **first newly rendered row after
  the next render** — focus must not move before those rows exist, and navigation
  never jumps past unloaded rows. When no more items exist, ArrowDown clamps
  (§4.6).
- **Tests use the explicit Load more fallback**, because jsdom provides no
  `IntersectionObserver` (§8).
- **Revalidation-clamp behavior** — the existing clamp on revalidated session
  lists (stale/renumbered rows) stays authoritative; navigation indices clamp to
  the current list, never to stale server indices.

### 5.3 QuestionDialog

- Preserve **`q.multiple`**: multi-select questions keep Space-as-toggle on rows
  and Enter-on-row does *not* submit.
- Preserve the **explicit Submit** button: Enter/Space never auto-submit the
  dialog; submission happens only via the Submit control.
- **One navigation scope and one active row per question index (`qi`).** Each
  rendered question owns its own navigation scope keyed by `qi`; arrows move the
  active row only within that question's options and never cross into another
  question's scope, and Enter/Space act only on the active row of the question
  that holds focus.
- **Arrows move among options; Enter/Space toggle the focused option** — toggling
  its checked state in `q.multiple`, selecting it (deselecting single-select peers)
  otherwise. The **custom-text option is an ordinary navigable row** and must be
  reachable by arrows like any other option.
- **Row activation never submits.** Enter/Space on an option only selects/toggles;
  submission happens only via the explicit Submit control.

### 5.4 DirectoryBrowser

- **Enter on a focused folder row selects/confirms that folder** (the row's
  single-activation action), **while double-click still navigates into it**. The
  two gestures stay distinct: Enter does not descend into the folder, and
  double-click does not merely select it.
- **The existing root Enter handler must ignore row-originated Enter** — any Enter
  whose target is (or sits inside) a primary row marked `data-list-nav-row` — so
  root-level and row-level Enter handling can never both fire for one keypress.

### 5.5 Non-Radix selector popovers (phase 2)

`ReasoningLevelSelector` and `ProfileSwitcher` are hand-rolled popovers, not Radix
`DialogContent`, and are integrated **after** the phase-1 dialog work. Arrow /
Home / End navigation follows §4; their focus contract is separate and must not
be conflated with the DialogContent initial-focus policy (§6):

- **On open:** move focus to the **current (selected) item** — not the first item
  and not DialogContent-style default focus.
- **Escape:** closes the popover and **returns focus to the trigger**.
- **Tab:** closes the popover (no tab-cycling within it).
- Their items are primary rows carrying `data-list-nav-row` like any other
  in-scope list (§4.10).

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
  focus, from the search input or first row. This policy covers Radix
  `DialogContent` surfaces only; the phase-2 non-Radix popovers follow their own
  contract — current item focused on open, Escape returns focus to the trigger,
  Tab closes (§5.5) — and must not be taught DialogContent-style initial focus.
- **Preserve Radix's focus trap** — do not set `onOpenAutoFocus`/
  `onKeyDownCapture` overrides that break it; navigation must work *within* the
  trap, and Escape handling is unchanged.

## 7. Implementation shape

- **One shared module** (e.g. `web/src/lib/listNavigation.ts` + a thin
  `useListNavigation` hook): a **pure navigation reducer/helper** (testable without
  DOM) plus a hook that wires key handling, focus, and scroll-into-view to a
  concrete list.
- The reducer's inputs: current index, **composite row identities
  (`section:name`)**, event key, list enabled/empty state, multi-select flag,
  whether a search input exists above; outputs: next index / "focus search input" /
  "activate row" / "toggle row" / "no-op".
- **Small integrations**: each in-scope dialog passes its visible primary rows
  (rendered with the `data-list-nav-row` marker), composite identity keys,
  activate handler, and (where applicable) multi-select flag and search-input
  ref. Integration diffs should be small and local.
- **Phase order:** the four phase-1 dialogs first, then the phase-2 popovers
  (§5.5). The surface list is closed (§3); any hand-rolled list popup discovered
  during implementation is recorded as a deferred surface (§3, §9) and left
  unchanged — never silently absorbed into this change.

## 8. Testing

1. **Pure reducer/helper tests** — every clause of §4: movement, Home/End,
   activate/toggle, search bridge both directions, no-wrap clamping (including the
   load-more extension at the last rendered row, §4.6/§5.2),
   reset/reconcile on open/filter/count change with composite `section:name`
   identity (the same name in two sections must not confuse reconcile),
   empty/disabled no-op, nested-target ignored.
2. **Focused integration tests** for the phase-1 dialogs:
   - `ModelDialog` (search bridge, section skipping, composite-identity reconcile
     when the same model appears in Recently Used, Favorites, and its provider
     group, favorites star still reachable with nested events excluded via
     `data-list-nav-row`/`stopPropagation`, single-select Enter/Space,
     cached-first list intact, row cap).
   - `SessionDialog` (ArrowDown on the last rendered row calls `loadMore()` and
     focuses the first newly rendered row after the next render; navigation uses
     `visibleCountClamped`; revalidation clamp). jsdom has no
     `IntersectionObserver`, so these tests drive the **explicit Load more
     fallback** control.
   - `DirectoryBrowser` (Enter on a folder row selects/confirms it; double-click
     still navigates into it; the root Enter handler ignores row-originated Enter).
   - `QuestionDialog` (one navigation scope and active row per question index
     `qi`; `q.multiple` toggle; custom-text option reachable by arrows; row
     activation never triggers the explicit Submit).

   Phase-2 tests, written with the popover integrations:
   - `ReasoningLevelSelector`, `ProfileSwitcher` (current item focused on open,
     Escape closes and returns focus to the trigger, Tab closes — §5.5).
3. **jsdom environment requirements** for every focus test: stub
   `Element.prototype.scrollIntoView` (jsdom does not implement it), assert
   `document.activeElement` after each focus move (never infer focus from the
   internal index alone), and handle the missing `IntersectionObserver` explicitly
   (SessionDialog tests use the Load-more fallback — no silently skipped tests).
4. **Typecheck and build** (`tsgo --noEmit`, `vite build`) clean.
5. Mutation-verify the new tests where practical (temporarily revert the fix and
   confirm the test fails), per project habit.

## 9. Docs impact

- **A new concept doc will be added during implementation** (e.g.
  `docs/concepts/web-list-dialog-keyboard-navigation.md`) describing the shared
  contract and integration checklist (including any deferred surfaces recorded
  per §3).
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
- **Contradictions:** none found. §4.6 (clamp, never wrap) and §5.2
  (ArrowDown at the last rendered row extends via `loadMore()`) are reconciled
  explicitly: §4.6 now names the lazy-list exception — extend, never wrap.
  §4.4 (Space activates in single-select) and §4.10 (nested controls keep their
  own keys) are compatible because §4.10 scopes the hook to primary rows/search
  input, requires the `data-list-nav-row` marker, and excludes nested targets;
  §4.5's search-input bridge is compatible with Space-on-search inserting a space
  (Space handling is row-scoped only). The superseded "clamp-until-loaded"
  alternative for SessionDialog was removed outright, not left as a parenthetical
  option.
- **Scope creep:** checked — §3 names exactly six surfaces across two phases,
  keeps every original exclusion, and imposes a closed-list rule: newly discovered
  hand-rolled popups become recorded *deferred surfaces* (§3, §9), never absorbed
  silently. Phases are sequencing, not scope expansion; the approved
  web/desktop scope (TUI still excluded), the shared-helper recommendation
  (§7), the key semantics (§4), and all citations (§9–§10) are unchanged.
- **Ambiguity resolved:** SessionDialog boundary pinned to "ArrowDown calls
  `loadMore()`, then focus the first newly rendered row after the next render"
  using `visibleCountClamped`, with the explicit Load-more fallback named for
  jsdom; DirectoryBrowser Enter = select/confirm the folder vs double-click =
  navigate into it, with the root Enter handler told to ignore row-originated
  Enter; QuestionDialog navigation scoped per `qi`, custom-text option reachable,
  row activation never submits; phase-2 popovers given their own focus contract,
  explicitly distinct from the Radix DialogContent initial-focus policy;
  DirectoryBrowser ARIA explicitly allows skipping `listbox/option` when the
  existing semantics differ.
- **Remaining unknown:** none blocking — the previous open item (the inventory of
  "other hand-rolled list selectors") is closed by the bounded named list plus the
  deferred-surface rule in §3, rather than left to in-implementation discovery.
