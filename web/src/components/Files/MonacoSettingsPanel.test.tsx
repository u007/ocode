import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ConvertedTheme } from "../../api/types";

const getMonacoSettings = vi.fn();
const setMonacoSettings = vi.fn();
const getAppearanceSettings = vi.fn();
const getAppearanceTheme = vi.fn();
vi.mock("../../api/client", () => ({
  api: {
    getMonacoSettings: (...args: unknown[]) => getMonacoSettings(...args),
    setMonacoSettings: (...args: unknown[]) => setMonacoSettings(...args),
    listMonacoExtensions: vi.fn().mockResolvedValue([]),
    toggleMonacoExtension: vi.fn().mockResolvedValue([]),
    getAppearanceSettings: (...args: unknown[]) => getAppearanceSettings(...args),
    getAppearanceTheme: (...args: unknown[]) => getAppearanceTheme(...args),
    // Builtin/opencode themes resolve through the legacy 16-colour endpoint.
    getTheme: vi.fn().mockResolvedValue({
      name: "tokyonight",
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
    }),
  },
}));

import MonacoSettingsPanel from "./MonacoSettingsPanel";
import { __resetAppearanceThemeForTests } from "../../lib/appearanceTheme";
import {
  EDITOR_APPEARANCE_STORAGE_KEY,
  __resetEditorAppearanceForTests,
} from "../../lib/editorAppearance";

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

function settingsFor(id: string) {
  return {
    color_theme: id,
    icon_theme: "",
    extra_extension_dirs: [],
    effective_color_theme: id,
  };
}

const STORED_SETTINGS = {
  theme: "ocode-dark",
  font_size: 13,
  tab_size: 2,
  word_wrap: false,
  minimap: false,
  line_numbers: true,
};

const THEME_NAMES = ["Ocode Dark", "VS Dark", "VS Light", "High Contrast"];

describe("MonacoSettingsPanel theme picker", () => {
  beforeEach(() => {
    window.localStorage.clear();
    __resetEditorAppearanceForTests();
    __resetAppearanceThemeForTests();
    getMonacoSettings.mockReset();
    setMonacoSettings.mockReset();
    getAppearanceSettings.mockReset();
    getAppearanceTheme.mockReset();
    getMonacoSettings.mockResolvedValue({ ...STORED_SETTINGS });
    setMonacoSettings.mockResolvedValue({ ...STORED_SETTINGS });
    // Builtin/opencode theme with the default "auto" preference: `monaco.theme`
    // is the control that decides the palette, so the picker must work.
    getAppearanceSettings.mockResolvedValue(settingsFor("tokyonight"));
    getAppearanceTheme.mockResolvedValue(converted());
  });

  function themeButton(name: string): HTMLButtonElement {
    return screen.getByRole("button", { name }) as HTMLButtonElement;
  }

  it("keeps the picker live and saves a new theme when monaco.theme decides", async () => {
    render(<MonacoSettingsPanel />);
    await waitFor(() => expect(themeButton("Ocode Dark")).toBeTruthy());

    for (const name of THEME_NAMES) expect(themeButton(name)).toBeEnabled();
    expect(screen.queryByRole("status")).toBeNull();

    fireEvent.click(themeButton("VS Dark"));
    fireEvent.click(screen.getByRole("button", { name: "Save Settings" }));

    await waitFor(() =>
      expect(setMonacoSettings).toHaveBeenCalledWith(expect.objectContaining({ theme: "vs-dark" })),
    );
  });

  it("disables the picker and says so under an explicit editor override", async () => {
    // The override (case 1) outranks `monaco.theme`, so painting a stored value
    // as active would misdescribe what the editor shows.
    window.localStorage.setItem(EDITOR_APPEARANCE_STORAGE_KEY, "light");
    __resetEditorAppearanceForTests();

    render(<MonacoSettingsPanel />);
    await waitFor(() => expect(themeButton("Ocode Dark")).toBeTruthy());

    for (const name of THEME_NAMES) expect(themeButton(name)).toBeDisabled();
    expect(screen.getByRole("status")).toHaveTextContent(/light\/dark override is on/i);
  });

  it("disables the picker and says so while a VS Code theme supplies the colours", async () => {
    getAppearanceSettings.mockResolvedValue(settingsFor("vscode:acme.themes/light"));
    getAppearanceTheme.mockResolvedValue(
      converted({ id: "vscode:acme.themes/light", kind: "light", monaco: { base: "vs", colors: {}, rules: [] } }),
    );

    render(<MonacoSettingsPanel />);

    await waitFor(() =>
      expect(screen.getByRole("status")).toHaveTextContent(/VS Code theme supplies the editor colours/i),
    );
    for (const name of THEME_NAMES) expect(themeButton(name)).toBeDisabled();
  });
});
