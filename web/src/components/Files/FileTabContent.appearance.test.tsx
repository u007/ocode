import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";

// FileEditor pulls Monaco (unresolvable in vitest); PreviewSurface pulls every
// heavy viewer. Both are captured as stubs so this test only covers the
// toolbar toggle and the props FileTabContent forwards.
vi.mock("./FileEditor", () => ({
  default: (props: { hideAppearanceToggle?: boolean }) => (
    <div data-testid="editor" data-hide={String(!!props.hideAppearanceToggle)} />
  ),
}));
vi.mock("../Preview/PreviewSurface", () => ({
  default: (props: { appearance?: string }) => (
    <div data-testid="preview" data-appearance={props.appearance ?? "unset"} />
  ),
}));
const getAppearanceSettings = vi.fn();
const getAppearanceTheme = vi.fn();
vi.mock("../../api/client", () => ({
  api: {
    getAppearanceSettings: (...args: unknown[]) => getAppearanceSettings(...args),
    getAppearanceTheme: (...args: unknown[]) => getAppearanceTheme(...args),
  },
}));

import FileTabContent from "./FileTabContent";
import { __resetAppearanceThemeForTests } from "../../lib/appearanceTheme";
import {
  EDITOR_APPEARANCE_STORAGE_KEY,
  __resetEditorAppearanceForTests,
} from "../../lib/editorAppearance";

describe("FileTabContent appearance toggle", () => {
  beforeEach(() => {
    window.localStorage.clear();
    __resetEditorAppearanceForTests();
    __resetAppearanceThemeForTests();
    getAppearanceSettings.mockReset();
    getAppearanceTheme.mockReset();
    // No theme loaded by default: "auto" resolves to the dark fallback.
    getAppearanceSettings.mockRejectedValue(new Error("offline"));
  });

  it("shows the toggle in the split-view toolbar, hides it in the editor header, and toggles both panes", async () => {
    render(<FileTabContent path="README.md" content="# Hi" />);

    // The lazy editor resolves to the stub.
    const editor = await screen.findByTestId("editor");
    // The toggle lives in the mode toolbar (one toggle for the whole tab), so
    // the editor header's copy is suppressed.
    expect(editor).toHaveAttribute("data-hide", "true");

    const toggle = screen.getByRole("button", { name: "Toggle light or dark theme" });
    expect(toggle).toHaveAttribute("aria-pressed", "false");

    // PreviewSurface mounts only once the mode leaves "edit" (the default).
    act(() => {
      screen.getByRole("button", { name: "Preview" }).click();
    });
    expect(screen.getByTestId("preview")).toHaveAttribute("data-appearance", "dark");

    act(() => {
      screen.getByRole("button", { name: "Toggle light or dark theme" }).click();
    });

    expect(window.localStorage.getItem(EDITOR_APPEARANCE_STORAGE_KEY)).toBe("light");
    expect(screen.getByRole("button", { name: "Toggle light or dark theme" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByTestId("preview")).toHaveAttribute("data-appearance", "light");
  });

  it("follows a light VS Code theme's polarity when the preference is auto", async () => {
    // §8.6 case 2 + the \"auto\" migration: the preview must receive the
    // EFFECTIVE polarity, not the stored \"auto\" string.
    getAppearanceSettings.mockResolvedValue({
      color_theme: "vscode:acme.themes/light",
      icon_theme: "",
      extra_extension_dirs: [],
      effective_color_theme: "vscode:acme.themes/light",
    });
    getAppearanceTheme.mockResolvedValue({
      id: "vscode:acme.themes/light",
      label: "Acme Light",
      kind: "light",
      colors: {
        user: "#1e66f5",
        assistant: "#4c4f69",
        header: "#1e66f5",
        border: "#ccd0da",
        hint: "#9ca0b0",
        text: "#4c4f69",
        background: "#eff1f5",
        status_bg: "#e6e9ef",
        status_fg: "#4c4f69",
        selected_fg: "#4c4f69",
        selected_bg: "#ccd0da",
        success: "#40a02b",
        error: "#d20f39",
        accent: "#7287fd",
        dim: "#9ca0b0",
        thinking: "#9ca0b0",
      },
      ui: {},
      monaco: { base: "vs", colors: {}, rules: [] },
      terminal: {},
    });

    render(<FileTabContent path="README.md" content="# Hi" />);
    await screen.findByTestId("editor");

    act(() => {
      screen.getByRole("button", { name: "Preview" }).click();
    });

    await waitFor(() =>
      expect(screen.getByTestId("preview")).toHaveAttribute("data-appearance", "light"),
    );
    expect(screen.getByRole("button", { name: "Toggle light or dark theme" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });
});
