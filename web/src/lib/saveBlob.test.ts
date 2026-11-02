import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The desktop bridge reads its per-launch nonce from ?clip= once, at module
// load, so each case sets the URL and re-imports the module.
async function loadWithClip(clip: string | null) {
  const search = clip === null ? "" : `?clip=${clip}`;
  window.history.replaceState({}, "", `/${search}`);
  vi.resetModules();
  const saveBlob = await import("./saveBlob");
  const wails = await import("./wails");
  return { ...saveBlob, MAX_DESKTOP_SAVE_BYTES: wails.MAX_DESKTOP_SAVE_BYTES };
}

// jsdom's Blob has no arrayBuffer(); browsers and WKWebView do. Give the test
// blob the method so the desktop branch can read its bytes.
function blobOf(bytes: number[]): Blob {
  const data = new Uint8Array(bytes);
  return Object.assign(new Blob([data]), { arrayBuffer: async () => data.slice().buffer });
}

describe("saveBlobAsFile", () => {
  beforeEach(() => {
    delete (window as Window & { _wails?: unknown })._wails;
  });
  afterEach(() => {
    delete (window as Window & { _wails?: unknown })._wails;
    vi.restoreAllMocks();
  });

  it("uses a download anchor in a plain browser", async () => {
    const { saveBlobAsFile } = await loadWithClip("abc");
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    await saveBlobAsFile(new Blob(["hi"]), "a.txt");
    expect(click).toHaveBeenCalledTimes(1);
  });

  it("sends the bytes to the desktop shell instead of clicking an anchor", async () => {
    const { saveBlobAsFile } = await loadWithClip("abc");
    const invoke = vi.fn();
    (window as Window & { _wails?: unknown })._wails = { invoke };
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});

    await saveBlobAsFile(blobOf([0x25, 0x50, 0x44, 0x46]), "doc:1.pdf");

    expect(click).not.toHaveBeenCalled();
    expect(invoke).toHaveBeenCalledTimes(1);
    const message = invoke.mock.calls[0][0] as string;
    expect(message.startsWith("ocode:save-file:abc:")).toBe(true);
    const body = JSON.parse(message.slice("ocode:save-file:abc:".length));
    expect(body.name).toBe("doc:1.pdf");
    expect(atob(body.data)).toBe("%PDF");
  });

  it("rejects on desktop when the page has no nonce, and sends nothing", async () => {
    const { saveBlobAsFile } = await loadWithClip(null);
    const invoke = vi.fn();
    (window as Window & { _wails?: unknown })._wails = { invoke };

    await expect(saveBlobAsFile(blobOf([1]), "a.txt")).rejects.toThrow(/no desktop nonce/);
    expect(invoke).not.toHaveBeenCalled();
  });

  it("rejects on desktop when the file is over the save limit, and sends nothing", async () => {
    const { saveBlobAsFile, MAX_DESKTOP_SAVE_BYTES } = await loadWithClip("abc");
    const invoke = vi.fn();
    (window as Window & { _wails?: unknown })._wails = { invoke };
    // A real Blob whose size is overridden: allocating 64 MiB in jsdom is not cheap.
    // arrayBuffer is an own spy so the test can prove the size is refused before
    // the bytes are read into memory.
    const big = new Blob([]);
    Object.defineProperty(big, "size", { value: MAX_DESKTOP_SAVE_BYTES + 1 });
    const arrayBuffer = vi.fn();
    Object.defineProperty(big, "arrayBuffer", { value: arrayBuffer });

    await expect(saveBlobAsFile(big, "big.bin")).rejects.toThrow(/desktop save limit/);
    expect(arrayBuffer).not.toHaveBeenCalled();
    expect(invoke).not.toHaveBeenCalled();
  });
});
