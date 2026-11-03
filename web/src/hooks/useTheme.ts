// The colour helpers and the legacy 16-colour mapping live in
// `lib/themeColors` so `lib/appearanceTheme` can share them without an import
// cycle (this hook imports the appearance store, which imports them).
//
// Re-exported here because `commands.ts`, `ThemeForm.tsx` and the existing
// test suite import them from this module.
export {
  AA_NORMAL,
  applyThemeColors,
  computeThemeVars,
  contrastRatio,
  hexToHslTriplet,
  pickReadableFg,
} from "@/lib/themeColors";

import { useAppearanceTheme } from "@/lib/appearanceTheme";

/**
 * The web UI follows `appearance.color_theme` — by default the terminal theme
 * (`tui.theme`) when no appearance selection is stored — so there is no
 * separate light/dark toggle. This routes through the appearance store so a
 * converted VS Code theme (`ui` palette) applies with its authored colours,
 * while a theme with no rich palette keeps the legacy 16-colour mapping.
 *
 * The returned shape is unchanged: `serverColors` is the 16-field palette (or
 * null before the first load / after a failure, in which case the stylesheet
 * defaults in index.css remain untouched).
 */
export function useTheme() {
  const { theme } = useAppearanceTheme();
  return { serverColors: theme?.colors ?? null };
}
