import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { BlockCopyControl } from "./BlockCopyControl";

const copyTextToClipboard = vi.hoisted(() => vi.fn(async (_text: string) => true));
vi.mock("../../lib/clipboard", () => ({ copyTextToClipboard }));

describe("BlockCopyControl", () => {
  afterEach(() => {
    copyTextToClipboard.mockClear();
    copyTextToClipboard.mockResolvedValue(true);
    vi.useRealTimers();
  });

  it("copies the rendered text by default", async () => {
    render(<BlockCopyControl rawText="# raw" getRenderedText={() => "rendered"} />);
    await act(async () => {
      fireEvent.click(screen.getByTestId("block-copy-default"));
    });
    expect(copyTextToClipboard).toHaveBeenLastCalledWith("rendered");
  });

  it("falls back to the raw text when the rendered extractor is empty", async () => {
    render(<BlockCopyControl rawText="# raw" getRenderedText={() => ""} />);
    await act(async () => {
      fireEvent.click(screen.getByTestId("block-copy-default"));
    });
    expect(copyTextToClipboard).toHaveBeenLastCalledWith("# raw");
  });

  it("copies the raw source from the dropdown item", async () => {
    render(
      <BlockCopyControl
        rawText="# raw"
        getRenderedText={() => "rendered"}
        rawLabel="Copy as raw Markdown"
      />,
    );
    fireEvent.click(screen.getByTestId("block-copy-menu"));
    const item = await screen.findByTestId("block-copy-raw");
    await act(async () => {
      fireEvent.click(item);
    });
    expect(copyTextToClipboard).toHaveBeenLastCalledWith("# raw");
  });

  it("shows Copied after a successful write and resets", async () => {
    vi.useFakeTimers();
    render(<BlockCopyControl rawText="raw" />);
    await act(async () => {
      fireEvent.click(screen.getByTestId("block-copy-default"));
    });
    const button = screen.getByTestId("block-copy-default");
    expect(button.dataset.state).toBe("copied");
    await act(async () => {
      vi.advanceTimersByTime(1600);
    });
    expect(button.dataset.state).toBe("idle");
  });

  it("shows a blocked state instead of claiming success", async () => {
    copyTextToClipboard.mockResolvedValueOnce(false);
    render(<BlockCopyControl rawText="raw" />);
    await act(async () => {
      fireEvent.click(screen.getByTestId("block-copy-default"));
    });
    expect(screen.getByTestId("block-copy-default").dataset.state).toBe("failed");
  });

  it("labels the menu trigger and keeps the control out of speech extraction", async () => {
    render(<BlockCopyControl rawText="raw" ariaLabel="Copy thinking" />);
    const menu = screen.getByTestId("block-copy-menu");
    expect(menu.getAttribute("aria-haspopup")).toBe("menu");
    expect(screen.getByTestId("block-copy").hasAttribute("data-speech-exclude")).toBe(true);
    fireEvent.click(menu);
    expect(await screen.findByRole("menu")).toBeInTheDocument();
    expect(screen.getByTestId("block-copy-raw").getAttribute("role")).toBe("menuitem");
  });
});
