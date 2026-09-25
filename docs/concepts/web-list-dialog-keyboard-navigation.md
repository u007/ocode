---
type: Concept
title: Web List-Dialog Keyboard Navigation
description: 'Shipped shared list-navigation helper/hook and its six web dialog integrations: keys, focus contract, per-surface preservation, deferred surfaces, tests.'
tags:
  - web
  - keyboard
  - accessibility
  - dialogs
  - navigation
timestamp: 2026-09-25T12:16:11Z
---
# Web list-dialog keyboard navigation

**Status:** Shipped (2026-09-25). Scope: web/desktop React UI only — the TUI has
its own list navigation and was not touched.
**Design source:** [List Dialog Keyboard Navigation — Design Spec](superpowers/specs/2026-09-25-list-dialog-keyboard-navigation-design.md).

## 1. What shipped

A shared, DOM-free movement helper plus a thin real-focus React hook, integrated
into six hand-rolled list popups. Handlers are **local** (dialog root /
popover container `onKeyDown`); **no global `window`/`document` key listener was
added**, and the hook itself early-returns unless the event target is a marked
primary row or the designated search input.

### Shared pieces

- `web/src/lib/listNavigation.ts` — pure `resolveListNavigation(state) → action`.
  No React, no DOM. Inputs: `current` index, `count`, `direction`
  (`next`/`previous`/`first`/`last`), optional `hasMore`, `hasInput`, `disabled`.
  Outputs: `{type:"focus", index}` | `{type:"load-more", nextIndex}` |
  `{type:"return-to-input"}` | `{type:"none"}`. All movement rules (clamp,
  no-wrap, empty/disabled no-op, search boundary, load-more at the last row) live
  here and are unit-tested without a browser.
- `web/src/hooks/useListNavigation.ts` — `useListNavigation(options)` owns DOM
  focus and row identity. Options: `itemIds` (stable caller-owned identities in
  rendered order), `onActivate`, optional `onToggle` + `multiple`, `inputRef`
  (search input bridge), `returnFocusRef`/`onReturnFocus` (popover trigger),
  `resetKey` (reset on dialog open), `initialActiveId` (focus current item on
  open), `hasMore`/`onReachEnd` (lazy-list handoff). Returns:
  - `getItemProps(index)` → spread onto each primary row: `ref`, roving
    `tabIndex` (0 on the active row, or the first row when nothing is active;
    −1 otherwise), `data-list-nav-row`, `data-list-nav-id`,
    `data-list-nav-active`.
  - `isActive(index)` → drives the visible active styling.
  - `onKeyDown(event)` → the local handler (see §2).

### Row contract

- **Primary rows must carry `data-list-nav-row`.** The hook resolves rows
  exclusively through that marker (or the designated input) — never by guessing
  row structure. Rows are registered via their `data-list-nav-id`.
- **Real focus, not `aria-activedescendant`.** `focusRow` does
  `row.focus({ preventScroll: true })` followed by
  `row.scrollIntoView({ block: "nearest" })`, so the focused row scrolls within
  the dialog's own scroll container without moving the page behind it.
- **Visible active styling.** `getItemProps` sets `data-list-nav-active` and
  each surface renders it as an inset ring (e.g. `ring-2 ring-inset ring-blue-400`
  in ModelDialog/QuestionDialog, `ring-accent-foreground/60` in DirectoryBrowser),
  plus the native `:focus-visible` ring.
- **Native button roles were retained.** The shipped code adds **no
  `role="listbox"` / `role="option"`** ARIA to any of the six surfaces — rows are
  plain `<button>` elements (QuestionDialog options add `aria-pressed`). A
  listbox/option retrofit would conflict with existing nested controls (the
  ModelDialog favorite star sits *outside* the row, option rows carry icon +
  description children, SessionDialog rows nest close/metadata buttons). The
  contract is **real focus + visible active styling**, not an ARIA shape.

### Key semantics (shared by all six surfaces)

| Key | Behavior |
| --- | --- |
| `ArrowDown` | Next primary row; from the search input, enters the **first** row; on the last row: `load-more` if the caller reports `hasMore`, otherwise clamp |
| `ArrowUp` | Previous row; on the **first** row: back to the search input / trigger when one exists (`return-to-input`), otherwise clamp |
| `Home` / `End` | First / last visible primary row (section headers are not rows, so they are skipped) |
| `Enter` | Activate the focused row — same as clicking it |
| `Space` | Single-select: activate (same as Enter). Explicit multi-select: toggle the focused row. On the search input Space still types a space — only marked rows are handled |

- **Clamp, never wrap.** Navigation never wraps to the opposite end; the only
  boundary extension is the sanctioned lazy-list `load-more` (still never wraps).
- **Disabled/empty lists are no-ops** (`resolveListNavigation` returns `none`).
- **Handled keys `preventDefault()` + `stopPropagation()`.**
- **Nested actions are never hijacked.** Events whose target is not a marked
  row (or the designated input) are ignored — the ModelDialog favorite star, the
  dialog close **X**, per-row metadata buttons and the custom-text textarea keep
  their own keys. The hook checks `target.closest("[data-list-nav-row]")` and
  equality with `inputRef`; rows without the marker are invisible to it.
- **Identity reconciliation.** State is keyed by row id, not index. On
  `resetKey` change (dialog open / purpose / project change) the active row
  resets to `initialActiveId` or none. When the visible row set changes, a still
  existing id is kept; an unmounted id reconciles by clamping the previous index
  into the new range (or to none). A `pendingIndex` set by `load-more` is applied
  only **after the new rows render** (focus never moves before they exist). The
  hook never leaves focus on an unmounted row, and it won't steal the search
  input's focus while a filter change is being typed.

## 2. Integrated surfaces (closed list of six)

### ModelDialog (`web/src/components/Layout/ModelDialog.tsx`)

- Row ids are **composite `section:name`** (`recents:<name>`, `favorites:<name>`,
  `<provider>:<name>`) built from `modelNavRows` in rendered order — the
  same model in Recently Used, Favorites and its provider group counts as
  distinct navigation targets, so reconcile/activation can't land on the wrong
  row.
- **Single-select.** `onActivate` = `handleSelect(row.model)` — commits and
  closes exactly like a click; `Space` behaves as `Enter`. No multi-select was
  added.
- Preserved unchanged: **cached-first loading** (render from snapshot, live
  refresh never blanks/reorders — see
  [web-model-picker-cached-not-live](gotchas/web-model-picker-cached-not-live.md)),
  configured/available **filtering**, the provider **row cap**
  (`capProviderGroups`, incl. the 500-row render trim), **section grouping and
  order** (`partitionModelSections`), **favorites** star behavior, and search.
  Navigation operates only on rendered rows; `Home`/`End` skip section headers.
- The **favorite star is a sibling button without `data-list-nav-row`** → arrow
  keys on a row never toggle it, and keys on the star are never consumed
  (pinned by tests).
- `resetKey = open|purpose|showAllProviders|sessionId|host`.

### SessionDialog (`web/src/components/Layout/SessionDialog.tsx`)

- **`visibleCount` is a synchronous client-side slice** of the already-loaded
  session list — there is no server page behind the 50-row window
  (`SESSION_DIALOG_PAGE_SIZE`). Navigation bounds use
  **`visibleCountClamped = min(visibleCount, filteredSessions.length)`**, never
  the raw counter; a background revalidation clamps the window instead of
  resetting it (see
  [web-all-sessions-dialog-slow](gotchas/web-all-sessions-dialog-slow.md)).
- **ArrowDown at the final rendered row** (while `hasMore`) calls the same
  `loadMore()` the `IntersectionObserver` drives; the hook parks a pending index
  and focuses the **first newly rendered row after the next render**. With no
  more items it clamps.
- The explicit **"Load more (N remaining)"** button is the jsdom test path (no
  `IntersectionObserver` in jsdom).
- `onActivate` opens the session tab; search-input bridge via the search field.

### DirectoryBrowser (`web/src/components/Layout/DirectoryBrowser.tsx`)

- **`Enter` on a focused folder row confirms that folder** — `onActivate` runs
  `setSelectedPath` → `onSelect(path)` → close (row `onClick` only selects;
  the hook's `preventDefault` keeps the native button click from double-firing).
- **Double-click still navigates into the folder** (`onDoubleClick →
  navigateInto`) — the two gestures stay distinct.
- The pre-existing root Enter handler lives on the **selected-path `Input`**
  (its target is the input, so row-originated Enter never reaches it), and the
  hook stops propagation for row Enter — one keypress can't fire both.
- Filter input bridge: ArrowDown from "Filter by keywords..." enters the list.

### QuestionDialog (`web/src/components/Chat/QuestionDialog.tsx`)

- `QuestionOptionList` is a child component **per question index (`qi`)** — each
  rendered question owns an independent `useListNavigation` scope keyed
  `question:<qi>:option:<i>:<label>`; arrows never cross question scopes
  (switching between questions is via the existing Radix `Tabs` triggers).
- **Explicit multi-select:** for `q.multiple`, `Enter` **and** `Space` toggle the
  focused option (never submit); for single-select they select it (deselecting
  single-select peers). The **custom-text option is an ordinary navigable row**
  and the textarea below it is a nested control the hook ignores.
- The **footer Submit / Don't answer row is untouched** — row activation never
  submits; only the explicit Submit control does.
- No search input → `ArrowUp` on the first row is a clamp.

### ReasoningLevelSelector (`web/src/components/Layout/ReasoningLevelSelector.tsx`)

Non-Radix popover — **own focus contract, deliberately distinct from the Radix
DialogContent initial-focus policy**:

- On open, `initialActiveId: currentLevel` focuses the **current (selected)
  item** (not the first).
- `Escape` closes and returns focus to the trigger.
- `Tab` closes the popover (no tab cycling inside).
- `ArrowUp` on the first row runs `return-to-input`: focus the trigger
  (`returnFocusRef`) and `onReturnFocus` closes the popover.
- Activating a level selects, closes, and refocuses the trigger.

### ProfileSwitcher (`web/src/components/ProfileSwitcher.tsx`)

Same phase-2 popover contract: `initialActiveId` = active profile (or
`"default"`), `Escape` → close + trigger focus, `Tab` → close, ArrowUp at the
first row → trigger + close, activate → PUT active profile → close + trigger
focus.

## 3. Explicitly out of scope (unchanged)

- **cmdk surfaces:** `FilePicker`, `CommandPalette` — cmdk's own navigation.
- **Native Radix `Select`** popovers.
- **Radix `ContextMenu` / `DropdownMenu`** keyboard handling (and role=menu
  popovers such as `BlockCopyControl`, the terminal panel menu).
- **Side-panel / table lists** (FileTree, GitPanel, side-panel session lists)
  and **non-list forms** (settings, permissions).
- The **TUI**.

## 4. Deferred surfaces (named, left unchanged)

The six-surface list above is closed. Observed hand-rolled list popups **left
out of scope** by this change (recorded by name per the design spec's closed-list
rule; neither was broken, both simply keep their existing behavior):

- **`SlashCommandMenu`** (`web/src/components/Chat/SlashCommandMenu.tsx`) — the
  composer's slash-command autocomplete. Arrow keys already work, but as an
  index highlight owned by `ChatInput.handleKeyDown` (no real row focus); not
  migrated to `useListNavigation`.
- **`UnifiedTabBar` session-tab dropdown** (`web/src/components/Layout/UnifiedTabBar.tsx`,
  the <1024px Radix-Popover listbox) — items handle `Enter`/`Space` but there is
  no arrow/Home/End movement; not migrated.

No deferred surface was recorded in `TODO.md` by the implementation plan — the
two names above were catalogued by this documentation pass from the shipped
tree. If either is later migrated, delete it from this section.

## 5. Tests and validation

Test files (all under `web/`):

| File | Coverage |
| --- | --- |
| `src/lib/listNavigation.test.ts` (7) | empty/disabled no-op, first/last, boundary entry, mid-list movement, clamp-not-wrap, gated load-more, return-to-search |
| `src/hooks/useListNavigation.test.tsx` (9) | search bridge + top return, Home/End, boundary no-ops, activate vs multi-toggle, nested-action/space-in-search exclusion, handled-key consumption + reconcile focus, no focus-steal on filter, composite-identity distinctness, load-more focus-after-render |
| `src/components/Layout/ModelDialog.test.tsx` — `ModelDialog keyboard navigation` | search→row→Enter select; favorite star stays outside row navigation (plus the pre-existing cached-loading / row-cap / section suites untouched) |
| `src/components/Layout/SessionDialog.test.tsx` — `SessionDialog keyboard navigation` | search→row→Enter open; ArrowDown at page boundary loads the next slice and focuses its first new row (via the explicit Load more fallback) |
| `src/components/Layout/DirectoryBrowser.test.tsx` (2, new file) | filter→row, Enter confirms folder (`onSelect` + close), double-click navigation preserved |
| `src/components/Chat/QuestionDialog.test.tsx` — `QuestionDialog keyboard navigation` | per-`qi` scope, multi-select Space toggle, custom-text row reachable, no submit on row activation |
| `src/components/Layout/ReasoningLevelSelector.test.tsx` | keyboard navigation + Escape focus restoration + Tab close |
| `src/components/ProfileSwitcher.test.tsx` (2, new file) | focus active profile, navigate, Enter select; Escape/Tab close with trigger-focus restore |

Environment notes: `web/src/test/setup.ts` installs a shared
`HTMLElement.prototype.scrollIntoView` stub (jsdom does not implement it), and
focus tests assert `document.activeElement` directly; SessionDialog tests use
the explicit Load-more control because jsdom has no `IntersectionObserver`.

Validation commands (run 2026-09-25, all green):

```bash
cd web
npx vitest run src/lib/listNavigation.test.ts src/hooks/useListNavigation.test.tsx \
  src/components/Layout/ModelDialog.test.tsx src/components/Layout/SessionDialog.test.tsx \
  src/components/Layout/DirectoryBrowser.test.tsx src/components/Chat/QuestionDialog.test.tsx \
  src/components/Layout/ReasoningLevelSelector.test.tsx src/components/ProfileSwitcher.test.tsx
# → 8 files, 78 tests passed
npx tsgo --noEmit    # clean
npx vite build       # clean
```

## 6. Related docs (preserved, no conflicts)

- [web-keyboard-shortcuts](web-keyboard-shortcuts.md) — the global /
  chat-input shortcut inventory (the arrows documented there are composer
  history, a different surface); unchanged.
- [web-model-picker-cached-not-live](gotchas/web-model-picker-cached-not-live.md)
  — ModelDialog cached-first contract; unchanged and still authoritative.
- [web-all-sessions-dialog-slow](gotchas/web-all-sessions-dialog-slow.md) —
  SessionDialog windowing/performance contract; unchanged.
- Design spec:
  [2026-09-25-list-dialog-keyboard-navigation-design.md](superpowers/specs/2026-09-25-list-dialog-keyboard-navigation-design.md).
