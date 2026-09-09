import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", () => ({
  apiPath: (path: string) => path,
  authHeaders: () => ({ Authorization: "Bearer test" }),
}));

import { restoreTerminalHistory, TerminalHistoryError } from "./terminalHistory";

function base64(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

function page(id: string, offset: number, data: Uint8Array, snapshotEnd: number, eof = false) {
  return {
    id,
    offset,
    next_offset: offset + data.length,
    snapshot_end: snapshotEnd,
    eof,
    data: base64(data),
    state: "exited",
  };
}

describe("restoreTerminalHistory", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("replays ordered pages and pins later requests to the first snapshot", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(page("t1", 0, new TextEncoder().encode("hello"), 11))))
      .mockResolvedValueOnce(new Response(JSON.stringify(page("t1", 5, new TextEncoder().encode(" world"), 11, true))));
    vi.stubGlobal("fetch", fetchMock);
    const texts: string[] = [];
    const result = await restoreTerminalHistory({
      id: "t1",
      projectPath: "/project",
      pageSize: 5,
      onText: (text) => { texts.push(text); },
    });

    expect(result).toEqual({ kind: "restored", snapshotEnd: 11, state: "exited" });
    expect(texts.join("")).toBe("hello world");
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(String(fetchMock.mock.calls[1][0])).toContain("offset=5");
    expect(String(fetchMock.mock.calls[1][0])).toContain("snapshot_end=11");
    expect(fetchMock.mock.calls[0][1]).toEqual(expect.objectContaining({ headers: { Authorization: "Bearer test" } }));
  });

  it("handles an empty exact-boundary snapshot without looping", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify(page("t1", 0, new Uint8Array(), 0, true))));
    vi.stubGlobal("fetch", fetchMock);
    const result = await restoreTerminalHistory({ id: "t1", projectPath: "/project" });
    expect(result.kind).toBe("restored");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("rejects an empty non-EOF page that makes no progress", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify(page("t1", 0, new Uint8Array(), 1))));
    vi.stubGlobal("fetch", fetchMock);

    await expect(restoreTerminalHistory({ id: "t1", projectPath: "/project" }))
      .rejects.toThrow("terminal history made no progress");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("preserves UTF-8 characters split across page boundaries", async () => {
    const utf8 = new TextEncoder().encode("café");
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(page("t1", 0, utf8.slice(0, 4), utf8.length))))
      .mockResolvedValueOnce(new Response(JSON.stringify(page("t1", 4, utf8.slice(4), utf8.length, true))));
    vi.stubGlobal("fetch", fetchMock);
    const texts: string[] = [];
    await restoreTerminalHistory({ id: "t1", projectPath: "/project", pageSize: 4, onText: (text) => { texts.push(text); } });
    expect(texts.join(" ")).toBe("caf é");
  });

  it("can carry a UTF-8 sequence from the history cursor into WebSocket bytes", async () => {
    const utf8 = new TextEncoder().encode("é");
    const decoder = new TextDecoder();
    const fetchMock = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify(page("t1", 0, utf8.slice(0, 1), 1, true))));
    vi.stubGlobal("fetch", fetchMock);
    const texts: string[] = [];
    await restoreTerminalHistory({
      id: "t1",
      projectPath: "/project",
      decoder,
      onText: (text) => { texts.push(text); },
    });
    expect(texts).toEqual([]);
    expect(decoder.decode(utf8.slice(1), { stream: true })).toBe("é");
  });

  it("returns an explicit missing result for the localStorage fallback path", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(new Response("not found", { status: 404 })));
    await expect(restoreTerminalHistory({ id: "t1", projectPath: "/project" })).resolves.toEqual({ kind: "missing", snapshotEnd: 0 });
  });

  it("does not append local fallback after a page has already been restored", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(page("t1", 0, new Uint8Array([1]), 2))))
      .mockResolvedValueOnce(new Response("gone", { status: 404 }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(restoreTerminalHistory({ id: "t1", projectPath: "/project" })).rejects.toThrow("disappeared");
  });

  it("rejects non-contiguous offsets and malformed base64 instead of truncating", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({
      ...page("t1", 1, new Uint8Array([1]), 2),
      offset: 0,
      data: "%%%",
    }))));
    await expect(restoreTerminalHistory({ id: "t1", projectPath: "/project" })).rejects.toBeInstanceOf(TerminalHistoryError);
  });

  it("rejects a changing snapshot cursor", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(page("t1", 0, new Uint8Array([1]), 2))))
      .mockResolvedValueOnce(new Response(JSON.stringify(page("t1", 1, new Uint8Array([2]), 3, true))));
    vi.stubGlobal("fetch", fetchMock);
    await expect(restoreTerminalHistory({ id: "t1", projectPath: "/project" })).rejects.toThrow("snapshot changed");
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("passes abort cancellation through to an in-flight page request", async () => {
    const controller = new AbortController();
    const fetchMock = vi.fn((_url: string, init?: RequestInit) =>
      new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true });
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const restore = restoreTerminalHistory({ id: "t1", projectPath: "/project", signal: controller.signal });
    controller.abort();
    await expect(restore).rejects.toMatchObject({ name: "AbortError" });
  });
});
