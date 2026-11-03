import type { ConvertedTheme, MonacoTheme, MonacoTokenRule } from "../api/types";
import type { EditorAppearance, EditorAppearanceValue } from "./editorAppearance";

/**
 * One resolver for the editor palette (design spec §8.6, binding).
 *
 * Three controls used to disagree about the Monaco theme: the `monaco.theme`
 * setting, the `editorAppearance` light/dark toggle, and the active
 * `appearance.color_theme`. `FileEditor` used to coerce a light `monaco.theme`
 * back to `ocode-dark` when the toggle said dark, so picking a light VS Code
 * theme silently produced the wrong editor palette with nothing in the UI
 * saying why. The precedence now lives here — first match wins:
 *
 * 1. `editorAppearance` is explicitly `light`/`dark` → `ocode-light`/`ocode-dark`.
 *    Settings shows "Editor override is on" plus a *Use theme colours* action.
 * 2. The active colour theme is a VS Code theme (`vscode:<id>`) → its converted
 *    Monaco palette, defined here as `ocode-vscode:<id>`.
 * 3. `monaco.theme` names a defined Monaco theme → that theme. This holds with
 *    OR without a loaded colour theme: before this feature the setting was
 *    honoured unconditionally, so a slow/failed appearance fetch (offline, 401)
 *    must not silently drop the user's editor palette.
 * 4. A loaded builtin/opencode theme without a usable `monaco.theme` → the
 *    theme's polarity default (`ocode-dark` / `ocode-light`).
 * 5. Nothing loaded and no defined `monaco.theme` → `ocode-dark`.
 *
 * Case 4/5 are ordered as they are so the outcome matches §8.6's table, with
 * the one deliberate widening in case 3 described above. Keep this table in
 * sync with §8.6 — the Settings compliance notice and the precedence tests
 * both read from it.
 */

/** Prefix of the Monaco theme defined from a converted VS Code theme. */
export const VSCODE_MONACO_THEME_PREFIX = "ocode-vscode:";

/** Id prefix the appearance service gives a converted VS Code theme. */
const VSCODE_COLOR_THEME_PREFIX = "vscode:";

/** Monaco theme ids that are always available, so naming one needs no
 *  `defineTheme` call: the custom pair defined by `FileEditor` and Monaco's own
 *  three built-ins. */
const DEFINED_MONACO_THEMES = new Set(["ocode-dark", "ocode-light", "vs", "vs-dark", "hc-black"]);

/** Monaco's accepted `base` values. */
type MonacoBase = "vs" | "vs-dark" | "hc-black" | "hc-light";

export interface ResolvedEditorTheme {
  /** The Monaco theme id to `setTheme`. */
  id: string;
  /** `defineTheme` payload for an id this module owns; `null` for a theme that
   *  is already defined (the custom pair or a Monaco built-in). */
  define: MonacoTheme | null;
}

/** The subset of `monaco.editor` this module needs — structurally satisfied by
 *  the real Monaco namespace, so tests can pass a spy-shaped fake without
 *  importing the editor. */
export interface MonacoThemeEditor {
  defineTheme(id: string, theme: MonacoThemeDefinition): void;
  setTheme(id: string): void;
}

export interface MonacoThemeApi {
  editor: MonacoThemeEditor;
}

/** `monaco.editor.IStandaloneThemeData` minus the parts we always set. */
export interface MonacoThemeDefinition {
  base: MonacoBase;
  inherit: boolean;
  rules: MonacoTokenRule[];
  colors: Record<string, string>;
}

function isVSCodeTheme(theme: ConvertedTheme): boolean {
  return typeof theme.id === "string" && theme.id.startsWith(VSCODE_COLOR_THEME_PREFIX);
}

function normalizeBase(base: string | undefined, theme: ConvertedTheme): MonacoBase {
  if (base === "vs" || base === "vs-dark" || base === "hc-black" || base === "hc-light") {
    return base;
  }
  return theme.kind === "light" ? "vs" : "vs-dark";
}

/** The theme's polarity default — the last resort before the hard fallback. */
function polarityDefault(theme: ConvertedTheme): string {
  return theme.kind === "light" ? "ocode-light" : "ocode-dark";
}

export interface ResolveEditorThemeInput {
  appearance: EditorAppearanceValue;
  /** The ACTIVE converted colour theme, or `null` before it loads. */
  theme: ConvertedTheme | null;
  /** The persisted `monaco.theme` setting (`internal/monaco`). */
  monacoTheme: string;
}

/**
 * Resolve the editor palette for the four §8.6 cases. Pure: no Monaco call and
 * no storage read, so every branch is directly testable.
 */
export function resolveEditorTheme({
  appearance,
  theme,
  monacoTheme,
}: ResolveEditorThemeInput): ResolvedEditorTheme {
  // 1. Explicit override — outranks the colour theme entirely.
  if (appearance === "light") return { id: "ocode-light", define: null };
  if (appearance === "dark") return { id: "ocode-dark", define: null };

  // 2. A VS Code theme contributes its converted Monaco palette. The id is
  //    prefixed so a converted theme can never collide with a persisted
  //    `monaco.theme` name.
  if (theme && isVSCodeTheme(theme)) {
    const suffix = theme.id.slice(VSCODE_COLOR_THEME_PREFIX.length);
    return {
      id: `${VSCODE_MONACO_THEME_PREFIX}${suffix}`,
      define: {
        base: normalizeBase(theme.monaco?.base, theme),
        colors: theme.monaco?.colors ?? {},
        rules: theme.monaco?.rules ?? [],
      },
    };
  }

  // 3. `monaco.theme` still owns the choice exactly as it did before this
  //    feature, as long as it names a defined theme. Deliberately NOT gated on
  //    a loaded theme: while the appearance fetch is pending (or failed —
  //    offline, 401) a loaded-theme-only rule dropped the user's choice.
  if (monacoTheme && DEFINED_MONACO_THEMES.has(monacoTheme)) {
    return { id: monacoTheme, define: null };
  }

  // 4. A loaded builtin/opencode theme contributes its polarity default.
  if (theme) return { id: polarityDefault(theme), define: null };

  // 5. Nothing loaded — the app's own default.
  return { id: "ocode-dark", define: null };
}

/** Monaco theme ids that paint on a light surface. `hc-light` is not a
 *  selectable `monaco.theme`, but it is a valid `base` so a converted theme
 *  can carry it. */
const LIGHT_MONACO_THEMES = new Set(["ocode-light", "vs", "hc-light"]);

/**
 * The polarity the RESOLVED editor theme actually paints with.
 *
 * This — not the stored preference, and not the colour theme's `kind` — is what
 * the light/dark toggle, the editor wrapper backdrop and the rendered preview
 * must follow: a light `monaco.theme` under a dark colour theme renders a light
 * editor, and a toggle labelled "Switch to light theme" there would be lying
 * (its first click would visibly do nothing).
 */
export function resolvedThemePolarity(resolved: ResolvedEditorTheme): EditorAppearance {
  if (resolved.define) {
    return resolved.define.base === "vs" || resolved.define.base === "hc-light" ? "light" : "dark";
  }
  return LIGHT_MONACO_THEMES.has(resolved.id) ? "light" : "dark";
}

/** Why the `monaco.theme` picker has no effect right now. */
export type MonacoThemeChoiceBlocker = "editor-appearance" | "vscode-theme";

/**
 * Whether a `monaco.theme` choice would currently be IGNORED, and by which
 * higher-precedence control. `MonacoSettingsPanel` disables the picker and says
 * so instead of painting a stored value as if it were in effect (cases 1 and 2
 * of `resolveEditorTheme`, which outrank case 3).
 */
export function monacoThemeChoiceBlocker({
  appearance,
  theme,
}: Pick<ResolveEditorThemeInput, "appearance" | "theme">): MonacoThemeChoiceBlocker | null {
  if (appearance === "light" || appearance === "dark") return "editor-appearance";
  if (theme && isVSCodeTheme(theme)) return "vscode-theme";
  return null;
}

/** Theme ids `applyMonacoTheme` has defined on the current Monaco instance. */
const definedIds = new Set<string>();

/**
 * Define the resolved theme (once per id — re-defining on every render is
 * pure work and resets Monaco's theme registry) and select it.
 */
export function applyMonacoTheme(resolved: ResolvedEditorTheme, monaco: MonacoThemeApi): void {
  if (resolved.define && !definedIds.has(resolved.id)) {
    monaco.editor.defineTheme(resolved.id, {
      base: resolved.define.base as MonacoBase,
      // A converted VS Code theme may omit keys Monaco needs; inheriting from
      // its own polarity base keeps the editor readable instead of falling
      // back to unthemed defaults.
      inherit: true,
      rules: resolved.define.rules ?? [],
      colors: resolved.define.colors ?? {},
    });
    definedIds.add(resolved.id);
  }
  monaco.editor.setTheme(resolved.id);
}

/** Test hook: forget which ids were defined (a fresh Monaco registry). */
export function __resetMonacoThemeForTests(): void {
  definedIds.clear();
}
