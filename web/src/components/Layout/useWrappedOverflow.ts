/**
 * Pure partition core for the wrapped, row-capped tab bar (spec:
 * docs/superpowers/specs/2026-09-06-multirow-tab-bar-design.md).
 *
 * This module contains ONLY pure, DOM-free exports (unit-tested without
 * jsdom). The `useWrappedOverflow` hook (added in the next phase) is the
 * measurement wiring that feeds these functions probe geometry.
 *
 * Geometry contract (all phases): pills are `flex: 0 0 auto` (never shrink),
 * so measured `offsetWidth`s simulate flex-wrap decisions exactly. The chip
 * probe is rendered last at a fixed reserved width (`chipReservedPx`), so
 * "the chip probe wrapped past maxRows" (its bucket ≥ maxRows in the probe
 * frame) is equivalent to the arithmetic condition "the chip does not fit
 * after the last baseline-visible pill":
 *
 *   Σ(row widths) + gapPx·n + chipReservedPx ≤ containerWidth − epsilonPx
 *
 * (gapPx·n covers the n−1 pill gaps plus the chip gap; advisor-confirmed.)
 * We use the arithmetic form so the fit is re-checked after every demotion
 * without needing the probe chip's bucket as an input.
 */

/** Default gap between pills on a row (`gap-x-0.5`). */
export const DEFAULT_GAP_PX = 2;
/** Slack absorbing measurement noise so the width simulation and real flex
 *  wrapping cannot disagree at the boundary. */
export const DEFAULT_EPSILON_PX = 4;

/**
 * Group flex-measured `offsetTop`s (in DOM/persisted order) into row indices.
 * A pill starts a new row when its top exceeds the current row's anchor top
 * (the top of the row's first pill) by more than `tolerancePx` — comparison
 * is against the row anchor, not the immediately preceding pill, so subpixel
 * jitter along a line cannot cascade into phantom rows.
 */
export function computeRowBuckets(tops: number[], tolerancePx = DEFAULT_EPSILON_PX): number[] {
  const buckets: number[] = [];
  let row = 0;
  let anchor = 0;
  for (const top of tops) {
    if (buckets.length === 0) {
      anchor = top;
    } else if (top - anchor > tolerancePx) {
      row += 1;
      anchor = top;
    }
    buckets.push(row);
  }
  return buckets;
}

export interface PartitionInput {
  /** Ordered (persisted) tab keys. Arrays below are index-aligned with this. */
  keys: string[];
  /** Probe-measured row index per key (see `computeRowBuckets`). */
  buckets: number[];
  /** Probe-measured `offsetWidth` per key. */
  widths: number[];
  /** Wrap-area inner width available to pills + chip. */
  containerWidth: number;
  /** Reserved chip width (40 — fits the widest "99+" label). */
  chipReservedPx: number;
  /** Gap between items on a row (default 2, `gap-x-0.5`). */
  gapPx?: number;
  /** Boundary slack (default 4). */
  epsilonPx?: number;
  /** Row cap (2). */
  maxRows: number;
  /** Active tab key, or null. */
  activeKey: string | null;
}

export interface PartitionResult {
  visibleKeys: string[];
  hiddenKeys: string[];
  /** True when capping is infeasible (absurd-narrow container): the caller
   *  renders ALL pills uncapped (bar may exceed maxRows) and no chip — every
   *  tab stays directly reachable. */
  uncapped?: boolean;
}

/**
 * Decide which tabs render inside the capped wrap area and which collapse
 * into the "+N" chip. Steps:
 *
 * 1. Baseline: pills measured on a row < maxRows stay visible; the rest hide.
 * 2. Chip-fit (only when pills are hidden, i.e. the chip renders): demote
 *    tail pills of the last visible row (reverse persisted order) until the
 *    reserved chip fits on that row. The chip never yields; a chip-only row
 *    is the accepted ultra-narrow state.
 * 3. Promotion (width-checked): the active tab is always visible. Greedily
 *    repack the last visible row with the active pill appended; pills beyond
 *    capacity (and everything after) demote. The prefix is accepted only if
 *    it includes the active pill; otherwise the active pill is retried alone,
 *    and if it cannot fit even with every other pill demoted the partition
 *    degrades to `uncapped: true` (all pills rendered, no chip).
 *
 * All loops are bounded by the visible-pill count and always terminate.
 * Output arrays are in canonical persisted order.
 */
export function partitionVisible(input: PartitionInput): PartitionResult {
  const gapPx = input.gapPx ?? DEFAULT_GAP_PX;
  const epsilonPx = input.epsilonPx ?? DEFAULT_EPSILON_PX;
  const { keys, buckets, widths, containerWidth, chipReservedPx, maxRows, activeKey } = input;
  const budget = containerWidth - epsilonPx;

  // Baseline partition. Sets iterate in insertion (ascending index = persisted)
  // order, so both stay canonical throughout.
  const visible = new Set<number>();
  const hidden = new Set<number>();
  for (let i = 0; i < keys.length; i++) {
    (buckets[i] < maxRows ? visible : hidden).add(i);
  }

  // No overflow → no chip renders → nothing to reserve.
  if (hidden.size === 0) {
    return { visibleKeys: keys.slice(), hiddenKeys: [] };
  }

  // A required chip that cannot fit on any row by itself means capping is
  // infeasible: degrade to uncapped (all pills rendered, no chip — every tab
  // directly reachable).
  if (chipReservedPx > budget) {
    return { visibleKeys: keys.slice(), hiddenKeys: [], uncapped: true };
  }

  const sumWidths = (idxs: number[]): number => {
    let sum = 0;
    for (const i of idxs) sum += widths[i];
    return sum;
  };

  /** Row of `n` pills plus the chip: Σwidths + n gaps (n−1 pill gaps + chip gap). */
  const rowWithChipFits = (idxs: number[]): boolean =>
    sumWidths(idxs) + gapPx * idxs.length + chipReservedPx <= budget;

  /** Group visible indices by measured row; groups keep persisted order. */
  const visibleRows = (): Map<number, number[]> => {
    const rows = new Map<number, number[]>();
    for (const i of visible) {
      const b = buckets[i];
      const arr = rows.get(b);
      if (arr) arr.push(i);
      else rows.set(b, [i]);
    }
    return rows;
  };
  const lastVisibleRow = (): { bucket: number; idxs: number[] } | null => {
    const rows = visibleRows();
    let bucket = -1;
    for (const b of rows.keys()) if (b > bucket) bucket = b;
    return bucket === -1 ? null : { bucket, idxs: rows.get(bucket)! };
  };

  // --- Chip-fit step (before promotion): the chip lands after the last
  // visible pill, so it must fit on the last visible row. Demote tail pills
  // (reverse persisted order) until it fits; a chip-only row is accepted.
  let lastRow = lastVisibleRow();
  if (lastRow) {
    while (lastRow.idxs.length > 0 && !rowWithChipFits(lastRow.idxs)) {
      const victim = lastRow.idxs[lastRow.idxs.length - 1];
      lastRow.idxs.pop();
      visible.delete(victim);
      hidden.add(victim);
    }
  }

  // --- Promotion (width-checked, runs before the render): the active tab is
  // always visible. Active ends the candidate sequence by construction.
  if (activeKey !== null) {
    const activeIdx = keys.indexOf(activeKey);
    if (activeIdx !== -1 && hidden.has(activeIdx)) {
      // Recompute after chip-fit demotions.
      lastRow = lastVisibleRow();
      const baseRow = lastRow ? lastRow.idxs : [];
      // Candidate sequence = last visible row's pills + active appended.
      const candidates = baseRow.includes(activeIdx) ? baseRow : [...baseRow, activeIdx];

      const fill = (reserveChip: boolean): number[] => {
        const kept: number[] = [];
        let sum = 0;
        for (const c of candidates) {
          // Adding the m-th pill: m pills have m−1 mutual gaps; with the chip
          // there is one more gap (pill→chip), i.e. gapPx·m total.
          const gaps = kept.length + (reserveChip ? 1 : 0);
          const total = sum + widths[c] + gapPx * gaps + (reserveChip ? chipReservedPx : 0);
          if (total > budget) break;
          kept.push(c);
          sum += widths[c];
        }
        return kept;
      };

      // Every demoting outcome keeps the hidden set non-empty, so reserving
      // the chip inside the fill is exactly the cases that need it. The one
      // exception is the boundary where demoting nothing empties the hidden
      // set (active was the only hidden key): there the chip vanishes and the
      // row may use the full width — retried below without the reservation.
      const othersHidden = [...hidden].filter((i) => i !== activeIdx);
      let kept = fill(true);
      if (!kept.includes(activeIdx) && othersHidden.length === 0) {
        const keptNoChip = fill(false);
        if (keptNoChip.includes(activeIdx)) kept = keptNoChip;
      }

      if (kept.includes(activeIdx)) {
        for (const c of baseRow) {
          if (!kept.includes(c)) {
            visible.delete(c);
            hidden.add(c);
          }
        }
        visible.add(activeIdx);
        hidden.delete(activeIdx);
      } else {
        // Active cannot share the last row with anything. Retry it alone.
        // The chip renders unless nothing else would remain hidden.
        const chipNeeded = othersHidden.length > 0 || baseRow.length > 0;
        const activeFits = chipNeeded
          ? widths[activeIdx] + gapPx + chipReservedPx <= budget
          : widths[activeIdx] <= budget;
        if (activeFits) {
          for (const c of baseRow) {
            visible.delete(c);
            hidden.add(c);
          }
          visible.add(activeIdx);
          hidden.delete(activeIdx);
        } else {
          // Absurd-narrow degradation: capping is infeasible; render all
          // pills (bar may exceed maxRows) and drop the chip.
          return { visibleKeys: keys.slice(), hiddenKeys: [], uncapped: true };
        }
      }
    }
  }

  const toKeys = (idxs: Iterable<number>): string[] =>
    [...idxs].sort((a, b) => a - b).map((i) => keys[i]);
  return { visibleKeys: toKeys(visible), hiddenKeys: toKeys(hidden) };
}
