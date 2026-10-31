import { useSyncExternalStore } from "react";

/**
 * previewWidth — how wide the rendered markdown column is inside the preview
 * pane (sidebar PreviewHost, Preview tab, Files-tab preview).
 *
 * WHY this exists: the markdown preview used to be `max-w-none … p-3`, i.e.
 * edge-to-edge prose. That killed the `@tailwindcss/typography` 65ch gutter
 * bug (see `MarkdownViewer.width.test.tsx`) but left a wall of text with no
 * margin. This preference restores a deliberate, centred reading column whose
 * width is a PAPER width — so what you see approximates the printed page —
 * while `full` keeps the old edge-to-edge behaviour for people who want it.
 *
 * Presets are width-only on purpose: F4/folio is 210 mm wide, the same column
 * as A4, so it is folded into the A4 entry instead of shipping a second option
 * that renders pixel-identically. A paper-HEIGHT/page-break rendering would be
 * the only way to tell them apart, and that was deliberately not built.
 *
 * Persistence: one GLOBAL localStorage key (`ocode.ui.previewWidth.v1`). Like
 * the editor appearance override, this is a reading preference, not a property
 * of a project or a file — every preview should agree.
 *
 * `auto` resolves per LOCALE (US customary-paper locales → Letter, everyone
 * else → A4) and never touches storage, so a travelling/`en-US` user keeps the
 * standard they expect until they pick explicitly.
 */

export const PREVIEW_WIDTH_STORAGE_KEY = "ocode.ui.previewWidth.v1";

/** What the user picked. `auto` is the default and has no stored width. */
export type PreviewWidthPreset = "auto" | "letter" | "a4" | "full";

/** The concrete column a preset resolves to. */
export type ResolvedPreviewWidth = "letter" | "a4" | "full";

// Paper column widths at the CSS reference density of 96 dpi:
// Letter 8.5 in = 816 px, A4 210 mm = 793.7 px (rounded to 794 in the class).
// They live in `previewColumnClass` as arbitrary-value Tailwind classes on
// purpose — one source of truth, and the values are only consumed by CSS.

const listeners = new Set<() => void>();
/** Cached preset. `null` means "not resolved yet" so the snapshot stays
 *  referentially stable across renders (required by useSyncExternalStore). */
let current: PreviewWidthPreset | null = null;

function normalize(raw: string | null | undefined): PreviewWidthPreset | null {
  return raw === "auto" || raw === "letter" || raw === "a4" || raw === "full" ? raw : null;
}

function readStored(): PreviewWidthPreset | null {
  try {
    return normalize(window.localStorage.getItem(PREVIEW_WIDTH_STORAGE_KEY));
  } catch (err) {
    // localStorage can throw (disabled cookies, quota, opaque origin).
    console.warn("[previewWidth] failed to read preference", err);
    return null;
  }
}

function emit(): void {
  for (const l of listeners) l();
}

/** Runtime locale, defensively (jsdom always has it; a bare Node import may not). */
function runtimeLocale(): string {
  try {
    return typeof navigator !== "undefined" && navigator.language ? navigator.language : "en-US";
  } catch (err) {
    console.warn("[previewWidth] failed to read locale", err);
    return "en-US";
  }
}

/**
 * Locale → default paper. US customary-paper markets use Letter; everywhere
 * else uses A4. Deliberately coarse: markdown carries no page size, so the
 * only honest signal is where the reader is.
 *
 * The region set is the CLDR territory list for countries whose default paper
 * is US Letter (Canada and Mexico included, even though they are metric). It is
 * NOT the "countries that do not use metric units" list: Liberia (LR) does use
 * Letter, but Myanmar (MM) is an A4 market, so a metric-vs-imperial list would
 * give a Myanmar reader the wrong column and deny it to Canada, Mexico and the
 * Philippines.
 */
const LETTER_REGIONS = new Set([
  "us", "ca", "mx", "ph", "cl", "co", "cr", "sv", "gt", "ni", "pa", "pr", "ve", "bz", "lr",
]);

export function defaultPaperForLocale(locale: string): "letter" | "a4" {
  const tag = (locale || "").toLowerCase().replace("_", "-");
  const region = tag.split("-")[1] ?? "";
  return LETTER_REGIONS.has(region) ? "letter" : "a4";
}

export interface ResolvePreviewWidthOptions {
  /** BCP-47 tag used when the preset is `auto`. Defaults to the runtime. */
  locale?: string;
}

/**
 * Preset (+ locale) → the column actually rendered.
 *
 * There is deliberately no `prose` switch here. Code and plain-text previews
 * stay full width by CONSTRUCTION: `PreviewSurface` renders MarkdownViewer only
 * for `kind === "markdown"`, and the header control is gated on the same kind,
 * so a non-prose surface never reaches this function. A dead option would let
 * the docs claim a guard that nothing exercises.
 */
export function resolvePreviewWidth(
  preset: PreviewWidthPreset,
  opts: ResolvePreviewWidthOptions = {},
): ResolvedPreviewWidth {
  if (preset === "auto") return defaultPaperForLocale(opts.locale ?? runtimeLocale());
  return preset;
}

/**
 * Tailwind classes for the column wrapper. `prose` styling lives on the
 * SCROLLER (with `max-w-none`, so typography cannot cap it); this wrapper is a
 * child and only owns the measure, the centring and the horizontal margin.
 *
 * The horizontal padding is what delivers the margin the user actually asked
 * for: when the pane is narrower than the sheet (the ~400px sidebar preview),
 * `mx-auto` has nothing to centre and the padding becomes the margin. It is
 * fluid rather than a fixed `px-8`, because the Files-tab SPLIT view renders the
 * same column in a pane the user can drag down to ~20% of the tab: 64px of
 * fixed padding there leaves almost no measure. `4%` of the scroller width,
 * clamped to 12–32px, gives the sidebar ~16px and a full-width pane 32px.
 */
export function previewColumnClass(width: ResolvedPreviewWidth): string {
  if (width === "letter") return "mx-auto w-full max-w-[816px] px-[max(0.75rem,min(2rem,4%))] py-3";
  if (width === "a4") return "mx-auto w-full max-w-[794px] px-[max(0.75rem,min(2rem,4%))] py-3";
  return "mx-auto w-full max-w-none px-3 py-3";
}

/** Short label for the header control. Folio/F4 shares the A4 column. */
export function previewWidthShortLabel(width: ResolvedPreviewWidth): string {
  if (width === "letter") return "Letter";
  if (width === "a4") return "A4";
  return "Full";
}

/** Menu label. The A4 entry names F4 so the folio request is visible even
 *  though it is the same column width. The `auto` entry resolves ITS OWN column
 *  rather than taking a caller-supplied value: passing the CURRENT preset's
 *  resolution made the Auto row read "Auto — A4" while Auto rendered Letter.
 *  `locale` defaults to the runtime, exactly as the renderer resolves it. */
export function previewWidthMenuLabel(preset: PreviewWidthPreset, locale?: string): string {
  if (preset === "letter") return "Letter (8.5 × 11 in)";
  if (preset === "a4") return "A4 / F4 (210 mm)";
  if (preset === "full") return "Full width";
  return `Auto — ${previewWidthShortLabel(resolvePreviewWidth("auto", { locale }))}`;
}

export function getPreviewWidthPreset(): PreviewWidthPreset {
  if (current === null) current = readStored() ?? "auto";
  return current;
}

export function setPreviewWidthPreset(next: PreviewWidthPreset): void {
  if (getPreviewWidthPreset() === next) return;
  current = next;
  try {
    window.localStorage.setItem(PREVIEW_WIDTH_STORAGE_KEY, next);
  } catch (err) {
    // The in-memory value still drives this session; persistence is best-effort.
    console.warn("[previewWidth] failed to persist preference", err);
  }
  emit();
}

function subscribe(onChange: () => void): () => void {
  listeners.add(onChange);
  return () => {
    listeners.delete(onChange);
  };
}

// Cross-window/tab sync. Another browser window writing the preference must
// move this one too (the terminal-tab lesson: a per-origin preference that
// only one window sees is a bug report waiting to happen).
if (typeof window !== "undefined" && typeof window.addEventListener === "function") {
  window.addEventListener("storage", (event) => {
    if (event.key !== PREVIEW_WIDTH_STORAGE_KEY) return;
    const next = normalize(event.newValue) ?? "auto";
    if (getPreviewWidthPreset() === next) return;
    current = next;
    emit();
  });
}

/** React binding — one source of truth for every mounted preview. */
export function usePreviewWidthPreset(): PreviewWidthPreset {
  return useSyncExternalStore(subscribe, getPreviewWidthPreset, getPreviewWidthPreset);
}

/** Test hook: drop the cached value and subscribers. */
export function __resetPreviewWidthForTests(): void {
  current = null;
  listeners.clear();
}
