import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";

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

import FileTabContent from "./FileTabContent";
import {
  EDITOR_APPEARANCE_STORAGE_KEY,
  __resetEditorAppearanceForTests,
} from "../../lib/editorAppearance";

describe("FileTabContent appearance toggle", () => {
  beforeEach(() => {
    window.localStorage.clear();
    __resetEditorAppearanceForTests();
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
});
