import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CopyValueButton, CopyableValue } from "./CopyValueButton";

const copyTextToClipboard = vi.hoisted(() => vi.fn(async (_text: string) => true));
vi.mock("../../lib/clipboard", () => ({ copyTextToClipboard }));

describe("CopyValueButton", () => {
  afterEach(() => {
    copyTextToClipboard.mockClear();
    copyTextToClipboard.mockResolvedValue(true);
    vi.useRealTimers();
  });

  it("copies the value and confirms it", async () => {
    render(<CopyValueButton value="ses_abc" label="Copy session ID" testId="copy" />);
    await act(async () => {
      fireEvent.click(screen.getByTestId("copy"));
    });
    expect(copyTextToClipboard).toHaveBeenCalledWith("ses_abc");
    expect(screen.getByTestId("copy").dataset.state).toBe("copied");
  });

  // The copy helper returns a success BOOLEAN, and it returns false in the real
  // insecure-origin / unfocused fallback path. Flashing "copied" regardless
  // would be a lie the user only discovers at paste time.
  it("reports failure instead of success when the clipboard write is refused", async () => {
    copyTextToClipboard.mockResolvedValue(false);
    render(<CopyValueButton value="ses_abc" label="Copy session ID" testId="copy" />);
    await act(async () => {
      fireEvent.click(screen.getByTestId("copy"));
    });
    expect(screen.getByTestId("copy").dataset.state).toBe("failed");
  });

  it("returns to idle after the confirmation expires", async () => {
    vi.useFakeTimers();
    render(<CopyValueButton value="ses_abc" label="Copy session ID" testId="copy" />);
    await act(async () => {
      fireEvent.click(screen.getByTestId("copy"));
    });
    expect(screen.getByTestId("copy").dataset.state).toBe("copied");
    await act(async () => {
      vi.advanceTimersByTime(1500);
    });
    expect(screen.getByTestId("copy").dataset.state).toBe("idle");
  });

  // The button lives inside clickable rows (a tab pill switches sessions, a
  // sidebar chat row opens the chat). If the click escaped, the user would be
  // navigated away from the session whose ID they just copied.
  it("does not let the click reach the surrounding clickable row", async () => {
    const onRowClick = vi.fn();
    render(
      // eslint-disable-next-line jsx-a11y/click-events-have-key-events
      <div onClick={onRowClick}>
        <CopyValueButton value="ses_abc" label="Copy session ID" testId="copy" />
      </div>,
    );
    await act(async () => {
      fireEvent.click(screen.getByTestId("copy"));
    });
    expect(copyTextToClipboard).toHaveBeenCalledWith("ses_abc");
    expect(onRowClick).not.toHaveBeenCalled();
  });

  // The sidebar's chat rows open on pointer-UP, not click. Stopping only the
  // click would still navigate.
  it("does not let pointer-up reach a row that opens on pointer-up", async () => {
    const onRowPointerUp = vi.fn();
    render(
      // eslint-disable-next-line jsx-a11y/click-events-have-key-events
      <div onPointerUp={onRowPointerUp}>
        <CopyValueButton value="ses_abc" label="Copy session ID" testId="copy" />
      </div>,
    );
    await act(async () => {
      fireEvent.pointerDown(screen.getByTestId("copy"));
      fireEvent.pointerUp(screen.getByTestId("copy"));
    });
    expect(onRowPointerUp).not.toHaveBeenCalled();
  });

  it("renders nothing for an empty value", () => {
    render(<CopyValueButton value="" label="Copy session ID" testId="copy" />);
    expect(screen.queryByTestId("copy")).toBeNull();
  });

  // Hovering must not reflow the row: the button is permanently mounted at a
  // fixed size and only its opacity changes. A conditionally rendered button
  // reshuffles every pill in the wrapping tab strip on pointer-arrival.
  it("keeps the button mounted and non-interactive while hidden", () => {
    render(<CopyableValue value="ses_abc" label="Copy session ID" testId="copy" />);
    const btn = screen.getByTestId("copy");
    expect(btn.className).toContain("opacity-0");
    expect(btn.className).toContain("pointer-events-none");
    expect(btn.className).toContain("group-hover:opacity-100");
    expect(btn.className).toContain("h-4");
  });

  it("reveals on hover over the value it belongs to, not the whole row", () => {
    render(<CopyableValue value="ses_abc" label="Copy session ID" testId="copy" />);
    const group = screen.getByTestId("copy").parentElement!;
    expect(group.className).toContain("group");
  });

  it("renders custom children beside the value", () => {
    render(
      <CopyableValue value="ses_abc" label="Copy session ID" testId="copy">
        <em>pretty</em>
      </CopyableValue>,
    );
    expect(screen.getByText("pretty")).toBeTruthy();
  });
});
