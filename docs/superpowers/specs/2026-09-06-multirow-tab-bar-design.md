# Multi-Row Wrapping Tab Bar — Design

- **Date:** 2026-09-06
- **Status:** Draft — design approved in conversation (multi-row cap + "+N" fallback); spec review pending (todo t6)
- **Scope:** `web/src/components/Layout/UnifiedTabBar.tsx` + new helper files; web frontend only. No backend changes.
- **Related:** `docs/superpowers/specs/2026-08-29-unified-session-terminal-tabs-design.md` (predecessor single-row unified bar)

## Problem

With many open tabs (chat sessions, terminals, browser tabs), the unified tab bar is a single
horizontal row that overflows into a **hidden** horizontal scroll (`scrollbar-hide` +
wheel-to-horizontal handler). Consequences:

- The active tab can sit **off-screen and invisible** (no `scrollIntoView` anywhere in the bar).
- There is no visible affordance that more tabs exist beyond the clipped edge.
- Users with 15–30 tabs lose track of what is open.

The user explicitly asked for **multi-line tabs**. Options 1 (well-behaved single row + switcher)
and 2 (multi-row wrap) were presented; the user chose **Option 2**.

## Goals / Non-Goals

**Goals**

1. Tabs wrap into multiple visible rows instead of one hidden-scroll line.
2. Bounded height: the bar never grows past **2 rows** (code constant), keeping the
   chat/terminal viewport stable.
3. Tabs that do not fit the capped rows collapse into a **"+N" chip** that opens a popover
   listing the hidden tabs (icon, badges, close buttons).
4. The **active tab is always visible** (promotion rule below — it is never left in the
   overflow); the chip's highlight remains only as a same-frame fallback.
5. Remove the hidden horizontal scroll / wheel-hijack mechanism entirely.
6. Drag-to-reorder keeps working, including across wrapped rows, and order persistence is
   unchanged.

**Non-Goals**

- Keyboard tab switching (Ctrl/Cmd+Tab MRU, Ctrl/Cmd+1..9) and the all-tabs switcher dialog —
  these belonged to the rejected Option 1 and may be follow-ups later.
- Any change to `SessionSubTabs.tsx` (fixed Chat/Agents/Changes/Logs/Status row).
- Persisted user preferences for row count (the 2-row cap ships as a constant; a preference may
  be added later once usage validates demand).
- Backend/API changes, TUI changes.

## Behavior Spec

- The tab bar becomes: `[wrapping tab area (flex-1, min-w-0, flex-wrap)] [fixed trailing button
  cluster]`. The trailing cluster (💬+, 🌐+, ⌨️+, Processes, "All sessions") is pinned right and
  never wraps.
- Pills flow left-to-right, wrapping onto a second row when they exceed the available width.
- **Height contract:** the current bar is `h-9 pt-2` and cannot hold two wrapped rows — the
  fixed height goes away. Pills are normalized to `h-6` (24px), rows separated by `gap-y-1`
  (4px), container padded `py-1.5` (6px top/bottom). One-row bar = **36px** (identical to
  today's `h-9`); two-row bar = **64px**. Height is content-driven (`h-auto min-h-9`) and
  bounded by the row cap.
- Only pills that fit within the first **2 rows** render inside the wrap area. The remainder are
  represented by a trailing **"+N" chip** on the last visible row (N = hidden tab count).
- The "+N" chip:
  - Opens a **popover** (Radix `@radix-ui/react-popover`, shadcn wrapper) listing hidden tabs:
    kind emoji, title (truncated), running/pending/alert badges, and a close button per row.
    Popover rows are focusable and clickable to activate the tab; **no drag handles** in the
    popover.
  - Is **outside `SortableContext`** and never participates in persistence.
- **Overflow control semantics (one pattern, no mixed ARIA roles):** non-modal Radix Popover
  (content carries dialog semantics) containing a plain list of **button rows** — each row
  activates its tab and carries an inner ✕ button that closes it. No `listbox`/`menu` roles
  (they forbid the nested interactive elements the rows need). Keyboard: roving-tabindex
  arrow navigation across rows, Enter/Space activates, ✕ is its own tab stop; Escape and
  outside-click close (Radix built-in); focus returns to the chip on close.
- **Active-tab promotion rule:** the active tab is **always visible in the bar**. After each
  measurement, if the active tab lands in a hidden row it is promoted to the last slot of the
  final visible row, demoting the last previously-visible pill into the hidden set. The
  chip's active-highlight style remains only as a same-frame fallback (the instant before
  promotion lands).
- **Popover live updates:** rows list hidden tabs in canonical persisted `order` (deterministic
  ordering); closing a hidden tab removes its row; a hidden tab that becomes visible for any
  reason (resize, promotion, reorder, a visible tab closing) leaves the list; if the hidden
  set becomes empty the popover closes automatically. Tabs added while the popover is open
  follow the same rule — they either land visible (not in the list) or hidden (appended per
  `reconcileTabOrder`).
- **Row allocation (incl. narrow widths):** rows fill greedily left-to-right in persisted
  `order`. Pills yield before rows do — titles truncate progressively down to a minimum pill
  (icon + close ≈ 56px) before wrapping; the trailing cluster is `shrink-0` at its natural
  width; at extreme narrowness the bar degrades to one pill per row plus the chip.
- The wheel-to-horizontal `handleWheel` and `scrollbar-hide overflow-x-auto` container CSS are
  removed; in the capped state there is nothing to scroll.
- Edge behaviors:
  - **Narrow window:** rows get shorter → more tabs overflow into the chip; the trailing cluster
    keeps a guaranteed minimum width (pills yield first).
  - **1 tab / no tabs:** single row, no chip, no measurement churn.
  - **Initial render (probe → measure → paint):** the pre-measure "probe" frame renders all
    pills plus a reserved-width chip probe; a `useLayoutEffect` measures row buckets and sets
    the hidden set synchronously, so React flushes the final render (visible pills + real chip)
    **before the browser paints** — the probe frame is never visible. One measurement, no
    iteration, no flicker. Note: `TabPill` requires a DndContext ancestor (`useSortable`), so
    the probe frame still wraps pills in a **sensors-disabled** DndContext — no drag listeners
    are ever attached in the probe frame.

## Architecture

### Files

| File | Change |
|---|---|
| `web/src/components/Layout/UnifiedTabBar.tsx` | Restructure outer layout; swap DnD strategy; render overflow chip + popover; remove wheel/scroll CSS |
| `web/src/components/Layout/useWrappedOverflow.ts` | **New** — measurement hook (see below) |
| `web/src/components/Layout/useWrappedOverflow.test.ts` | **New** — vitest unit tests (hook logic, mocked geometry) |
| `web/src/components/Layout/UnifiedTabBar.test.tsx` | **New** — component tests (@testing-library/react present; conventions per `GitPanel.test.tsx`) for chip/popover interactions, geometry mocked at the hook boundary |
| `web/src/components/ui/popover.tsx` | **New** — shadcn popover primitive. **Dep verification:** no popover/dropdown primitive exists today (10 Radix deps present, none popover; `ContextMenu.tsx` / `TerminalPanel.tsx` hand-roll fixed-position popovers with no focus management) — adding `@radix-ui/react-popover` matches the existing dependency style and supplies the focus-restore/escape/outside-click behavior the chip requires. Recorded as an implementation decision; **if an accessible popover primitive appears in the codebase before implementation, reuse it instead of adding the dep** |
| `web/src/components/Layout/TabPill.tsx` | Unchanged |
| `web/src/components/Layout/SessionSubTabs.tsx` | Unchanged |
| `web/src/components/Layout/tabOrderPersistence.ts` | Unchanged |
| `web/package.json` | Add `@radix-ui/react-popover` |

### `useWrappedOverflow` hook

```
input:  orderedKeys: string[], containerRef, chipReservedPx (= 40), MAX_ROWS (= 2)
output: { visibleKeys: string[], hiddenKeys: string[] }
```

- Mechanism: **single-probe measurement** (advisor-confirmed measurement is required; CSS
  clamping cannot produce the hidden-set, and a fixed-point loop without a chip-width contract
  cannot settle). The probe frame renders ALL pills plus a chip probe of **reserved fixed
  width** `chipReservedPx` (40px — ≥ the widest possible "+99+" label incl. padding) in the real
  flex geometry. A `useLayoutEffect` groups pill elements by `offsetTop` into row buckets;
  pills in rows ≥ `MAX_ROWS` are hidden. The result is stored in state and the **final frame
  renders only `visibleKeys` + the real chip**, flushed before paint. Because the final render
  drops exactly the hidden pills (rows can only shrink, never grow past the cap, and the real
  chip width ≤ reserved width), the final wrap matches the probe wrap — one measurement,
  deterministic, no oscillation.
- Re-measure on: `orderedKeys` identity/length change, container width change
  (`ResizeObserver` on the wrap area; `window.resize` fallback where unsupported), and pill
  content changes (titles, process badges — geometry inputs).
- **Frozen during drag:** while a dnd drag is active (`activeDragId != null`) re-measures are
  suppressed; the visible/hidden split is frozen for the drag's lifetime and recomputed on
  drag end (a drop can change wrapping; the recalc lands after the drop settles).
- **DnD separation:** the probe frame mounts **no** `SortableContext`/DnD listeners; the final
  frame's `SortableContext` receives **`visibleKeys` only** — hidden pills are never draggable
  DOM. Persistence (`tabOrderPersistence.ts`) always consumes the **complete** `order`, never
  the visible subset.
- **Pure core, DOM shell:** the algorithm is split into exported pure helpers —
  `computeRowBuckets(tops: number[]): number[]` (element top → row index) and
  `partitionVisible(keys, buckets, maxRows, activeKey): { visibleKeys, hiddenKeys }` (row-cap
  filter + active-tab promotion) — so the row-layout algorithm is unit-tested without DOM. The
  hook itself is only measurement wiring (refs, `ResizeObserver`, drag freeze, state).
- jsdom cannot compute real flex layout: the hook's *logic* (bucketing, hidden-set derivation,
  drag freeze) is unit-tested with mocked geometry; real flex behavior is covered by the
  browser harness (Testing section).

### Drag & keyboard reorder

- Strategy: `horizontalListSortingStrategy` → **`rectSortingStrategy`** (wrap/grid-aware);
  keep `closestCenter` collision and existing `PointerSensor` + `KeyboardSensor` setup.
- Keyboard sensor: the current `sortableKeyboardCoordinates` is row-oriented and misbehaves
  across wrapped rows; replace with a **grid-aware coordinate getter**. Cross-row keyboard
  reorder (up/down between rows) is an explicit manual-checklist item, not assumed from
  pointer-drag correctness.
- Drag lifecycle: measurement is frozen while a drag is active (see hook); drops that change
  wrapping recompute the overflow split after drag end.
- Overflow pills are not in the drag surface (not rendered in the wrap area); the popover has no
  drag handles. Persistence continues to receive only tab IDs — `tabOrderPersistence.ts`
  untouched.

## Error Handling

- Measurement failure / ref not mounted → fall back to "no overflow" (render all pills wrapped;
  worst case the bar grows past 2 rows for one frame) — never crash the bar.
- ResizeObserver unsupported (very old browsers) → listen to `window.resize` only.
- Popover open during a tab-list change (e.g. hidden tab closed from elsewhere) → hidden-set
  recomputes; popover list re-renders; a now-visible tab activates normally.

## Testing

- **Unit — hook (vitest, jsdom, mocked geometry):** bucketing / hidden-set derivation (0/1/
  exact/multi-row overflow), chip reservation math, re-measure triggers, drag freeze (no
  re-measure while `activeDragId` set), empty/single-tab cases.
- **Pure algorithm (vitest, no DOM):** `computeRowBuckets` + `partitionVisible` — row-cap
  filter, active-tab promotion (active never hidden), greedy allocation, empty/single-tab
  cases.
- **Component (vitest + @testing-library/react, following the existing `GitPanel.test.tsx`
  conventions — viability evidenced by that file, not inferred from package.json):**
  geometry mocked at the hook boundary — chip renders correct N, popover lists hidden tabs with
  badges + close, activate-from-popover switches tab **and promotes it visible**, popover
  keyboard navigation (arrows / Enter / Escape / outside-click / focus return to chip),
  active-in-overflow chip-highlight fallback, popover open during a hidden-set change
  re-renders and auto-closes on empty, chip never enters persistence inputs.
- **Real-browser harness (deterministic):** dev-only `?tabStress=N` query param seeds N fake
  tabs into the bar; the manual checklist covers what jsdom cannot validate:
  - Wrap + cap at 2 rows with 10/25/50 tabs; chip count correct; one-row height = 36px,
    two-row = 64px.
  - Cross-row pointer drag AND cross-row keyboard reorder (long/short title mix, unequal
    widths).
  - Resize while the popover is open; title/process-badge change reflows rows; narrow window
    where the trailing cluster keeps its min width.
- **Build:** `tsc && vite build` clean; `npx vitest run` green.

## Rollout

Single increment (all pieces land together — the hook, chip, popover, and strategy swap are
co-dependent). No feature flag; behavior change is contained to the tab bar chrome.
**Dependency cost:** the sole new dependency is `@radix-ui/react-popover` (small runtime, no
CSS import; 10 Radix packages already in use). Everything else is existing code or new local
files.

## Open Questions

None — 2-row cap + "+N" fallback was confirmed by the user ("yes") after being presented as the
alternative to uncapped all-rows-visible.
