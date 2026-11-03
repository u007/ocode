import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import ThemeForm from "./ThemeForm";
import { __resetAppearanceThemeForTests } from "../../lib/appearanceTheme";
import type {
  AppearanceSettings,
  AppearanceThemesResponse,
  ConvertedTheme,
  ThemeRef,
} from "../../api/types";

const getAppearanceThemes = vi.fn();
const getAppearanceSettings = vi.fn();
const setAppearanceSettings = vi.fn();
const getAppearanceTheme = vi.fn();
const getTheme = vi.fn();

vi.mock("../../api/client", () => ({
  api: {
    getAppearanceThemes: (...args: unknown[]) => getAppearanceThemes(...args),
    getAppearanceSettings: (...args: unknown[]) => getAppearanceSettings(...args),
    setAppearanceSettings: (...args: unknown[]) => setAppearanceSettings(...args),
    getAppearanceTheme: (...args: unknown[]) => getAppearanceTheme(...args),
    getTheme: (...args: unknown[]) => getTheme(...args),
  },
}));

const THEMES: ThemeRef[] = [
  {
    id: "tokyonight",
    label: "Tokyonight",
    source: "builtin",
    kind: "dark",
    has_icon_theme: false,
  },
  {
    id: "vscode:acme.themes/dark",
    label: "Acme Dark",
    source: "vscode",
    kind: "dark",
    extension: "acme.themes",
    has_icon_theme: false,
  },
];

const THEME_LIST: AppearanceThemesResponse = {
  themes: THEMES,
  total: THEMES.length,
  offset: 0,
  limit: 1000,
};

const SETTINGS: AppearanceSettings = {
  color_theme: "tokyonight",
  icon_theme: "vscode:icons.pack/x",
  extra_extension_dirs: ["/tmp/ext"],
  effective_color_theme: "tokyonight",
};

function convertedTheme(id: string): ConvertedTheme {
  return {
    id,
    label: id,
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
    ui: { "--background": "#1e1e2e" },
    monaco: { base: "vs-dark", colors: {}, rules: [] },
    terminal: {},
  };
}

describe("ThemeForm", () => {
  beforeEach(() => {
    __resetAppearanceThemeForTests();
    document.documentElement.removeAttribute("style");
    vi.clearAllMocks();
    getAppearanceThemes.mockResolvedValue(THEME_LIST);
    getAppearanceSettings.mockResolvedValue(SETTINGS);
  });

  afterEach(() => {
    __resetAppearanceThemeForTests();
    document.documentElement.removeAttribute("style");
  });

  it("lists both theme groups with source badges and shows the current id", async () => {
    render(<ThemeForm />);

    expect(await screen.findByText("Acme Dark")).toBeInTheDocument();
    expect(screen.getByText("Tokyonight")).toBeInTheDocument();
    expect(screen.getByText("VS Code themes")).toBeInTheDocument();
    expect(screen.getByText("VS Code")).toBeInTheDocument();
    expect(screen.getByText("Built-in")).toBeInTheDocument();
    expect(screen.getByText("tokyonight", { selector: "code" })).toBeInTheDocument();
  });

  it("persists the picked theme and updates the shown id", async () => {
    setAppearanceSettings.mockResolvedValue({
      ...SETTINGS,
      color_theme: "vscode:acme.themes/dark",
      effective_color_theme: "vscode:acme.themes/dark",
    });
    getAppearanceTheme.mockResolvedValue(convertedTheme("vscode:acme.themes/dark"));

    render(<ThemeForm />);

    fireEvent.click(await screen.findByRole("button", { name: /Acme Dark/ }));

    await waitFor(() =>
      expect(setAppearanceSettings).toHaveBeenCalledWith({
        color_theme: "vscode:acme.themes/dark",
        icon_theme: "vscode:icons.pack/x",
        extra_extension_dirs: ["/tmp/ext"],
      }),
    );
    await waitFor(() =>
      expect(
        screen.getByText("vscode:acme.themes/dark", { selector: "code" }),
      ).toBeInTheDocument(),
    );
    // Applied only after the write succeeded.
    expect(document.documentElement.style.getPropertyValue("--background")).toBe(
      "240 21.1% 14.9%",
    );
  });

  it("surfaces a failed save inline and keeps the current theme", async () => {
    setAppearanceSettings.mockRejectedValue(new Error("save failed"));

    render(<ThemeForm />);

    fireEvent.click(await screen.findByRole("button", { name: /Acme Dark/ }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("vscode:acme.themes/dark");
    expect(alert.textContent).toContain("save failed");
    // The stored/applied selection did not move.
    expect(screen.getByText("tokyonight", { selector: "code" })).toBeInTheDocument();
    expect(document.documentElement.style.getPropertyValue("--background")).toBe("");
  });
});
