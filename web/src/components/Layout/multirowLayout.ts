/**
 * Pure arithmetic layout for the multi-row wrapping tab bar.
 *
 * All functions here are pure (no DOM, no React). The UnifiedTabBar's
 * measurement hook (`useWrappedOverflow`) sends probe-measured geometry
 * (`offsetTop` → row bucket, `offsetWidth` → width) and this module decides
 * which pills render in the bar (first `MAX_ROWS` rows) and which collapse
 * into the "+N" overflow chip.
 *
 * Two extra invariants beyond a naive "first N rows visible":
 *  - The "+N" chip must always sit on the last visible row (the chip never
 *    yields; if it would wrap to an overflow row, tail pills are demoted).
 *  - The active tab must always be visible (width-checked promotion: it is
 *    swapped into the last row, demoting the rightmost pill(s) as needed,
 *    and only degrades to `uncapped` in the absurd-narrow case where even a
 *    lone active pill plus the chip cannot fit).
 */

/** Map probe-measured `offsetTop` per pill → row index (0 = first row). */
export function computeRowBuckets(tops: number[], tolerancePx = 2): number[] {
  const buckets: number[] = [];
  let cluster = -1;
  let anchor: number | null = null;
  for (const top of tops) {
    if (anchor === null || Math.abs(top - anchor) > tolerancePx) {
      cluster++;
      anchor = top;
    }
    buckets.push(cluster);
  }
  return buckets;
}

export interface PartitionInput {
  /** Ordered tab keys (persisted order). */
  keys: string[];
  /** Probe-measured row bucket per key (parallel to `keys`). */
  buckets: number[];
  /** Probe-measured `offsetWidth` per key (parallel to `keys`). */
  widths: number[];
  /** Inner width of the wrap area. */
  containerWidth: number;
  /** Reserved width of the "+N" chip (fixed, e.g. 40px). */
  chipReservedPx: number;
  /** Flex gap between pills (`gap-x-0.5`). */
  gapPx?: number;
  /** Tolerance so a row that is just over capacity is still accepted. */
  epsilonPx?: number;
  /** Number of visible rows (2 in current design). */
  maxRows: number;
  /** The tab that must be visible; `null` when nothing is active. */
  activeKey: string | null;
}

export interface PartitionResult {
  /** Keys rendered as pills in the bar, in persisted order. */
  visibleKeys: string[];
  /** Keys collapsed into the "+N" overflow chip, in persisted order. */
  hiddenKeys: string[];
  /** True in the absurd-narrow case: render ALL keys uncapped past MAX_ROWS. */
  uncapped: boolean;
}

const DEFAULT_GAP = 2;
const DEFAULT_EPSILON = 4;

/**
 * Would `pillKeys` plus the trailing chip fit on one row?
 *
 * Geometry: n pills + 1 chip = n + 1 flex items, therefore n gaps
 * (`gapPx * n`); consumed width = Σ(pillWidth) + chip + gapPx·n. This is the
 * corrected gap count (a common off-by-one is to use `n-1`).
 */
function fitsRow(
  pillKeys: string[],
  widths: Record<string, number>,
  chipPx: number,
  gap: number,
  containerWidth: number,
  eps: number
): boolean {
  let w = chipPx;
  for (const k of pillKeys) w += widths[k] ?? 0;
  w += gap * pillKeys.length;
  return w <= containerWidth - eps;
}

export function partitionVisible(input: PartitionInput): PartitionResult {
  const { keys, buckets } = input;
  const gap = input.gapPx ?? DEFAULT_GAP;
  const eps = input.epsilonPx ?? DEFAULT_EPSILON;
  const maxRows = input.maxRows;
  const chip = input.chipReservedPx;
  const containerWidth = input.containerWidth;

  const widths: Record<string, number> = {};
  keys.forEach((k, i) => {
    widths[k] = input.widths[i] ?? 0;
  });

  // --- Baseline: group keys into rows by their probe bucket ---
  const rowCount = Math.max(maxRows, ...buckets, 0) + 1;
  const rows: string[][] = Array.from({ length: rowCount }, () => []);
  const hidden: string[] = [];
  keys.forEach((k, i) => {
    const b = buckets[i];
    if (b < maxRows) rows[b].push(k);
    else hidden.push(k);
  });

  const lastRowIndex = Math.min(maxRows - 1, rows.length - 1);
  let lastRow = rows[lastRowIndex] ?? [];

  // --- Chip-fit step: the chip must fit on the last visible row ---
  while (lastRow.length > 0 && !fitsRow(lastRow, widths, chip, gap, containerWidth, eps)) {
    const demoted = lastRow.pop()!;
    hidden.push(demoted);
  }

  // --- Promotion step: the active tab must be visible ---
  let uncapped = false;
  if (input.activeKey) {
    const activeHidden = hidden.includes(input.activeKey);
    if (activeHidden) {
      const candidate = [...lastRow, input.activeKey];
      // Drop pills from the right of the row (before the active pill) until
      // the active pill fits; if even a lone active + chip cannot fit,
      // degrade to `uncapped`.
      while (candidate.length > 1 && !fitsRow(candidate, widths, chip, gap, containerWidth, eps)) {
        const removed = candidate.splice(candidate.length - 2, 1)[0];
        if (!hidden.includes(removed)) hidden.push(removed);
      }
      if (!fitsRow([input.activeKey], widths, chip, gap, containerWidth, eps)) {
        uncapped = true;
      } else {
        lastRow = candidate;
        const hi = hidden.indexOf(input.activeKey);
        if (hi !== -1) hidden.splice(hi, 1);
      }
    }
  }

  if (uncapped) {
    return { visibleKeys: [...keys], hiddenKeys: [], uncapped: true };
  }

  // --- Rebuild visible in canonical row order, de-duplicated ---
  const seen = new Set<string>();
  const visible: string[] = [];
  const pushRow = (row: string[]) => {
    for (const k of row) {
      if (!seen.has(k)) {
        seen.add(k);
        visible.push(k);
      }
    }
  };
  for (let r = 0; r < maxRows; r++) {
    pushRow(r === lastRowIndex ? lastRow : (rows[r] ?? []));
  }

  const hiddenOrdered = hidden
    .filter((k) => !seen.has(k))
    .sort((a, b) => keys.indexOf(a) - keys.indexOf(b));

  return { visibleKeys: visible, hiddenKeys: hiddenOrdered, uncapped: false };
}
