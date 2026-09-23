// The header Save button exists so touch devices (which have no Cmd/Ctrl+S)
// can save an edited file. Monaco itself is irrelevant to the button — it lives
// in the editor chrome — so stub the editor and its worker setup (the
// `?worker` imports don't resolve under vitest).
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

vi.mock("@monaco-editor/react", () => ({
  default: () => <div data-testid="monaco-stub" />,
}));
vi.mock("../../lib/monaco-setup", () => ({}));

import FileEditor from "./FileEditor";

describe("FileEditor header Save button", () => {
  it("renders a disabled Save button when there are no unsaved changes", () => {
    render(<FileEditor path="src/a.ts" content="x" dirty={false} onSave={vi.fn()} />);

    const btn = screen.getByRole("button", { name: "Save file" });
    expect(btn).toBeDisabled();
  });

  it("enables Save and calls onSave when the buffer is dirty", () => {
    const onSave = vi.fn();
    render(<FileEditor path="src/a.ts" content="x" dirty onSave={onSave} />);

    const btn = screen.getByRole("button", { name: "Save file" });
    expect(btn).toBeEnabled();

    fireEvent.click(btn);
    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it("omits the Save button when no onSave handler is provided (read-only surfaces)", () => {
    render(<FileEditor path="src/a.ts" content="x" dirty />);

    expect(screen.queryByRole("button", { name: "Save file" })).toBeNull();
  });
});
