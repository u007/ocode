import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  AppearanceSettings,
  ConvertedTheme,
  ThemeColors,
  ThemeResponse,
} from "@/api/types";
import {
  __resetAppearanceThemeForTests,
  applyAppearanceUI,
  getColorThemeId,
  loadAppearanceTheme,
  setColorThemeId,
} from "./appearanceTheme";
import { computeThemeVars, hexToHslTriplet } from "./themeColors";

const getAppearanceThemes = vi.fn();
const getAppearanceTheme = vi.fn();
const getAppearanceSettings = vi.fn();
const getTheme = vi.fn();
const setAppearanceSettings = vi.fn();

vi.mock("@/api/client", () => ({
  api: {
    getAppearanceThemes: (...args: unknown[]) => getAppearanceThemes(...args),
    getAppearanceTheme: (...args: unknown[]) => getAppearanceTheme(...args),
    getAppearanceSettings: (...args: unknown[]) => getAppearanceSettings(...args),
    getTheme: (...args: unknown[]) => getTheme(...args),
    setAppearanceSettings: (...args: unknown[]) => setAppearanceSettings(...args),
  },
}));

const COLORS: ThemeColors = {
  user: "#7aa2f7",
  assistant: "#a9b1d6",
  header: "#7aa2f7",
  border: "#3b4261",
  hint: "#565f89",
  text: "#c0caf5",
  background: "#1a1b26",
  status_bg: "#16161e",
  status_fg: "#c0caf5",
  selected_fg: "#c0caf5",
  selected_bg: "#283457",
  success: "#9ece6a",
  error: "#f7768e",
  accent: "#bb9af7",
  dim: "#16161e",
  thinking: "#565f89",
};

function settings(overrides: Partial<AppearanceSettings> = {}): AppearanceSettings {
  return {
    color_theme: "vscode:acme.themes/dark",
    icon_theme: "",
    extra_extension_dirs: [],
    effective_color_theme: "vscode:acme.themes/dark",
    ...overrides,
  };
}

function convertedTheme(overrides: Partial<ConvertedTheme> = {}): ConvertedTheme {
  return {
    id: "vscode:acme.themes/dark",
    label: "Acme Dark",
    kind: "dark",
    colors: COLORS,
    ui: { "--background": "#1e1e2e" },
    monaco: { base: "vs-dark", colors: {}, rules: [] },
    terminal: {},
    ...overrides,
  };
}

function rootVar(name: string): string {
  return document.documentElement.style.getPropertyValue(name);
}

describe("appearance theme store", () => {
  beforeEach(() => {
    __resetAppearanceThemeForTests();
    document.documentElement.removeAttribute("style");
    vi.clearAllMocks();
    getAppearanceSettings.mockResolvedValue(settings());
  });

  afterEach(() => {
    __resetAppearanceThemeForTests();
    document.documentElement.removeAttribute("style");
  });

  it("applies the theme's ui map verbatim", async () => {
    getAppearanceTheme.mockResolvedValue(
      convertedTheme({ ui: { "--background": "#1e1e2e" } }),
    );

    await loadAppearanceTheme();

    expect(getAppearanceTheme).toHaveBeenCalledWith("vscode:acme.themes/dark");
    // #1e1e2e converts to this exact HSL triplet; no client-side repaint.
    expect(rootVar("--background")).toBe("240 21.1% 14.9%");
    expect(getColorThemeId()).toBe("vscode:acme.themes/dark");
  });

  it("leaves a missing ui key at the stylesheet default", async () => {
    getAppearanceTheme.mockResolvedValue(
      convertedTheme({ ui: { "--background": "#1e1e2e" } }),
    );

    await loadAppearanceTheme();

    // --accent was never in the payload, so the inline property is untouched
    // and index.css's :root default is what renders.
    expect(rootVar("--accent")).toBe("");
  });

  it("clears legacy variables a later rich theme omits", async () => {
    // Apply the legacy 16-colour palette first: it writes its whole mapping
    // inline. Only a recorded application path can remove it again.
    getAppearanceSettings.mockResolvedValue(
      settings({ color_theme: "tokyonight", effective_color_theme: "tokyonight" }),
    );
    getTheme.mockResolvedValue({ name: "tokyonight", colors: COLORS } as ThemeResponse);
    await loadAppearanceTheme();
    expect(rootVar("--accent")).not.toBe("");
    expect(rootVar("--primary")).not.toBe("");
    expect(rootVar("--border")).not.toBe("");

    // A rich theme that supplies only one key must reveal the stylesheet
    // default for everything the legacy palette had painted.
    getAppearanceTheme.mockResolvedValue(
      convertedTheme({ ui: { "--background": "#1e1e2e" } }),
    );
    await loadAppearanceTheme("vscode:acme.themes/dark");

    expect(rootVar("--background")).toBe("240 21.1% 14.9%");
    expect(rootVar("--accent")).toBe("");
    expect(rootVar("--primary")).toBe("");
    expect(rootVar("--border")).toBe("");
  });

  it("drops a variable the previous theme applied but the new one omits", async () => {
    getAppearanceTheme
      .mockResolvedValueOnce(
        convertedTheme({
          ui: { "--background": "#1e1e2e", "--accent": "#cba6f7" },
        }),
      )
      .mockResolvedValueOnce(
        convertedTheme({
          id: "vscode:acme.themes/plain",
          ui: { "--background": "#eff1f5" },
        }),
      );

    await loadAppearanceTheme();
    expect(rootVar("--accent")).not.toBe("");

    await loadAppearanceTheme("vscode:acme.themes/plain");
    expect(rootVar("--accent")).toBe("");
    expect(rootVar("--background")).toBe("220 23.1% 94.9%");
  });

  it("repairs only a synthesized foreground that fails AA", async () => {
    getAppearanceTheme.mockResolvedValue(
      convertedTheme({
        ui: {
          "--background": "#000000",
          "--foreground": "#ffffff",
          "--primary": "#ffffff",
          "--primary-foreground": "#ffffff",
          // Authored, but not one of the synthesized-foreground slots: it must
          // stay exactly as the theme/convertor emitted it, even though it is
          // unreadable on the page background.
          "--muted-foreground": "#111111",
        },
      }),
    );

    await loadAppearanceTheme();

    // White on white fails AA, so the surface's foreground is replaced.
    expect(rootVar("--primary-foreground")).toBe("0 0% 0%");
    // Not a pair foreground: written verbatim.
    expect(rootVar("--muted-foreground")).toBe("0 0% 6.7%");
  });

  it("persists the picked theme through setAppearanceSettings", async () => {
    getAppearanceSettings.mockResolvedValue(
      settings({
        icon_theme: "vscode:icons.pack/x",
        extra_extension_dirs: ["/tmp/ext"],
      }),
    );
    setAppearanceSettings.mockResolvedValue(
      settings({
        color_theme: "vscode:new.theme/dark",
        effective_color_theme: "vscode:new.theme/dark",
        icon_theme: "vscode:icons.pack/x",
        extra_extension_dirs: ["/tmp/ext"],
      }),
    );
    getAppearanceTheme.mockResolvedValue(
      convertedTheme({
        id: "vscode:new.theme/dark",
        ui: { "--background": "#101010" },
      }),
    );

    await setColorThemeId("vscode:new.theme/dark");

    // A colour-only write must not clear the icon theme or extra roots.
    expect(setAppearanceSettings).toHaveBeenCalledWith({
      color_theme: "vscode:new.theme/dark",
      icon_theme: "vscode:icons.pack/x",
      extra_extension_dirs: ["/tmp/ext"],
    });
    expect(getAppearanceTheme).toHaveBeenCalledWith("vscode:new.theme/dark");
    expect(rootVar("--background")).toBe("0 0% 6.3%");
    expect(getColorThemeId()).toBe("vscode:new.theme/dark");
  });

  it("surfaces a PUT failure and keeps the applied theme", async () => {
    getAppearanceTheme.mockResolvedValue(
      convertedTheme({ ui: { "--background": "#1e1e2e" } }),
    );
    await loadAppearanceTheme();
    const applied = rootVar("--background");

    setAppearanceSettings.mockRejectedValue(new Error("save failed"));

    await expect(setColorThemeId("vscode:other/theme")).rejects.toThrow("save failed");
    expect(rootVar("--background")).toBe(applied);
    expect(getColorThemeId()).toBe("vscode:acme.themes/dark");
    expect(getAppearanceTheme).not.toHaveBeenCalledWith("vscode:other/theme");
  });

  it("falls back to the legacy 16-colour path when ui is absent", async () => {
    getAppearanceSettings.mockResolvedValue(
      settings({ color_theme: "tokyonight", effective_color_theme: "tokyonight" }),
    );
    getTheme.mockResolvedValue({ name: "tokyonight", colors: COLORS } as ThemeResponse);

    await loadAppearanceTheme();

    expect(getAppearanceTheme).not.toHaveBeenCalled();
    expect(getTheme).toHaveBeenCalledWith("tokyonight");
    const expected = computeThemeVars(COLORS);
    expect(rootVar("--background")).toBe(hexToHslTriplet(expected["--background"]));
    // The legacy path writes the full mapping, not only the background.
    expect(rootVar("--primary")).toBe(hexToHslTriplet(expected["--primary"]));
    expect(getColorThemeId()).toBe("tokyonight");
  });

  it("keeps the last-good theme when a payload is malformed", async () => {
    getAppearanceTheme.mockResolvedValueOnce(
      convertedTheme({ ui: { "--background": "#1e1e2e" } }),
    );
    await loadAppearanceTheme();
    const applied = rootVar("--background");

    // No colour palette: applying half of it would leave a broken UI.
    getAppearanceTheme.mockResolvedValueOnce({ id: "vscode:acme.themes/broken" });
    await expect(loadAppearanceTheme("vscode:acme.themes/broken")).rejects.toThrow(
      /colour palette/,
    );

    expect(rootVar("--background")).toBe(applied);
  });

  it("writes only keys the ui map supplies", () => {
    applyAppearanceUI(convertedTheme({ ui: { "--border": "#313244" } }));
    expect(rootVar("--border")).toBe("236.8 16.2% 22.9%");
    expect(rootVar("--background")).toBe("");
  });
});
