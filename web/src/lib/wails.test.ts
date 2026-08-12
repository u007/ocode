import { describe, expect, it, vi } from "vitest";
import { notifyWailsRuntimeReady } from "./wails";

describe("notifyWailsRuntimeReady", () => {
  it("notifies an injected Wails bridge", () => {
    const invoke = vi.fn();
    const target = { _wails: { invoke } } as unknown as Window;

    expect(notifyWailsRuntimeReady(target)).toBe(true);
    expect(invoke).toHaveBeenCalledWith("wails:runtime:ready");
  });

  it("reports when the bridge has not been injected yet", () => {
    expect(notifyWailsRuntimeReady({} as Window)).toBe(false);
  });
});
