// FileEditor's light/dark toggle lives in the editor header (the split-view
// host suppresses it in favour of the mode toolbar's copy). Monaco itself is
// irrelevant here, so stub the editor and its worker setup.
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@monaco-editor/react", () => ({
  default: (props: { theme?: string }) => (
    <div data-testid="monaco-stub" data-theme={props.theme} />
  ),
}));
vi.mock("../../lib/monaco-setup", () => ({}));
const getAppearanceSettings = vi.fn();
const getAppearanceTheme = vi.fn();
vi.mock("../../api/client", () => ({
  api: {
    getMonacoSettings: vi.fn().mockResolvedValue({
      theme: "ocode-dark",
      font_size: 13,
      tab_size: 2,
      word_wrap: false,
      minimap: false,
      line_numbers: true,
    }),
    getAppearanceSettings: (...args: unknown[]) => getAppearanceSettings(...args),
    getAppearanceTheme: (...args: unknown[]) => getAppearanceTheme(...args),
    // A builtin/opencode theme resolves through the legacy 16-colour endpoint.
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

import FileEditor from "./FileEditor";
import { __resetAppearanceThemeForTests } from "../../lib/appearanceTheme";
import {
  EDITOR_APPEARANCE_STORAGE_KEY,
  __resetEditorAppearanceForTests,
} from "../../lib/editorAppearance";
import type { ConvertedTheme } from "../../api/types";

function convertedTheme(overrides: Partial<ConvertedTheme> = {}): ConvertedTheme {
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

const SETTINGS = {
  color_theme: "tokyonight",
  icon_theme: "",
  extra_extension_dirs: [],
  effective_color_theme: "tokyonight",
};

describe("FileEditor appearance toggle", () => {
  beforeEach(() => {
    window.localStorage.clear();
    __resetEditorAppearanceForTests();
    __resetAppearanceThemeForTests();
    getAppearanceSettings.mockReset();
    getAppearanceTheme.mockReset();
    getAppearanceSettings.mockResolvedValue(SETTINGS);
    getAppearanceTheme.mockResolvedValue(convertedTheme());
  });

  it("toggles and persists the choice", async () => {
    render(<FileEditor path="src/a.ts" content="x" />);
    await waitFor(() => expect(screen.getByTestId("monaco-stub")).toBeInTheDocument());

    const btn = screen.getByRole("button", { name: "Toggle light or dark theme" });
    expect(btn).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(btn);

    expect(window.localStorage.getItem(EDITOR_APPEARANCE_STORAGE_KEY)).toBe("light");
    expect(screen.getByRole("button", { name: "Toggle light or dark theme" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("passes ocode-light theme to Monaco after light toggle", async () => {
    render(<FileEditor path="src/a.ts" content="x" />);
    await waitFor(() => expect(screen.getByTestId("monaco-stub")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Toggle light or dark theme" }));
    const wrapper = screen.getByTestId("monaco-stub").parentElement;
    expect(wrapper).toBeInTheDocument();
    expect(wrapper).toHaveStyle({ backgroundColor: "rgb(255, 255, 255)" });
    expect(screen.getByTestId("monaco-stub")).toHaveAttribute("data-theme", "ocode-light");
  });

  it("uses the active VS Code theme's converted Monaco palette when following the theme", async () => {
    getAppearanceSettings.mockResolvedValue({
      ...SETTINGS,
      color_theme: "vscode:acme.themes/dark",
      effective_color_theme: "vscode:acme.themes/dark",
    });
    getAppearanceTheme.mockResolvedValue(
      convertedTheme({
        id: "vscode:acme.themes/dark",
        monaco: { base: "vs-dark", colors: { "editor.background": "#101010" }, rules: [] },
      }),
    );

    render(<FileEditor path="src/a.ts" content="x" />);

    await waitFor(() =>
      expect(screen.getByTestId("monaco-stub")).toHaveAttribute(
        "data-theme",
        "ocode-vscode:acme.themes/dark",
      ),
    );
  });

  it("an explicit override still beats the VS Code theme", async () => {
    window.localStorage.setItem(EDITOR_APPEARANCE_STORAGE_KEY, "light");
    __resetEditorAppearanceForTests();
    getAppearanceSettings.mockResolvedValue({
      ...SETTINGS,
      color_theme: "vscode:acme.themes/dark",
      effective_color_theme: "vscode:acme.themes/dark",
    });
    getAppearanceTheme.mockResolvedValue(
      convertedTheme({ id: "vscode:acme.themes/dark" }),
    );

    render(<FileEditor path="src/a.ts" content="x" />);

    expect(screen.getByTestId("monaco-stub")).toHaveAttribute("data-theme", "ocode-light");
    await waitFor(() => expect(getAppearanceTheme).toHaveBeenCalled());
  });

  it("hides the toggle when the split host owns it", () => {
    render(<FileEditor path="src/a.ts" content="x" hideAppearanceToggle />);
    expect(screen.queryByRole("button", { name: "Toggle light or dark theme" })).toBeNull();
  });
});
