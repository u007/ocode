import { beforeEach, describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { PreviewWidthMenu } from "./PreviewWidthMenu";
import {
  __resetPreviewWidthForTests,
  PREVIEW_WIDTH_STORAGE_KEY,
  getPreviewWidthPreset,
} from "../../lib/previewWidth";

beforeEach(() => {
  window.localStorage.clear();
  __resetPreviewWidthForTests();
});

describe("PreviewWidthMenu", () => {
  it("shows the column in use and lists every preset", () => {
    render(<PreviewWidthMenu />);

    // Default is auto, which resolves from the runtime locale (jsdom: en-US →
    // Letter). The trigger names the RESOLVED column, not the word "auto".
    const trigger = screen.getByTestId("preview-width-trigger");
    expect(trigger).toHaveAttribute("aria-label", expect.stringContaining("Auto"));

    fireEvent.click(trigger);
    for (const preset of ["auto", "letter", "a4", "full"]) {
      expect(screen.getByTestId(`preview-width-${preset}`)).toBeTruthy();
    }
    // The F4 request stays visible even though it shares the A4 column width.
    expect(screen.getByTestId("preview-width-a4").textContent).toContain("F4");
  });

  it("marks the active preset and moves the preference when one is picked", () => {
    render(<PreviewWidthMenu />);
    fireEvent.click(screen.getByTestId("preview-width-trigger"));

    expect(screen.getByTestId("preview-width-auto")).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByTestId("preview-width-letter")).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(screen.getByTestId("preview-width-letter"));

    expect(getPreviewWidthPreset()).toBe("letter");
    expect(window.localStorage.getItem(PREVIEW_WIDTH_STORAGE_KEY)).toBe("letter");
    // The menu closes, and the trigger now names the chosen column.
    expect(screen.queryByTestId("preview-width-full")).toBeNull();
    expect(screen.getByTestId("preview-width-trigger").textContent).toContain("Letter");
  });

  it("reflects a preference already stored by another surface", () => {
    window.localStorage.setItem(PREVIEW_WIDTH_STORAGE_KEY, "full");
    render(<PreviewWidthMenu />);

    expect(screen.getByTestId("preview-width-trigger").textContent).toContain("Full");
  });

  it("labels Auto with what AUTO renders, not with the selected preset", () => {
    // Regression: with a non-auto preset the Auto row used to be labelled with
    // that preset's column ("Auto — A4" on en-US, where Auto renders Letter).
    window.localStorage.setItem(PREVIEW_WIDTH_STORAGE_KEY, "a4");
    render(<PreviewWidthMenu />);
    fireEvent.click(screen.getByTestId("preview-width-trigger"));

    expect(screen.getByTestId("preview-width-auto").textContent).toBe("Auto — Letter");
    expect(screen.getByTestId("preview-width-a4").textContent).toContain("A4 / F4");
  });
});
