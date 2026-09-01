import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import EditorContextChip from "./EditorContextChip";

describe("EditorContextChip", () => {
  it("renders the path with no X when onRemove is absent (read-only)", () => {
    render(<EditorContextChip path="src/foo.ts" />);
    expect(screen.getByText("src/foo.ts")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: /remove src\/foo\.ts from this message/i }),
    ).toBeNull();
  });

  it("renders a selection range in the label when provided", () => {
    render(<EditorContextChip path="src/foo.ts" selection={{ startLine: 10, endLine: 20 }} />);
    expect(screen.getByText("src/foo.ts:10-20")).toBeTruthy();
  });

  it("shows an X button when onRemove is provided and fires it on click", () => {
    const onRemove = vi.fn();
    render(<EditorContextChip path="src/foo.ts" onRemove={onRemove} />);
    const btn = screen.getByRole("button", { name: /remove src\/foo\.ts from this message/i });
    expect(btn).toBeTruthy();
    fireEvent.click(btn);
    expect(onRemove).toHaveBeenCalledTimes(1);
  });

  it("uses an accessible aria-label that includes the file path", () => {
    const onRemove = vi.fn();
    render(<EditorContextChip path="deep/nested/file.go" onRemove={onRemove} />);
    expect(
      screen.getByRole("button", { name: /remove deep\/nested\/file\.go from this message/i }),
    ).toBeTruthy();
  });
});
