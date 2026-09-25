import { afterEach, describe, expect, it, vi } from "vitest";
import { copyTextToClipboard } from "./clipboard";

const originalClipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");
const originalExecCommand = (document as unknown as { execCommand?: unknown }).execCommand;

function restoreEnvironment() {
  if (originalClipboard) {
    Object.defineProperty(navigator, "clipboard", originalClipboard);
  } else {
    delete (navigator as unknown as { clipboard?: unknown }).clipboard;
  }
  (document as unknown as { execCommand?: unknown }).execCommand = originalExecCommand;
}

afterEach(() => {
  restoreEnvironment();
  vi.restoreAllMocks();
});

/** Emulate WKWebView: execCommand always claims success, but the copied value
 *  only exists when the active element really is the scratch textarea. */
function installExecCommand(copied: string[]): void {
  (document as unknown as { execCommand: (command: string) => boolean }).execCommand = (
    command: string,
  ) => {
    if (command === "copy") {
      const active = document.activeElement as HTMLTextAreaElement | null;
      if (active && active.tagName === "TEXTAREA") copied.push(active.value);
    }
    return true;
  };
}

describe("copyTextToClipboard", () => {
  it("refuses empty text without touching the clipboard", async () => {
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    await expect(copyTextToClipboard("")).resolves.toBe(false);
    expect(writeText).not.toHaveBeenCalled();
  });

  it("writes through navigator.clipboard when it is available", async () => {
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    await expect(copyTextToClipboard("hello")).resolves.toBe(true);
    expect(writeText).toHaveBeenCalledWith("hello");
  });

  it("falls back to a focused scratch textarea when navigator.clipboard rejects", async () => {
    const writeText = vi.fn(async () => {
      throw new Error("denied");
    });
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    const copied: string[] = [];
    installExecCommand(copied);
    await expect(copyTextToClipboard("fallback text")).resolves.toBe(true);
    expect(copied).toEqual(["fallback text"]);
  });

  it("does not trust execCommand when the scratch field never takes focus", async () => {
    delete (navigator as unknown as { clipboard?: unknown }).clipboard;
    installExecCommand([]);
    const focus = vi
      .spyOn(HTMLTextAreaElement.prototype, "focus")
      .mockImplementation(() => {});
    await expect(copyTextToClipboard("unfocused")).resolves.toBe(false);
    focus.mockRestore();
  });

  it("restores the previously focused element after the fallback", async () => {
    delete (navigator as unknown as { clipboard?: unknown }).clipboard;
    const copied: string[] = [];
    installExecCommand(copied);
    const button = document.createElement("button");
    document.body.appendChild(button);
    button.focus();
    await expect(copyTextToClipboard("focus restore")).resolves.toBe(true);
    expect(document.activeElement).toBe(button);
    button.remove();
  });

  it("returns false when neither clipboard path is available", async () => {
    delete (navigator as unknown as { clipboard?: unknown }).clipboard;
    delete (document as unknown as { execCommand?: unknown }).execCommand;
    await expect(copyTextToClipboard("nowhere")).resolves.toBe(false);
  });
});
