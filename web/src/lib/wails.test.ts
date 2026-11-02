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

describe("printDesktopDocument", () => {
  afterEach(() => {
    window.history.replaceState({}, "", "/");
    vi.resetModules();
  });

  // The nonce is read at module load, so each case loads a fresh copy.
  async function loadWithNonce() {
    window.history.replaceState({}, "", "/?clip=abc123");
    vi.resetModules();
    return import("./wails");
  }

  const prefix = "ocode:print-document:abc123:";

  it("sends nothing and reports false when the page has no nonce", async () => {
    window.history.replaceState({}, "", "/session/abc?token=t");
    vi.resetModules();
    const { printDesktopDocument } = await import("./wails");
    const invoke = vi.fn();
    const target = { _wails: { invoke } } as unknown as Window;

    expect(printDesktopDocument("<p>doc</p>", target)).toBe(false);
    expect(invoke).not.toHaveBeenCalled();
  });

  it("sends a message whose total size is exactly the limit", async () => {
    const { printDesktopDocument, MAX_PRINT_DOCUMENT_BYTES } = await loadWithNonce();
    const invoke = vi.fn();
    const target = { _wails: { invoke } } as unknown as Window;
    const html = "x".repeat(MAX_PRINT_DOCUMENT_BYTES - prefix.length);

    expect(printDesktopDocument(html, target)).toBe(true);
    expect(invoke).toHaveBeenCalledWith(`${prefix}${html}`);
  });

  it("throws and sends nothing when the message is over the limit", async () => {
    const { printDesktopDocument, MAX_PRINT_DOCUMENT_BYTES } = await loadWithNonce();
    const invoke = vi.fn();
    const target = { _wails: { invoke } } as unknown as Window;
    const html = "x".repeat(MAX_PRINT_DOCUMENT_BYTES - prefix.length + 1);

    expect(() => printDesktopDocument(html, target)).toThrow(
      `print: document is ${MAX_PRINT_DOCUMENT_BYTES + 1} bytes, over the ${MAX_PRINT_DOCUMENT_BYTES}-byte print limit`,
    );
    expect(invoke).not.toHaveBeenCalled();
  });

  it("measures UTF-8 bytes, not characters", async () => {
    const { printDesktopDocument, MAX_PRINT_DOCUMENT_BYTES } = await loadWithNonce();
    const invoke = vi.fn();
    const target = { _wails: { invoke } } as unknown as Window;
    // Each "é" is 2 UTF-8 bytes, so half the limit in characters is over the limit in bytes.
    const html = "é".repeat(MAX_PRINT_DOCUMENT_BYTES / 2);

    expect(html.length).toBeLessThan(MAX_PRINT_DOCUMENT_BYTES);
    expect(() => printDesktopDocument(html, target)).toThrow("over the 8388608-byte print limit");
    expect(invoke).not.toHaveBeenCalled();
  });
});
