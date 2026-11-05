import { afterEach, describe, expect, it, vi } from "vitest";
import { PRINT_BOOTSTRAP } from "./printDocument";

// Runs the bootstrap's inline script in the test window, as the print window
// runs it in the desktop shell.
function runBootstrap(): void {
  const source = PRINT_BOOTSTRAP.replace(/^<script>/, "").replace(/<\/script>$/, "");
  new Function(source)();
}

afterEach(() => {
  vi.useRealTimers();
  delete (window as unknown as { _wails?: unknown })._wails;
});

describe("PRINT_BOOTSTRAP", () => {
  it("sends the ready message once the Wails bridge is injected after load", () => {
    vi.useFakeTimers();
    const invoke = vi.fn();
    runBootstrap();
    expect(invoke).not.toHaveBeenCalled();

    // Wails injects _wails.invoke from its WebViewDidFinishNavigation hook, after
    // the document has loaded and the bootstrap has already run.
    vi.advanceTimersByTime(200);
    (window as unknown as { _wails: unknown })._wails = { invoke };
    vi.advanceTimersByTime(100);

    expect(invoke).toHaveBeenCalledTimes(1);
    expect(invoke).toHaveBeenCalledWith("ocode:print-window-ready");

    vi.advanceTimersByTime(30_000);
    expect(invoke).toHaveBeenCalledTimes(1);
  });

  it("sends the ready message immediately when the bridge already exists", () => {
    const invoke = vi.fn();
    (window as unknown as { _wails: unknown })._wails = { invoke };
    runBootstrap();
    expect(invoke).toHaveBeenCalledWith("ocode:print-window-ready");
  });

  it("stops polling without a bridge", () => {
    vi.useFakeTimers();
    runBootstrap();
    vi.advanceTimersByTime(16_000);
    expect(vi.getTimerCount()).toBe(0);
  });
});
