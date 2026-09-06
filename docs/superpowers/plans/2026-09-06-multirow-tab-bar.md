# Multi-Row Tab Bar — Implementation Plan

- **Date:** 2026-09-06
- **Status:** Plan ready for execution (spec approved: `7144c7d` + `af5c529`)
- **Spec:** `docs/superpowers/specs/2026-09-06-multirow-tab-bar-design.md`
- **Scope:** web frontend only; persistence module untouched. One commit per phase; tests land
  beside each phase (not batched at the end). Every phase — including Phase 1 — ends with
  `cd web && pnpm exec vitest run` + `pnpm exec tsc --noEmit` green. **Package manager is
  pnpm** (`packageManager: pnpm@10.27.0` in `web/package.json`, `pnpm-lock.yaml` present) —
  never npm/yarn commands.

## Conventions & invariants (all phases)

- Pills must be non-shrinking: `flex: 0 0 auto` on pill roots — flex-shrink would make the
  measured-width simulation in P2/P3 unsound (advisor).
- `SortableContext` receives **visible keys only**; persistence always receives the **complete
  `order`** (asserted by tests in P4c/P5).
- No synchronous partition reset on `onDragEnd` — keep the frozen split through dnd-kit
  cleanup, mark measurement dirty, re-probe in the next layout cycle (advisor).
- React Strict Mode safe: measurement is idempotent; no side effects outside `setState`.

## Phase 1 — Dependency + popover primitive

1. `cd web && pnpm add @radix-ui/react-popover` (package.json + pnpm-lock.yaml only).
2. New `web/src/components/ui/popover.tsx` — shadcn wrapper (Root/Trigger/Portal/Content),
   styled after the existing `ui/dialog.tsx` conventions (CSS-var classes, animate-in/out).

**Validate:** `pnpm exec tsc --noEmit`; `pnpm exec vitest run` (full suite untouched-green);
app boots.
**Commit:** `web: add radix popover primitive (tab bar overflow chip groundwork)`

## Phase 2 — Pure algorithm + unit tests (no DOM)

New `web/src/components/Layout/multirowLayout.ts` (pure exports only) + `multirowLayout.test.ts`.
Landed as `multirowLayout.ts` (the pure arithmetic) — the React measurement
hook lives separately in `useWrappedOverflow.ts` (Phase 3), which imports
`computeRowBuckets` / `partitionVisible` from `multirowLayout.ts`.

```ts
export function computeRowBuckets(tops: number[], tolerancePx?: number): number[]; // top → row
export interface PartitionInput {
  keys: string[];            // ordered (persisted order)
  buckets: number[];         // probe-measured row index per key
  widths: number[];          // probe-measured offsetWidth per key
  containerWidth: number;    // wrap-area inner width
  chipReservedPx: number;    // 40
  gapPx?: number;            // 2 (gap-x-0.5)
  epsilonPx?: number;        // 4
  maxRows: number;           // 2
  activeKey: string | null;
}
export function partitionVisible(input: PartitionInput): {
  visibleKeys: string[]; hiddenKeys: string[]; uncapped?: boolean;
};
```

- Baseline: visible = keys with `bucket < maxRows`.
- **Chip-fit step (runs before promotion):** the probe renders the chip too, so if the chip
  probe itself wrapped past `maxRows` (its bucket ≥ `maxRows`), demote tail pills of the last
  visible row (in reverse order, width arithmetic below) until the reserved chip fits on that
  row. Bounded by the row's pill count. **The chip never yields** — hidden tabs must stay
  accessible; if the row empties to chip-only, that is the accepted ultra-narrow state.
- Promotion (width-checked, advisor-corrected): if active is hidden, **greedy repack of the
  last visible row** — candidate sequence = that row's pills + active appended; fill
  left-to-right while `sum(widths) + gapPx·(n−1) + chipReservedPx ≤ containerWidth − epsilon`;
  pills beyond capacity (and everything after) demote to hidden. Active is reached last by
  construction, so it is visible unless it alone cannot fit with the chip even after every
  other pill demoted — the **absurd-narrow degradation** (container ≲ chip + one min pill):
  return an explicit `uncapped: true` flag and the component renders all pills uncapped (bar
  grows past `MAX_ROWS` per the spec's error-handling fallback) — hidden tabs and active stay
  accessible; never drop the chip.
- Bounded by the visible-pill count; terminates always.

**Tests:** bucket tolerance; baseline partitions (0/1/exact/multi-row overflow); **chip-induced
third-row prevention (chip probe wrapped → tail demoted until chip fits)**; promotion fit +
no-fit demote chains; wider-promoted-pill demotion; termination; active-already-visible
no-op; empty/single; ragged widths; chip reservation arithmetic; **ultra-narrow container
(chip-only row) and absurd-narrow (`uncapped: true`) cases**.
**Validate:** `pnpm exec vitest run src/components/Layout/useWrappedOverflow.test.ts`.
**Commit:** `web: wrapped-overflow pure partition algorithm (+tests)`

## Phase 3 — Hook wiring (probe → measure → final) + tests

`useWrappedOverflow({ orderedKeys, containerRef, contentSignature, activeDragId, ...opts })`:

- **Probe/final duality:** when partition is unset (initial) or dirty, the hook reports
  `probe: true` — the component renders ALL pills + a 40px chip probe inside a
  **sensors-disabled DndContext**; probe pills pass `disabled: true` to `useSortable` and get
  `pointer-events-none` (advisor: explicitly disable sortable listeners/refs/transforms).
- `useLayoutEffect`: measure `offsetTop` (buckets, flex ground truth) + `offsetWidth` per pill
  and chip probe; run `partitionVisible`; `setState` → final render flushed **before paint**.
- Re-measure triggers: `orderedKeys` identity/length, container width (`ResizeObserver` on the
  wrap area; `window.resize` fallback), `contentSignature` (titles/badges geometry input),
  **and `activeKey`** — promotion depends on it, so activating a hidden tab must mark the
  partition dirty and re-probe even when no geometry changed (advisor).
- **Drag freeze:** while `activeDragId != null` re-measures are suppressed; on drag end set
  dirty (never reset the split synchronously — advisor); re-probe next layout cycle.
- Probe-mode sortable discipline: keep the `useSortable` **ref** (needed for measurement) but
  disable listeners, attributes, and transforms (`disabled: true` + `pointer-events-none`).

**Tests (jsdom, mocked geometry):** measure→partition wiring, freeze during drag, dirty on
resize, **dirty on activeKey-only change (no geometry change)**, resize-listener fallback,
Strict Mode double-invoke idempotence.
**Validate:** hook tests green.
**Commit:** `web: useWrappedOverflow measurement hook (+tests)`

## Phase 4a — UnifiedTabBar layout + probe/final rendering

- Outer: `[wrap area: flex-1 min-w-0 flex flex-wrap gap-x-0.5 gap-y-1 items-start py-1.5]`
  `[trailing cluster: shrink-0 natural width]`. Bar height content-driven (`h-auto min-h-9`);
  pills normalized `h-6`. One-row = 36px, two-row = 64px (spec height contract).
- Probe/final rendering per the hook; **remove** `handleWheel`, `onWheel`, and
  `overflow-x-auto scrollbar-hide overflow-y-hidden` container CSS entirely.

**Validate:** tsc; existing suite green; manual smoke: tabs render, wrap + cap works.
**Commit:** `web: tab bar multi-row wrap layout (probe/final render)`

## Phase 4b — "+N" chip + popover + a11y

- Chip inside the wrap-area flow after visible pills (participates in wrap; 40px reserved in
  measurement), label `min(count, 99) + "+"`, active-fallback highlight class.
- Popover (`ui/popover.tsx`): non-modal; rows = **sibling** buttons — row-activate
  (emoji + truncated title + badges, flex-1) and separate ✕ close (own tab stop). Roving
  tabindex arrow navigation across activate buttons; Enter/Space activates; Escape and
  outside-click close (Radix); `onCloseAutoFocus` → chip; **auto-close when the hidden set
  empties**. No `listbox`/`menu` roles.
- Activating a hidden tab focuses it; the promotion rule makes it visible on the next
  measurement cycle.

**Tests (component, `GitPanel.test.tsx` conventions):** chip N label + cap; popover rows =
hidden set in canonical order; activate promotes + switches; ✕ closes; focus returns to chip;
auto-close on empty.
**Validate:** those tests + tsc.
**Commit:** `web: tab bar overflow chip + popover (a11y wired)`

## Phase 4c — DnD integration (grid reorder + freeze)

- Swap strategy `horizontalListSortingStrategy` → `rectSortingStrategy`; `SortableContext`
  items = visibleKeys.
- New `web/src/components/Layout/gridKeyboardCoordinates.ts` — custom grid-aware coordinate
  getter (advisor: no dependable built-in): group `droppableRects` by top (same tolerance as
  P2), candidates restricted to visibleKeys, direction-aware nearest-center (adjacent
  horizontally; nearest X in the row above/below). Read current rects (not stale drag-time
  snapshots).
- Wire drag freeze (start/end) per Phase 3; persistence handler untouched.

> **Landed deviation:** `gridKeyboardCoordinates.ts` is deferred — the shipped code keeps
> `sortableKeyboardCoordinates` (handles the common case); adding it is a documented follow-up.
> `rectSortingStrategy` + the drag freeze are landed. `handleDragEnd` still reorders the
> complete `order` (incl. hidden keys), so `saveTabOrder` always persists the full sequence.

**Note (landed design):** the bar uses **one** `DndContext` for both the probe and final frames
(sensors stay `dndSensors`; in probe mode the pills themselves pass `disabled` to
`useSortable` + `pointer-events-none`, so no drag can start during measurement). Swapping
`sensors={[]}` → `sensors={dndSensors}` on the same `DndContext` broke dnd-kit's drag
registration, so the probe frame is not sensors-disabled at the DndContext level.

**Tests:** getter unit tests (ragged rows, boundaries, single row); component assertion that
`saveTabOrder` always receives the complete order incl. hidden keys during visible-only
reorders.
**Validate:** those tests + tsc.
**Commit:** `web: grid-aware dnd reorder for wrapped tab bar (+freeze)`

## Phase 5 — Stress harness + full validation gate

1. Dev-only `?tabStress=N` seeding in `UnifiedTabBar` (guard `import.meta.env.DEV`): N
   synthetic tabs to exercise wrap/cap deterministically.
2. `UnifiedTabBar.test.tsx` (component): chip never enters persistence inputs; popover live
   updates (hidden tab closed elsewhere → row removed; empty → auto-closed); uncapped
   degradation renders all tabs reachable.
3. **Full gates:** `cd web && pnpm exec vitest run && pnpm exec tsc --noEmit && pnpm run build`
   — all green.
4. **Manual QA checklist (browser — jsdom cannot validate flex):**
   - 10/25/50 tabs: wrap + 2-row cap, chip count correct, 1-row = 36px / 2-row = 64px.
   - Cross-row pointer drag AND cross-row keyboard reorder (long/short title mix, ragged
     widths).
   - Resize with popover open; title/process-badge change reflows; narrow window (trailing
     cluster min-width; one-pill-per-row degradation).
   - Strict Mode double-render check (no measure flicker loop).

**Commit:** `web: tab bar stress harness + component tests (full gate green)`

## Risks & mitigations

| Risk | Mitigation |
|---|---|
| flex-shrink invalidates width simulation | `flex: 0 0 auto` invariant (P2 onward) |
| dnd-kit rect staleness mid-drag | getter reads live `droppableRects` |
| epsilon too tight → wrap disagreement | start 4px; browser harness verifies at 10/25/50 tabs |
| Strict Mode double layout effects | idempotent measure; explicit P3 test |
| Probe frame visible flicker | layout-effect setState flushes before paint (React guarantee); harness check |
