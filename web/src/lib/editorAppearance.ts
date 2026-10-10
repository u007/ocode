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
 * Default when unset: the app's own polarity, read once from the computed
 * `--background` lightness. A user on a light terminal theme therefore gets a
 * light preview by default instead of a jarring dark pane; the moment they
 * toggle, an explicit value is stored and the default stops applying.
 *
 * Cross-tab sync uses the `storage` event (same-tab components all read this
 * one module store through `useEditorAppearance`, so no custom event is
 * needed).
 */

export const EDITOR_APPEARANCE_STORAGE_KEY = "ocode.ui.editorAppearance.v1";

export type EditorAppearance = "light" | "dark";

const listeners = new Set<() => void>();
/** Cached resolved value. `null` means "not resolved yet". Caching keeps the
 *  `useSyncExternalStore` snapshot referentially stable and avoids re-reading
 *  computed style on every render. */
let current: EditorAppearance | null = null;

/** Strictly validate a raw stored value: anything that is not exactly
 *  "light"/"dark" is treated as unset so a corrupt or foreign value can never
 *  select a non-existent palette. */
function normalize(raw: string | null | undefined): EditorAppearance | null {
  return raw === "light" || raw === "dark" ? raw : null;
}

function readStored(): EditorAppearance | null {
  try {
    return normalize(window.localStorage.getItem(EDITOR_APPEARANCE_STORAGE_KEY));
  } catch (err) {
    // localStorage can throw (disabled cookies, quota, opaque origin).
    console.warn("[editorAppearance] failed to read preference", err);
    return null;
  }
}

/**
 * The app's current polarity from the computed `--background` HSL triplet
 * ("H S% L%"). Falls back to "dark" when the value is absent (e.g. jsdom, or a
 * read before the theme is applied) — the app's own `:root` default is dark.
 */
function defaultAppearance(): EditorAppearance {
  try {
    const bg = getComputedStyle(document.documentElement).getPropertyValue("--background").trim();
    const m = /(\d+(?:\.\d+)?)%\s*$/.exec(bg);
    if (m) return Number.parseFloat(m[1]) > 50 ? "light" : "dark";
  } catch (err) {
    console.warn("[editorAppearance] failed to detect app polarity", err);
  }
  return "dark";
}

function emit(): void {
  for (const l of listeners) l();
}

/** Current appearance (resolved from storage, then the app polarity). */
export function getEditorAppearance(): EditorAppearance {
  if (current === null) current = readStored() ?? defaultAppearance();
  return current;
}

export function setEditorAppearance(next: EditorAppearance): void {
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

/** Flip light ⇄ dark and persist. Returns the new value. */
export function toggleEditorAppearance(): EditorAppearance {
  const next: EditorAppearance = getEditorAppearance() === "light" ? "dark" : "light";
  setEditorAppearance(next);
  return next;
}

function subscribe(onChange: () => void): () => void {
  listeners.add(onChange);
  return () => {
    listeners.delete(onChange);
  };
}

/** React binding — one source of truth for every mounted editor/preview. */
export function useEditorAppearance(): EditorAppearance {
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
    const v = normalize(e.newValue);
    if (v && v !== current) {
      current = v;
      emit();
    }
  });
}
