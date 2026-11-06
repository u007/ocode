import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import ViewerContextChip from "./ViewerContextChip";
import type { ViewerState } from "../../lib/viewerContext";

const state: ViewerState = {
  path: "docs/guide/install.md",
  kind: "markdown",
  location: { heading: "Install" },
  selection: "run make",
  visibleText: null,
  find: null,
  control: null,
  updatedAt: 0,
};

describe("ViewerContextChip", () => {
  it("renders the basename, the location and the selection marker", () => {
    render(<ViewerContextChip state={state} onRemove={() => {}} />);
    expect(screen.getByText("install.md · § Install · selection")).toBeTruthy();
  });

  it("marks visible text when there is no selection", () => {
    render(<ViewerContextChip state={{ ...state, selection: null, visibleText: "hello" }} onRemove={() => {}} />);
    expect(screen.getByText("install.md · § Install · visible")).toBeTruthy();
  });

  it("calls onRemove when the X button is clicked", () => {
    const onRemove = vi.fn();
    render(<ViewerContextChip state={state} onRemove={onRemove} />);
    fireEvent.click(screen.getByRole("button", { name: "Remove viewer context from this message" }));
    expect(onRemove).toHaveBeenCalledTimes(1);
  });
});
