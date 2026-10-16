import { afterEach, describe, expect, it, vi } from "vitest";
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

describe("writeDesktopClipboard", () => {
  afterEach(() => {
    window.history.replaceState({}, "", "/");
    vi.resetModules();
  });

  // wails.ts reads ?clip= once at module load, so each case loads a fresh copy
  // after setting the location it should see.
  async function loadWithLocation(path: string) {
    window.history.replaceState({}, "", path);
    vi.resetModules();
    return import("./wails");
  }

  it("sends the nonce from the window URL with the text", async () => {
    const { writeDesktopClipboard } = await loadWithLocation("/session/abc?token=t&clip=abc123");
    const invoke = vi.fn();
    const target = { _wails: { invoke } } as unknown as Window;

    expect(writeDesktopClipboard("hello", target)).toBe(true);
    expect(invoke).toHaveBeenCalledWith("ocode:clipboard-write:abc123:hello");
  });

  it("keeps the nonce after the router rewrites the location", async () => {
    const { writeDesktopClipboard } = await loadWithLocation("/?clip=abc123");
    window.history.replaceState({}, "", "/session/next");
    const invoke = vi.fn();
    const target = { _wails: { invoke } } as unknown as Window;

    expect(writeDesktopClipboard("hi", target)).toBe(true);
    expect(invoke).toHaveBeenCalledWith("ocode:clipboard-write:abc123:hi");
  });

  it("sends nothing and reports false when the page has no nonce", async () => {
    const { writeDesktopClipboard } = await loadWithLocation("/session/abc?token=t");
    const invoke = vi.fn();
    const target = { _wails: { invoke } } as unknown as Window;

    expect(writeDesktopClipboard("hello", target)).toBe(false);
    expect(invoke).not.toHaveBeenCalled();
  });
});
