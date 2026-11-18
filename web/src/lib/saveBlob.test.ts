import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The desktop bridge reads its per-launch nonce from ?clip= once, at module
// load, so each case sets the URL and re-imports the module.
async function loadWithClip(clip: string | null) {
  const search = clip === null ? "" : `?clip=${clip}`;
  window.history.replaceState({}, "", `/${search}`);
  vi.resetModules();
  const saveBlob = await import("./saveBlob");
  const wails = await import("./wails");
  return { ...saveBlob, MAX_DESKTOP_SAVE_BYTES: wails.MAX_DESKTOP_SAVE_BYTES, SAVE_CHUNK_BYTES: wails.SAVE_CHUNK_BYTES };
}

// jsdom's Blob has no arrayBuffer(); browsers and WKWebView do. Give the test
// blob the method so blob slices can be read, and make the desktop path's
// whole-blob arrayBuffer() observable.
function blobOf(bytes: Uint8Array): Blob & { wholeBufferRead: () => boolean } {
  let readWhole = false;
  const mk = (d: Uint8Array) =>
    Object.assign(new Blob([d as unknown as BlobPart]), {
      arrayBuffer: async () => {
        if (d === bytes) {
          readWhole = true;
        }
        return d.slice().buffer as ArrayBuffer;
      },
    });
  const blob = mk(bytes) as Blob & { wholeBufferRead: () => boolean };
  Object.defineProperty(blob, "slice", {
    value: (start?: number, end?: number) => mk(bytes.subarray(start ?? 0, end ?? bytes.length)),
  });
  blob.wholeBufferRead = () => readWhole;
  return blob;
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

  it("streams the file to the desktop shell as start/chunk/end, never clicking an anchor", async () => {
    const { saveBlobAsFile } = await loadWithClip("abc");
    const invoke = vi.fn();
    (window as Window & { _wails?: unknown })._wails = { invoke };
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});

    const bytes = new Uint8Array([0x25, 0x50, 0x44, 0x46]);
    const blob = blobOf(bytes);
    await saveBlobAsFile(blob, "doc:1.pdf");

    expect(click).not.toHaveBeenCalled();
    expect(blob.wholeBufferRead()).toBe(false);
    expect(invoke).toHaveBeenCalledTimes(3);

    const [startMsg, chunkMsg, endMsg] = invoke.mock.calls.map((c) => c[0] as string);
    expect(startMsg.startsWith("ocode:save-file-start:abc:")).toBe(true);
    const start = JSON.parse(startMsg.slice("ocode:save-file-start:abc:".length));
    expect(start.name).toBe("doc:1.pdf");
    expect(start.size).toBe(4);
    expect(typeof start.id).toBe("string");
    expect(start.id.length).toBeGreaterThan(0);

    expect(chunkMsg.startsWith("ocode:save-file-chunk:abc:")).toBe(true);
    const chunk = JSON.parse(chunkMsg.slice("ocode:save-file-chunk:abc:".length));
    expect(chunk.id).toBe(start.id);
    expect(chunk.seq).toBe(0);
    expect(atob(chunk.data)).toBe("%PDF");

    expect(endMsg.startsWith("ocode:save-file-end:abc:")).toBe(true);
    const end = JSON.parse(endMsg.slice("ocode:save-file-end:abc:".length));
    expect(end.id).toBe(start.id);
  });

  it("splits a file larger than one chunk into sequenced messages", async () => {
    const { saveBlobAsFile, SAVE_CHUNK_BYTES } = await loadWithClip("abc");
    const invoke = vi.fn();
    (window as Window & { _wails?: unknown })._wails = { invoke };

    const bytes = new Uint8Array(SAVE_CHUNK_BYTES + 3).fill(65);
    await saveBlobAsFile(blobOf(bytes), "big.bin");

    expect(invoke).toHaveBeenCalledTimes(4); // start + 2 chunks + end
    const messages = invoke.mock.calls.map((c) => c[0] as string);
    const seqs: number[] = [];
    let total = 0;
    for (const message of messages.slice(1, 3)) {
      const body = JSON.parse(message.slice("ocode:save-file-chunk:abc:".length));
      seqs.push(body.seq);
      total += atob(body.data).length;
    }
    expect(seqs).toEqual([0, 1]);
    expect(total).toBe(bytes.length);
    const end = JSON.parse(messages[3].slice("ocode:save-file-end:abc:".length));
    const start = JSON.parse(messages[0].slice("ocode:save-file-start:abc:".length));
    expect(end.id).toBe(start.id);
  });

  it("sends start then end for a zero-byte file, with no chunk", async () => {
    const { saveBlobAsFile } = await loadWithClip("abc");
    const invoke = vi.fn();
    (window as Window & { _wails?: unknown })._wails = { invoke };

    await saveBlobAsFile(new Blob([]), "empty.txt");
    expect(invoke).toHaveBeenCalledTimes(2);
    const start = JSON.parse((invoke.mock.calls[0][0] as string).slice("ocode:save-file-start:abc:".length));
    expect(start.size).toBe(0);
    expect((invoke.mock.calls[1][0] as string).startsWith("ocode:save-file-end:abc:")).toBe(true);
  });

  it("rejects on desktop when the page has no nonce, and sends nothing", async () => {
    const { saveBlobAsFile } = await loadWithClip(null);
    const invoke = vi.fn();
    (window as Window & { _wails?: unknown })._wails = { invoke };

    await expect(saveBlobAsFile(blobOf(new Uint8Array([1])), "a.txt")).rejects.toThrow(/no desktop nonce/);
    expect(invoke).not.toHaveBeenCalled();
  });

  it("rejects on desktop when the file is over the save limit, and sends nothing", async () => {
    const { saveBlobAsFile, MAX_DESKTOP_SAVE_BYTES } = await loadWithClip("abc");
    const invoke = vi.fn();
    (window as Window & { _wails?: unknown })._wails = { invoke };
    // A Blob whose size is overridden: allocating 2 GiB in jsdom is not
    // possible. slice/arrayBuffer are own spies so the test can prove the
    // size is refused before the bytes are read.
    const big = new Blob([]);
    Object.defineProperty(big, "size", { value: MAX_DESKTOP_SAVE_BYTES + 1 });
    const slice = vi.fn();
    const arrayBuffer = vi.fn();
    Object.defineProperty(big, "slice", { value: slice });
    Object.defineProperty(big, "arrayBuffer", { value: arrayBuffer });

    await expect(saveBlobAsFile(big, "big.bin")).rejects.toThrow(/desktop save limit/);
    expect(slice).not.toHaveBeenCalled();
    expect(arrayBuffer).not.toHaveBeenCalled();
    expect(invoke).not.toHaveBeenCalled();
  });
});

function fakeResponse(
  bytes: Uint8Array,
  init: { contentLength?: string | null; streamParts?: Uint8Array[] | null },
): Response {
  const headers = new Headers();
  if (init.contentLength != null) {
    headers.set("Content-Length", init.contentLength);
  }
  const body =
    init.streamParts === null
      ? null
      : new ReadableStream<Uint8Array>({
          start(controller) {
            for (const part of init.streamParts ?? [bytes]) {
              controller.enqueue(part);
            }
            controller.close();
          },
        });
  return {
    headers,
    body,
    blob: async () => blobOf(bytes),
  } as unknown as Response;
}

describe("saveResponseAsFile", () => {
  beforeEach(() => {
    delete (window as Window & { _wails?: unknown })._wails;
  });
  afterEach(() => {
    delete (window as Window & { _wails?: unknown })._wails;
    vi.restoreAllMocks();
  });

  it("streams a Response body through the bridge without materializing it", async () => {
    const { saveResponseAsFile } = await loadWithClip("abc");
    const invoke = vi.fn();
    (window as Window & { _wails?: unknown })._wails = { invoke };

    const bytes = new Uint8Array([1, 2, 3, 4, 5, 6, 7]);
    const response = fakeResponse(bytes, { contentLength: "7", streamParts: [bytes.subarray(0, 3), bytes.subarray(3)] });
    await saveResponseAsFile(response, "a.bin");

    expect(invoke).toHaveBeenCalledTimes(3); // start + one coalesced chunk + end
    const [startMsg, chunkMsg, endMsg] = invoke.mock.calls.map((c) => c[0] as string);
    const chunk = JSON.parse(chunkMsg.slice("ocode:save-file-chunk:abc:".length));
    expect(atob(chunk.data)).toBe("\x01\x02\x03\x04\x05\x06\x07");
    void startMsg;
    expect(endMsg.startsWith("ocode:save-file-end:abc:")).toBe(true);
  });

  it("coalesces many tiny reads into chunk-sized bridge messages", async () => {
    const { saveResponseAsFile, SAVE_CHUNK_BYTES } = await loadWithClip("abc");
    const invoke = vi.fn();
    (window as Window & { _wails?: unknown })._wails = { invoke };

    const part = new Uint8Array(1024).fill(7);
    const parts: Uint8Array[] = [];
    // ~9 MiB of 1 KiB reads: must flush one ≥8 MiB chunk plus one remainder,
    // not thousands of tiny messages.
    const totalBytes = SAVE_CHUNK_BYTES + 1024 * 1024;
    for (let i = 0; i < totalBytes / 1024; i++) {
      parts.push(part);
    }
    const total = parts.length * 1024;
    const response = fakeResponse(new Uint8Array(total), { contentLength: String(total), streamParts: parts });
    await saveResponseAsFile(response, "big.bin");

    const chunkCount = invoke.mock.calls.length - 2;
    expect(chunkCount).toBe(2);
    let size = 0;
    for (const call of invoke.mock.calls.slice(1, -1)) {
      const chunk = JSON.parse((call[0] as string).slice("ocode:save-file-chunk:abc:".length));
      expect(chunk.seq === 0 || chunk.seq === 1).toBe(true);
      size += atob(chunk.data).length;
    }
    expect(size).toBe(total);
  });

  it("falls back to a materialized blob when Content-Length is missing", async () => {
    const { saveResponseAsFile } = await loadWithClip("abc");
    const invoke = vi.fn();
    (window as Window & { _wails?: unknown })._wails = { invoke };

    const bytes = new Uint8Array([9, 8, 7]);
    const response = fakeResponse(bytes, { contentLength: null, streamParts: [bytes] });
    await saveResponseAsFile(response, "a.bin");

    expect(invoke).toHaveBeenCalledTimes(3); // start + one blob chunk + end
    const start = JSON.parse((invoke.mock.calls[0][0] as string).slice("ocode:save-file-start:abc:".length));
    expect(start.size).toBe(3);
  });

  it("falls back to a materialized blob when the body is not streamable", async () => {
    const { saveResponseAsFile } = await loadWithClip("abc");
    const invoke = vi.fn();
    (window as Window & { _wails?: unknown })._wails = { invoke };

    const response = fakeResponse(new Uint8Array([1, 2]), { contentLength: "2", streamParts: null });
    await saveResponseAsFile(response, "a.bin");

    expect(invoke).toHaveBeenCalledTimes(3);
    const start = JSON.parse((invoke.mock.calls[0][0] as string).slice("ocode:save-file-start:abc:".length));
    expect(start.size).toBe(2);
  });

  it("rejects when the stream ends early, and sends no end message", async () => {
    const { saveResponseAsFile } = await loadWithClip("abc");
    const invoke = vi.fn();
    (window as Window & { _wails?: unknown })._wails = { invoke };

    const response = fakeResponse(new Uint8Array([1, 2, 3]), { contentLength: "10", streamParts: [new Uint8Array([1, 2, 3])] });
    await expect(saveResponseAsFile(response, "short.bin")).rejects.toThrow(/ended at 3 of 10/);
    const endMsgs = invoke.mock.calls.filter((c) => (c[0] as string).startsWith("ocode:save-file-end:"));
    expect(endMsgs).toHaveLength(0);
  });

  it("uses a download anchor in a plain browser", async () => {
    const { saveResponseAsFile } = await loadWithClip("abc");
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    const response = fakeResponse(new Uint8Array([1, 2]), { contentLength: "2", streamParts: [new Uint8Array([1, 2])] });
    await saveResponseAsFile(response, "a.bin");
    expect(click).toHaveBeenCalledTimes(1);
  });
});
