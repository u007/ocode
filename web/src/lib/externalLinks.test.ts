import { afterEach, describe, expect, it, vi } from "vitest";
import { OPEN_EXTERNAL_MESSAGE_PREFIX, openExternalURL } from "./externalLinks";

type WailsWindow = Window & { _wails?: { invoke?: (message: string) => void } };

function setBridge(invoke?: (message: string) => void) {
  (window as WailsWindow)._wails = invoke ? { invoke } : {};
}

afterEach(() => {
  delete (window as WailsWindow)._wails;
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("openExternalURL", () => {
  it("sends the URL to the desktop bridge instead of navigating the webview", () => {
    const invoke = vi.fn();
    setBridge(invoke);
    const open = vi.fn();
    vi.stubGlobal("open", open);

    openExternalURL("https://example.com/a?b=c#d");

    expect(invoke).toHaveBeenCalledTimes(1);
    expect(invoke).toHaveBeenCalledWith(OPEN_EXTERNAL_MESSAGE_PREFIX + "https://example.com/a?b=c#d");
    // The browser popup path must not be used when the desktop bridge exists.
    expect(open).not.toHaveBeenCalled();
  });

  it("retries once when the bridge has not been injected yet", () => {
    vi.useFakeTimers();
    const invoke = vi.fn();
    setBridge(undefined); // window._wails present, but no invoke yet

    openExternalURL("https://example.com");
    expect(invoke).not.toHaveBeenCalled();

    (window as WailsWindow)._wails = { invoke };
    vi.advanceTimersByTime(50);
    expect(invoke).toHaveBeenCalledWith(OPEN_EXTERNAL_MESSAGE_PREFIX + "https://example.com");
  });

  it("ignores values that are not http(s)", () => {
    const invoke = vi.fn();
    setBridge(invoke);

    openExternalURL("javascript:alert(1)");
    openExternalURL("file:///etc/passwd");
    openExternalURL("ocode-file://open?d=x");

    expect(invoke).not.toHaveBeenCalled();
  });

  it("opens a popup in a plain browser", () => {
    delete (window as WailsWindow)._wails;
    const open = vi.fn(() => ({}) as Window);
    vi.stubGlobal("open", open);

    openExternalURL("https://example.com");

    expect(open).toHaveBeenCalledWith("https://example.com", "_blank", "noopener,noreferrer");
  });
});
