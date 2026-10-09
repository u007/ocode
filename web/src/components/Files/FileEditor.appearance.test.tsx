// FileEditor's light/dark toggle lives in the editor header (the split-view
// host suppresses it in favour of the mode toolbar's copy). Monaco itself is
// irrelevant here, so stub the editor and its worker setup.
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@monaco-editor/react", () => ({
  default: () => <div data-testid="monaco-stub" />,
}));
vi.mock("../../lib/monaco-setup", () => ({}));
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
  },
}));

import FileEditor from "./FileEditor";
import {
  EDITOR_APPEARANCE_STORAGE_KEY,
  __resetEditorAppearanceForTests,
} from "../../lib/editorAppearance";

describe("FileEditor appearance toggle", () => {
  beforeEach(() => {
    window.localStorage.clear();
    __resetEditorAppearanceForTests();
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
  });

  it("hides the toggle when the split host owns it", () => {
    render(<FileEditor path="src/a.ts" content="x" hideAppearanceToggle />);
    expect(screen.queryByRole("button", { name: "Toggle light or dark theme" })).toBeNull();
  });
});
