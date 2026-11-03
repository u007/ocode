import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ConvertedTheme } from "../api/types";

const getAppearanceSettings = vi.fn();
const getAppearanceTheme = vi.fn();
vi.mock("../api/client", () => ({
  api: {
    getAppearanceSettings: (...args: unknown[]) => getAppearanceSettings(...args),
    getAppearanceTheme: (...args: unknown[]) => getAppearanceTheme(...args),
    getTheme: vi.fn(),
  },
}));

import {
  DEFAULT_PERSISTED_MONACO_THEME,
  getPersistedMonacoTheme,
  setPersistedMonacoTheme,
  useResolvedEditorTheme,
  __resetResolvedEditorThemeForTests,
} from "./useResolvedEditorTheme";
import { __resetAppearanceThemeForTests } from "../lib/appearanceTheme";
import {
  EDITOR_APPEARANCE_STORAGE_KEY,
  setEditorAppearance,
  __resetEditorAppearanceForTests,
} from "../lib/editorAppearance";

function converted(overrides: Partial<ConvertedTheme> = {}): ConvertedTheme {
  return {
    id: "tokyonight",
    label: "Tokyonight",
    kind: "dark",
    colors: {
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
    },
    ui: {},
    monaco: { base: "vs-dark", colors: {}, rules: [] },
    terminal: {},
    ...overrides,
  };
}

const LIGHT_VSCODE = converted({
  id: "vscode:acme.themes/light",
  label: "Acme Light",
  kind: "light",
  monaco: { base: "vs", colors: {}, rules: [] },
});

function settings(id: string) {
  return {
    color_theme: id,
    icon_theme: "",
    extra_extension_dirs: [],
    effective_color_theme: id,
  };
}

describe("useResolvedEditorTheme", () => {
  beforeEach(() => {
    window.localStorage.clear();
    __resetEditorAppearanceForTests();
    __resetAppearanceThemeForTests();
    __resetResolvedEditorThemeForTests();
    getAppearanceSettings.mockReset();
    getAppearanceTheme.mockReset();
    // Default: the theme store is empty (offline / 401 / slow fetch).
    getAppearanceSettings.mockRejectedValue(new Error("offline"));
  });

  it("starts on the dark defaults", () => {
    const { result } = renderHook(() => useResolvedEditorTheme());
    expect(getPersistedMonacoTheme()).toBe(DEFAULT_PERSISTED_MONACO_THEME);
    expect(result.current.resolved).toEqual({ id: "ocode-dark", define: null });
    expect(result.current.effectiveAppearance).toBe("dark");
    expect(result.current.isLightSurface).toBe(false);
  });

  it("honours a light monaco.theme even with no loaded colour theme", () => {
    // Fail-state regression: the appearance fetch never resolves, so the
    // editor keeps the user's chosen palette instead of dropping it.
    setPersistedMonacoTheme("vs");
    const { result } = renderHook(() => useResolvedEditorTheme());
    expect(result.current.resolved).toEqual({ id: "vs", define: null });
    expect(result.current.effectiveAppearance).toBe("light");
    expect(result.current.isLightSurface).toBe(true);
  });

  it("keeps one notion of polarity: a light monaco.theme under a dark colour theme is light", async () => {
    getAppearanceSettings.mockResolvedValue(settings("tokyonight"));
    getAppearanceTheme.mockResolvedValue(converted());
    setPersistedMonacoTheme("vs");

    const { result } = renderHook(() => useResolvedEditorTheme());

    await waitFor(() => expect(result.current.resolved.id).toBe("vs"));
    // The colour theme is dark, but the editor is light — the toggle must say so.
    expect(result.current.effectiveAppearance).toBe("light");
    expect(result.current.isLightSurface).toBe(true);
  });

  it("lets an explicit override beat both the theme and monaco.theme", async () => {
    getAppearanceSettings.mockResolvedValue(settings("vscode:acme.themes/light"));
    getAppearanceTheme.mockResolvedValue(LIGHT_VSCODE);
    setPersistedMonacoTheme("vs");
    setEditorAppearance("dark");

    const { result } = renderHook(() => useResolvedEditorTheme());

    await waitFor(() => expect(result.current.resolved).toEqual({ id: "ocode-dark", define: null }));
    expect(result.current.effectiveAppearance).toBe("dark");
    expect(result.current.isLightSurface).toBe(false);
  });

  it("re-renders every subscriber when the persisted setting arrives", () => {
    const a = renderHook(() => useResolvedEditorTheme());
    const b = renderHook(() => useResolvedEditorTheme());
    expect(a.result.current.resolved.id).toBe("ocode-dark");
    expect(b.result.current.resolved.id).toBe("ocode-dark");

    act(() => {
      setPersistedMonacoTheme("ocode-light");
    });

    expect(a.result.current.resolved.id).toBe("ocode-light");
    expect(b.result.current.resolved.id).toBe("ocode-light");
    expect(b.result.current.effectiveAppearance).toBe("light");
  });

  it("treats an empty persisted value as the default", () => {
    setPersistedMonacoTheme("");
    expect(getPersistedMonacoTheme()).toBe(DEFAULT_PERSISTED_MONACO_THEME);
  });

  it("keeps the stored preference readable across a reset", () => {
    window.localStorage.setItem(EDITOR_APPEARANCE_STORAGE_KEY, "light");
    __resetEditorAppearanceForTests();
    const { result } = renderHook(() => useResolvedEditorTheme());
    expect(result.current.effectiveAppearance).toBe("light");
    expect(result.current.isLightSurface).toBe(true);
  });
});
