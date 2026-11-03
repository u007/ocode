import { useEffect, useState } from "react";
import { api } from "@/api/client";
import type {
  AppearanceSettings,
  ConvertedTheme,
  ThemeColors,
  ThemeResponse,
} from "@/api/types";
import {
  AA_NORMAL,
  computeThemeVars,
  contrastRatio,
  hexToHslTriplet,
  pickReadableFg,
} from "@/lib/themeColors";

/**
 * The web chrome palette for one theme. A module store (same shape as
 * `chatVerbosity.ts`) so `App`'s `useTheme`, the Settings picker and any future
 * consumer share one fetch, one applied theme and one last-good value.
 *
 * The server hands back a fully resolved palette: every `ui` value is already a
 * flat `#RRGGBB`, with the theme's alpha composited over its background and its
 * contrast policy applied. The client therefore writes the map nearly verbatim
 * — only the four synthesized-foreground pairs are re-checked (see
 * `applyAppearanceUI`).
 */
export interface AppearanceThemeState {
  theme: ConvertedTheme | null;
  /** The applied theme id: the stored `color_theme`, or the server's effective
   *  fallback when nothing is stored. Empty before the first successful load. */
  colorThemeId: string;
  loading: boolean;
  error: string | null;
}

export interface AppearanceThemeHandle {
  theme: ConvertedTheme | null;
  colorThemeId: string;
  setColorThemeId(id: string): Promise<void>;
}

type Listener = (state: AppearanceThemeState) => void;

const EMPTY_STATE: AppearanceThemeState = {
  theme: null,
  colorThemeId: "",
  loading: false,
  error: null,
};

let state: AppearanceThemeState = { ...EMPTY_STATE };
let settings: AppearanceSettings | null = null;
let settingsInFlight: Promise<AppearanceSettings> | null = null;
const themeCache = new Map<string, ConvertedTheme>();
const themeInFlight = new Map<string, Promise<ConvertedTheme>>();
const listeners = new Set<Listener>();

// Variables this module wrote inline. Tracked so switching to a theme that
// omits one of them reveals the stylesheet default again instead of leaving
// the previous theme's colour in place.
let appliedUI = new Set<string>();

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * The server omits `extra_extension_dirs` when it is empty (a fresh install).
 * Normalise it once here so the store never hands a consumer a null it might
 * `.map()` over.
 */
function normalizeAppearanceSettings(response: AppearanceSettings): AppearanceSettings {
  return {
    ...response,
    extra_extension_dirs: Array.isArray(response.extra_extension_dirs)
      ? response.extra_extension_dirs
      : [],
  };
}

function publish(next: AppearanceThemeState): AppearanceThemeState {
  state = next;
  for (const listener of listeners) listener(next);
  return next;
}

function hasUI(theme: ConvertedTheme): boolean {
  return !!theme.ui && Object.keys(theme.ui).length > 0;
}

/**
 * Reject a payload that cannot be applied as a whole. A theme is only useful
 * with its 16-colour palette; skipping a malformed one keeps the currently
 * applied theme instead of painting half a palette over it.
 */
function assertConvertedTheme(theme: unknown, target: string): asserts theme is ConvertedTheme {
  if (!theme || typeof theme !== "object" || Array.isArray(theme)) {
    throw new Error(`appearance theme ${target || "(current)"} is not an object`);
  }
  const candidate = theme as Partial<ConvertedTheme>;
  if (typeof candidate.id !== "string" || candidate.id === "") {
    throw new Error(`appearance theme ${target} has no id`);
  }
  const colors = candidate.colors as Partial<ThemeColors> | undefined;
  if (!colors || typeof colors !== "object" || typeof colors.background !== "string") {
    throw new Error(`appearance theme ${target} has no colour palette`);
  }
  if (candidate.ui !== undefined) {
    if (!candidate.ui || typeof candidate.ui !== "object" || Array.isArray(candidate.ui)) {
      throw new Error(`appearance theme ${target} has an invalid ui map`);
    }
    for (const [name, value] of Object.entries(candidate.ui)) {
      if (typeof value !== "string") {
        throw new Error(`appearance theme ${target} ui value for ${name} is not a string`);
      }
    }
  }
}

/**
 * Wrap the legacy `GET /api/theme` payload (16 colours, no rich palette) in the
 * `ConvertedTheme` shape so the rest of the store has one type. `ui` is empty,
 * which routes the application path through `computeThemeVars`.
 */
function legacyTheme(response: ThemeResponse): ConvertedTheme {
  const background = response.colors?.background ?? "";
  const light =
    contrastRatio(background, "#000000") > contrastRatio(background, "#ffffff");
  return {
    id: response.name,
    label: response.name,
    kind: light ? "light" : "dark",
    colors: response.colors,
    ui: {},
    monaco: { base: light ? "vs" : "vs-dark", colors: {}, rules: [] },
    terminal: {},
  };
}

/**
 * Fetch the stored appearance block once. Concurrent callers share the request;
 * a failure keeps the last-known value so a transient error cannot reset the
 * selection to empty.
 */
export async function loadAppearanceSettings(): Promise<AppearanceSettings> {
  if (settings) return settings;
  if (settingsInFlight) return settingsInFlight;
  settingsInFlight = api
    .getAppearanceSettings()
    .then((response) => {
      settings = normalizeAppearanceSettings(response);
      return settings;
    })
    .catch((error: unknown) => {
      console.warn("[appearance] settings request failed; retaining the last known selection", {
        error,
      });
      if (settings) return settings;
      throw error;
    })
    .finally(() => {
      settingsInFlight = null;
    });
  return settingsInFlight;
}

async function resolveTheme(target: string): Promise<ConvertedTheme> {
  const cached = themeCache.get(target);
  if (cached) return cached;
  const inFlight = themeInFlight.get(target);
  if (inFlight) return inFlight;

  const request = (
    target.startsWith("vscode:")
      ? api.getAppearanceTheme(target)
      : api.getTheme(target || undefined).then(legacyTheme)
  )
    .then((theme) => {
      assertConvertedTheme(theme, target);
      themeCache.set(target, theme);
      return theme;
    })
    .finally(() => {
      themeInFlight.delete(target);
    });

  themeInFlight.set(target, request);
  return request;
}

function clearAppliedUI(): void {
  const root = document.documentElement;
  for (const name of appliedUI) root.style.removeProperty(name);
  appliedUI = new Set();
}

/** The foreground/surface pairs spec §8.2 allows the client to re-check. */
const SYNTHESIZED_FOREGROUND_PAIRS: ReadonlyArray<readonly [string, string]> = [
  ["--primary", "--primary-foreground"],
  ["--destructive", "--destructive-foreground"],
  ["--secondary", "--secondary-foreground"],
  ["--accent", "--accent-foreground"],
];

/**
 * Write one theme variable and record it in `appliedUI`, so switching to a
 * theme that omits the same name can remove it again. A value that cannot be
 * parsed is cleared instead of leaving a previous theme's colour in place.
 */
function writeThemeVar(name: string, hex: string): void {
  const root = document.documentElement;
  const triplet = hexToHslTriplet(hex);
  if (triplet === null) {
    console.warn(`Skipping theme variable ${name}: invalid hex color`, hex);
    if (appliedUI.delete(name)) root.style.removeProperty(name);
    return;
  }
  root.style.setProperty(name, triplet);
  appliedUI.add(name);
}

/**
 * Apply one full variable map as HSL triplets (the form Tailwind consumes) and
 * remove every variable this module applied for a previous theme that the new
 * map omits. BOTH application branches (rich `ui` and the legacy 16-colour
 * mapping) go through here, so `appliedUI` is a complete record of what is
 * inline and a legacy -> rich switch cannot leave legacy-only names painted
 * over the stylesheet default.
 */
function applyVars(vars: Record<string, string>): void {
  const root = document.documentElement;
  for (const name of Array.from(appliedUI)) {
    if (!(name in vars)) {
      root.style.removeProperty(name);
      appliedUI.delete(name);
    }
  }
  for (const [name, hex] of Object.entries(vars)) writeThemeVar(name, hex);
}

/** Apply one resolved theme: rich `ui` palette when present, legacy otherwise. */
function applyTheme(theme: ConvertedTheme): void {
  if (hasUI(theme)) {
    applyAppearanceUI(theme);
    return;
  }
  // The legacy mapping derives all its variables from the 16-colour palette;
  // routing it through applyVars is what records them for a later removal.
  applyVars(computeThemeVars(theme.colors));
}

/**
 * Repair a synthesized foreground that fails WCAG AA on its surface. Only the
 * four foreground slots above are ever touched: every theme-authored surface,
 * border, link and body colour is written verbatim.
 *
 * The server also synthesizes these slots, so this is normally a no-op. It
 * exists because a payload can still arrive with an unreadable pair (an older
 * server, an authored partner foreground the server deliberately preserved,
 * or a future converter change), and unreadable button/selection text is the
 * one failure worth correcting over "author verbatim".
 */
function repairSynthesizedForegrounds(ui: Record<string, string>, text?: string): void {
  const root = document.documentElement;
  for (const [surfaceVar, foregroundVar] of SYNTHESIZED_FOREGROUND_PAIRS) {
    const surface = ui[surfaceVar];
    const foreground = ui[foregroundVar];
    if (!surface || !foreground) continue;
    if (contrastRatio(surface, foreground) >= AA_NORMAL) continue;
    const candidates = [text, ui["--foreground"]].filter(
      (value): value is string => typeof value === "string" && value !== "",
    );
    const triplet = hexToHslTriplet(pickReadableFg(surface, candidates));
    if (triplet !== null) {
      root.style.setProperty(foregroundVar, triplet);
      appliedUI.add(foregroundVar);
    }
  }
}

/**
 * Write the converted theme's `ui` map to the document root. A variable the map
 * omits is left at the stylesheet default in `index.css`; a variable this module
 * applied for a previous theme and the new theme omits is removed for the same
 * reason.
 */
export function applyAppearanceUI(theme: ConvertedTheme): void {
  const ui = theme.ui ?? {};
  applyVars(ui);
  if (hasUI(theme)) repairSynthesizedForegrounds(ui, theme.colors?.text);
}

/**
 * Load the converted theme for `id` (or the configured/effective theme when
 * omitted) and apply it. Failures keep the currently applied theme and are
 * published as `state.error`; the rejection lets an explicit caller surface the
 * problem inline.
 */
export async function loadAppearanceTheme(id?: string): Promise<ConvertedTheme> {
  publish({ ...state, loading: true, error: null });
  try {
    let target = id;
    if (target === undefined) {
      const currentSettings = await loadAppearanceSettings();
      target = currentSettings.effective_color_theme || currentSettings.color_theme;
    }
    const theme = await resolveTheme(target ?? "");
    applyTheme(theme);
    // The APPLIED id, not the stored one: a stored id that no longer resolves
    // falls back to the effective theme, and showing the dead id as "current"
    // would describe a palette the user is not looking at.
    const colorThemeId =
      settings?.effective_color_theme || settings?.color_theme || theme.id || target || "";
    publish({ theme, colorThemeId, loading: false, error: null });
    return theme;
  } catch (error) {
    const message = errorMessage(error);
    console.warn("[appearance] theme load failed; keeping the applied theme", { error });
    publish({ ...state, loading: false, error: message });
    throw error;
  }
}

/** The applied theme id (empty until the first successful load). */
export function getColorThemeId(): string {
  return state.colorThemeId;
}

/**
 * Persist a colour theme and apply it. The settings endpoint replaces the whole
 * appearance block, so the current icon theme and extra roots are sent back
 * unchanged; the apply happens only after the PUT succeeds, so a rejected write
 * never changes what is on screen.
 */
export async function setColorThemeId(id: string): Promise<void> {
  const current = await loadAppearanceSettings();
  const response = await api.setAppearanceSettings({
    color_theme: id,
    icon_theme: current.icon_theme,
    extra_extension_dirs: current.extra_extension_dirs ?? [],
  });
  settings = normalizeAppearanceSettings(response);
  const target = response.effective_color_theme || id;
  publish({
    ...state,
    colorThemeId: response.effective_color_theme || response.color_theme || target,
  });
  await loadAppearanceTheme(target);
}

/**
 * Subscribe to the appearance store. Mounts a load when no theme is applied
 * yet; the load is shared with every other consumer and with `useTheme`.
 */
export function useAppearanceTheme(): AppearanceThemeHandle {
  const [snapshot, setSnapshot] = useState<AppearanceThemeState>(() => state);

  useEffect(() => {
    const listener: Listener = (next) => setSnapshot(next);
    listeners.add(listener);
    if (state.theme === null && !state.loading) {
      void loadAppearanceTheme().catch(() => {
        // Published as state.error; the stylesheet defaults stay in place.
      });
    }
    return () => {
      listeners.delete(listener);
    };
  }, []);

  return {
    theme: snapshot.theme,
    colorThemeId: snapshot.colorThemeId,
    setColorThemeId,
  };
}

/** Test-only: drop every cache and remove the variables this module applied. */
export function __resetAppearanceThemeForTests(): void {
  clearAppliedUI();
  state = { ...EMPTY_STATE };
  settings = null;
  settingsInFlight = null;
  themeCache.clear();
  themeInFlight.clear();
  listeners.clear();
}
