import { useState, useEffect } from "react";
import { api } from "@/api/client";
import type { ThemeColors } from "@/api/types";

// ── Color helpers ──

function hexToRgb(hex: string): [number, number, number] | null {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
  if (!m) return null;
  const n = parseInt(m[1], 16);
  return [(n >> 16) & 0xff, (n >> 8) & 0xff, n & 0xff];
}

// Tailwind consumes these variables as HSL component triplets — see
// tailwind.config.js `hsl(var(--x))` wrappers and index.css `:root` defaults.
// Server colors arrive as hex, so they must be converted to "H S% L%" form.
function hexToHslTriplet(hex: string): string | null {
  const rgb = hexToRgb(hex);
  if (!rgb) return null;
  const [r, g, b] = rgb.map((c) => c / 255);
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  let h = 0;
  let s = 0;
  if (max !== min) {
    const d = max - min;
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    if (max === r) h = (g - b) / d + (g < b ? 6 : 0);
    else if (max === g) h = (b - r) / d + 2;
    else h = (r - g) / d + 4;
    h /= 6;
  }
  const round = (v: number) => Math.round(v * 10) / 10;
  return `${round(h * 360)} ${round(s * 100)}% ${round(l * 100)}%`;
}

// WCAG 2.x relative luminance and contrast ratio.
function relLuminance([r, g, b]: [number, number, number]): number {
  const lin = (c: number) => {
    const s = c / 255;
    return s <= 0.04045 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

// Contrast ratio (1–21) between two hex colors. Invalid hex returns 21 so
// callers treat unknown colors as "already fine" and leave them untouched
// (applyThemeColors logs a warning for those separately).
export function contrastRatio(a: string, b: string): number {
  const ca = hexToRgb(a);
  const cb = hexToRgb(b);
  if (!ca || !cb) return 21;
  const la = relLuminance(ca);
  const lb = relLuminance(cb);
  const hi = Math.max(la, lb);
  const lo = Math.min(la, lb);
  return (hi + 0.05) / (lo + 0.05);
}

// Linear RGB blend: t=0 → a, t=1 → b.
function mixHex(a: string, b: string, t: number): string {
  const ca = hexToRgb(a);
  const cb = hexToRgb(b);
  if (!ca || !cb) return t < 0.5 ? a : b;
  const part = (i: number) =>
    Math.round(ca[i] + (cb[i] - ca[i]) * t)
      .toString(16)
      .padStart(2, "0");
  return `#${part(0)}${part(1)}${part(2)}`;
}

// WCAG AA for normal-size text.
const AA_NORMAL = 4.5;

// Pick a readable foreground for a solid colored surface from theme
// candidates (first candidate that clears AA wins, preserving theme
// character); fall back to pure black/white when no theme color passes.
function pickReadableFg(bg: string, candidates: string[]): string {
  for (const c of candidates) {
    if (contrastRatio(bg, c) >= AA_NORMAL) return c;
  }
  return contrastRatio(bg, "#ffffff") >= contrastRatio(bg, "#000000")
    ? "#ffffff"
    : "#000000";
}

// Blend `fg` toward `fallback` in small steps until it clears AA on every
// background in `bgs`. Contrast is re-evaluated on the actual blended color
// each step (RGB interpolation is not assumed to be monotonic in luminance).
function ensureContrast(fg: string, bgs: string[], fallback: string): string {
  const ok = (c: string) => bgs.every((bg) => contrastRatio(c, bg) >= AA_NORMAL);
  if (ok(fg)) return fg;
  for (let t = 0.1; t < 1.0; t += 0.1) {
    const c = mixHex(fg, fallback, t);
    if (ok(c)) return c;
  }
  return mixHex(fg, fallback, 1.0);
}

/**
 * Map a server (terminal) theme palette onto the shadcn CSS variables used by
 * the web/desktop UI, returning final hex values per variable.
 *
 * Terminal palettes assign colors for text-on-background use only; mapping
 * them 1:1 onto surface roles can produce unreadable pairs (e.g. LCARS's tan
 * `text` on its orange `user` bubble is ~1.4:1). Foreground colors that sit
 * on solid colored surfaces (primary/accent/destructive/secondary) are
 * therefore checked for WCAG AA contrast and replaced with the best-fitting
 * theme color (black/white only as a last resort). The muted surface/foreground
 * pair is dimmed/brightened only when the raw pair fails, so themes that
 * already read well are left untouched.
 */
export function computeThemeVars(colors: ThemeColors): Record<string, string> {
  const fgCandidates = [colors.text, colors.background, colors.selected_fg];

  // `dim` doubles as the muted surface (assistant bubbles, cards, inputs) and
  // `hint` as the muted foreground. If hint is unreadable on dim, first nudge
  // the surface toward the page background — never toward `text`, which could
  // flip the surface to the opposite polarity and break foreground text —
  // keeping the blend that maximizes hint contrast (capped so the surface
  // retains a tint). If the surface tweak isn't enough, move the foreground
  // toward the theme's text color and finally toward neutral black/white,
  // following the page polarity. Only the pieces that actually fail change.
  let muted = colors.dim;
  if (contrastRatio(muted, colors.hint) < AA_NORMAL) {
    let bestRatio = contrastRatio(muted, colors.hint);
    for (let t = 0.1; t <= 0.7; t += 0.1) {
      const cand = mixHex(colors.dim, colors.background, t);
      const ratio = contrastRatio(cand, colors.hint);
      if (ratio > bestRatio) {
        bestRatio = ratio;
        muted = cand;
      }
      if (ratio >= AA_NORMAL) break;
    }
  }
  const pageIsLight =
    contrastRatio(colors.background, "#000000") >
    contrastRatio(colors.background, "#ffffff");
  let mutedFg = ensureContrast(colors.hint, [muted, colors.background], colors.text);
  mutedFg = ensureContrast(mutedFg, [muted, colors.background], pageIsLight ? "#000000" : "#ffffff");

  return {
    "--background": colors.background,
    "--foreground": colors.text,
    "--primary": colors.user,
    "--primary-foreground": pickReadableFg(colors.user, fgCandidates),
    "--accent": colors.accent,
    "--accent-foreground": pickReadableFg(colors.accent, fgCandidates),
    "--destructive": colors.error,
    "--destructive-foreground": pickReadableFg(colors.error, fgCandidates),
    "--input": colors.dim,
    "--border": colors.border,
    "--muted": muted,
    "--muted-foreground": mutedFg,
    "--card": colors.background,
    "--card-foreground": colors.text,
    "--popover": colors.background,
    "--popover-foreground": colors.text,
    "--secondary": colors.selected_bg,
    // selected_fg/selected_bg are designed as a pair; guard anyway for
    // custom themes where the pair fails AA.
    "--secondary-foreground": pickReadableFg(colors.selected_bg, [
      colors.selected_fg,
      ...fgCandidates,
    ]),
    "--ring": colors.user,
  };
}

export function applyThemeColors(colors: ThemeColors) {
  const root = document.documentElement;
  const vars = computeThemeVars(colors);
  for (const [name, hex] of Object.entries(vars)) {
    const triplet = hexToHslTriplet(hex);
    if (triplet === null) {
      console.warn(`Skipping theme variable ${name}: invalid hex color`, hex);
      continue;
    }
    root.style.setProperty(name, triplet);
  }
}

export function useTheme() {
  const [serverColors, setServerColors] = useState<ThemeColors | null>(null);

  // Fetch server theme colors on mount. The web UI follows the terminal
  // theme; there is no separate light/dark toggle. On fetch failure the
  // stylesheet defaults in index.css remain untouched.
  useEffect(() => {
    api
      .getTheme()
      .then((resp) => {
        setServerColors(resp.colors);
        applyThemeColors(resp.colors);
      })
      .catch((err) => {
        console.warn("Failed to fetch server theme, keeping defaults:", err);
      });
  }, []);

  return { serverColors };
}
