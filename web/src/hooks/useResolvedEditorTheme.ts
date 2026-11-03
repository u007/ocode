import { useMemo, useSyncExternalStore } from "react";
import {
  resolveEffectiveEditorAppearance,
  useEditorAppearance,
  type EditorAppearance,
} from "../lib/editorAppearance";
import { useAppearanceTheme } from "../lib/appearanceTheme";
import { resolveEditorTheme, resolvedThemePolarity, type ResolvedEditorTheme } from "../lib/monacoTheme";

/**
 * One resolved editor theme for every Files surface.
 *
 * `FileEditor` and `FileTabContent` used to compute the light/dark polarity
 * themselves — from the stored preference and the COLOUR theme's `kind` — while
 * `FileEditor` separately decided its wrapper backdrop from a hardcoded
 * `id === "ocode-light"` check. Those three notions could disagree: with a
 * light `monaco.theme` under a dark colour theme the editor painted light while
 * the toolbar toggle said "Switch to light theme", so the first click visibly
 * did nothing.
 *
 * This hook resolves all three from ONE value (`resolveEditorTheme`, spec
 * §8.6):
 *
 * - `resolved` — the Monaco theme to `setTheme` (id + optional `define` payload).
 * - `effectiveAppearance` — the polarity that resolved theme actually paints
 *   with; drives the toggle label/`aria-pressed` and the preview pane.
 * - `isLightSurface` — the same polarity, for the editor wrapper's seam colour.
 */

/** Default persisted `monaco.theme`; matches `internal/monaco`'s default and
 *  the resolver's last-resort fallback, so the first paint is unchanged. */
export const DEFAULT_PERSISTED_MONACO_THEME = "ocode-dark";

let persistedMonacoTheme = DEFAULT_PERSISTED_MONACO_THEME;
const listeners = new Set<() => void>();

/** The last known `monaco.theme` (`GET /api/monaco/settings`). */
export function getPersistedMonacoTheme(): string {
  return persistedMonacoTheme;
}

/**
 * Record the persisted `monaco.theme`.
 *
 * `FileEditor` is the only writer (it already fetches the Monaco settings for
 * the editor options), so no extra request is made: `FileTabContent`'s toolbar
 * reads the same value through `useResolvedEditorTheme`. A failed fetch leaves
 * the last known value alone rather than resetting it — same last-good rule the
 * appearance store follows.
 */
export function setPersistedMonacoTheme(next: string | null | undefined): void {
  const value = next || DEFAULT_PERSISTED_MONACO_THEME;
  if (value === persistedMonacoTheme) return;
  persistedMonacoTheme = value;
  for (const listener of listeners) listener();
}

function subscribe(onChange: () => void): () => void {
  listeners.add(onChange);
  return () => {
    listeners.delete(onChange);
  };
}

export interface ResolvedEditorThemeHandle {
  resolved: ResolvedEditorTheme;
  effectiveAppearance: EditorAppearance;
  isLightSurface: boolean;
}

/** Resolve the Monaco theme, its polarity and the light-surface flag together. */
export function useResolvedEditorTheme(): ResolvedEditorThemeHandle {
  const appearanceValue = useEditorAppearance();
  const { theme: activeTheme } = useAppearanceTheme();
  const monacoTheme = useSyncExternalStore(
    subscribe,
    getPersistedMonacoTheme,
    getPersistedMonacoTheme,
  );

  return useMemo(() => {
    // The precedence lives in lib/monacoTheme.ts; the stored preference is only
    // one of its inputs, which is exactly why polarity is derived from the
    // RESULT rather than recomputed from the preference.
    const resolved = resolveEditorTheme({
      appearance: appearanceValue,
      theme: activeTheme,
      monacoTheme,
    });
    const effectiveAppearance = resolveEffectiveEditorAppearance(
      appearanceValue,
      resolvedThemePolarity(resolved),
    );
    return {
      resolved,
      effectiveAppearance,
      isLightSurface: effectiveAppearance === "light",
    };
  }, [appearanceValue, activeTheme, monacoTheme]);
}

/** Test hook: forget the cached setting and subscribers. */
export function __resetResolvedEditorThemeForTests(): void {
  persistedMonacoTheme = DEFAULT_PERSISTED_MONACO_THEME;
  listeners.clear();
}
