import { useSyncExternalStore } from "react";

/**
 * editorAppearance — the light/dark override for the file editor (Monaco) and
 * its rendered preview pane.
 *
 * WHY a separate preference: the app's global palette comes from the *terminal*
 * theme (`useTheme` → `applyThemeColors`), which is a single app-wide choice.
 * Someone authoring a README, or eyeballing a rendered HTML/Markdown document,
 * often wants to see it on the opposite polarity without re-theming the whole
 * chrome. This preference is that override — it never touches
 * `document.documentElement`; consumers scope the palette with a wrapper class.
 *
 * Persistence: one GLOBAL localStorage key (`ocode.ui.editorAppearance.v1`) —
 * unlike "show hidden files" or per-file viewer state, the appearance is a
 * reading/workflow preference, not a property of a project or a document.
 *
 * Three stored values (spec §8.6):
 *
 * - `"auto"` (the default) — the editor follows the active colour theme's
 *   converted MONACO palette (a `vscode:` theme supplies one; a builtin theme
 *   is outranked only by `monaco.theme`, see `lib/monacoTheme.ts`). The preview
 *   pane takes the polarity of that RESOLVED editor palette, not the colour
 *   theme's declared polarity, so a light `monaco.theme` under a dark colour
 *   theme still renders a light preview.
 * - `"light"` / `"dark"` — an EXPLICIT override. It outranks the colour theme
 *   (`ocode-light` / `ocode-dark`) and Settings tells the user it is on, with a
 *   *Use theme colours* action that writes `"auto"` back.
 *
 * Migration: an ABSENT or unparsable stored value reads as `"auto"`, so an
 * existing install stops being coerced to a polarity of the editor's own and
 * instead gets the active theme's palette. An explicitly stored `light`/`dark`
 * keeps its meaning. `resolveEffectiveEditorAppearance` turns the stored value
 * into the light/dark polarity the preview chrome needs.
 *
 * Cross-tab sync uses the `storage` event (same-tab components all read this
 * one module store through `useEditorAppearance`, so no custom event is
 * needed).
 */

export const EDITOR_APPEARANCE_STORAGE_KEY = "ocode.ui.editorAppearance.v1";

/** The EFFECTIVE (light/dark) palette polarity. Preview viewers take this. */
export type EditorAppearance = "light" | "dark";

/** The persisted preference: an explicit override, or `"auto"` to follow the
 *  active colour theme. */
export type EditorAppearanceValue = "auto" | EditorAppearance;

/** The stored default: an absent/unparsable preference means `"auto"`. */
export const DEFAULT_EDITOR_APPEARANCE: EditorAppearanceValue = "auto";

const listeners = new Set<() => void>();
/** Cached resolved value. `null` means "not resolved yet". Caching keeps the
 *  `useSyncExternalStore` snapshot referentially stable. */
let current: EditorAppearanceValue | null = null;

/** Strictly validate a raw stored value: anything that is not exactly
 *  "auto"/"light"/"dark" is treated as unset so a corrupt or foreign value can
 *  never select a non-existent palette. */
function normalize(raw: string | null | undefined): EditorAppearanceValue | null {
  return raw === "auto" || raw === "light" || raw === "dark" ? raw : null;
}

function readStored(): EditorAppearanceValue | null {
  try {
    return normalize(window.localStorage.getItem(EDITOR_APPEARANCE_STORAGE_KEY));
  } catch (err) {
    // localStorage can throw (disabled cookies, quota, opaque origin).
    console.warn("[editorAppearance] failed to read preference", err);
    return null;
  }
}

function emit(): void {
  for (const l of listeners) l();
}

/** The stored preference (`"auto"` when never set or unparsable). */
export function getEditorAppearance(): EditorAppearanceValue {
  if (current === null) current = readStored() ?? DEFAULT_EDITOR_APPEARANCE;
  return current;
}

/**
 * Resolve the stored preference to the light/dark polarity everything visually
 * scoped by it uses (`editorAppearanceClass`, the toggle label, Monaco).
 *
 * `"auto"` takes the polarity of the editor's RESOLVED palette, so a light
 * `monaco.theme` — or a light VS Code theme — yields a light preview/gutter
 * without the user touching the toggle. The editor surfaces get both inputs
 * from `useResolvedEditorTheme()`, which is the only production caller: it
 * passes the polarity of `resolveEditorTheme`'s result, so the toggle and the
 * editor can never disagree about which palette is on screen.
 */
export function resolveEffectiveEditorAppearance(
  value: EditorAppearanceValue | null | undefined,
  themePolarity: EditorAppearance,
): EditorAppearance {
  if (value === "light" || value === "dark") return value;
  return themePolarity === "light" ? "light" : "dark";
}

export function setEditorAppearance(next: EditorAppearanceValue): void {
  if (getEditorAppearance() === next) return;
  current = next;
  try {
    window.localStorage.setItem(EDITOR_APPEARANCE_STORAGE_KEY, next);
  } catch (err) {
    // The in-memory value still drives this session; persistence is best-effort.
    console.warn("[editorAppearance] failed to persist preference", err);
  }
  emit();
}

/**
 * Flip light ⇄ dark and persist. `currentEffective` is the value the caller is
 * currently showing (it already resolved `"auto"` against the active theme, or
 * — for the editor surfaces — against the RESOLVED Monaco theme, which can be
 * light under a dark colour theme). It is REQUIRED: a bare call would otherwise
 * have to assume `"auto"` means dark, and a first click from that assumption
 * flips to a palette the user is already looking at.
 */
export function toggleEditorAppearance(currentEffective: EditorAppearance): EditorAppearance {
  const next: EditorAppearance = currentEffective === "light" ? "dark" : "light";
  setEditorAppearance(next);
  return next;
}

function subscribe(onChange: () => void): () => void {
  listeners.add(onChange);
  return () => {
    listeners.delete(onChange);
  };
}

/** React binding — one source of truth for every mounted editor/preview.
 *  Returns the STORED value; resolve with `resolveEffectiveEditorAppearance`. */
export function useEditorAppearance(): EditorAppearanceValue {
  return useSyncExternalStore(subscribe, getEditorAppearance, getEditorAppearance);
}

/** CSS scope class carrying the neutral light/dark preview palette (index.css).
 *  `""` for a caller that should inherit the app theme unchanged. */
export function editorAppearanceClass(appearance: EditorAppearance | undefined): string {
  if (appearance === "light") return "editor-appearance-light";
  if (appearance === "dark") return "editor-appearance-dark";
  return "";
}

/** Test hook: drop the cached value and subscribers so a fresh import (or a
 *  fresh test) re-resolves from storage. */
export function __resetEditorAppearanceForTests(): void {
  current = null;
  listeners.clear();
}

// Cross-tab: another tab flipping the preference updates this one. `storage`
// only fires in *other* documents, which is exactly what we want here.
if (typeof window !== "undefined") {
  window.addEventListener("storage", (e) => {
    if (e.key !== EDITOR_APPEARANCE_STORAGE_KEY) return;
    // A removed key reverts to the default; an unparsable value is ignored.
    const v = e.newValue === null ? DEFAULT_EDITOR_APPEARANCE : normalize(e.newValue);
    if (v && v !== current) {
      current = v;
      emit();
    }
  });
}
