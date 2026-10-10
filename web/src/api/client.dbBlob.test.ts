import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { api } from "./client";

/**
 * The BLOB cell endpoints. Two things matter here and are easy to get wrong:
 *
 *  - a download returns RAW bytes, not JSON, so the response must not be
 *    parsed; and a 204 (a NULL cell) must be distinguishable from a 200 with an
 *    empty body (an empty file).
 *  - an upload sends the bytes as the request body verbatim, with a
 *    Content-Type of application/octet-stream and no multipart wrapping, so the
 *    server stores exactly what the user picked.
 */

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("api.dbBlobDownload", () => {
  const fetchMock = vi.fn();

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockReset();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("returns the bytes and the media type", async () => {
    const bytes = new Uint8Array([1, 2, 3]);
    fetchMock.mockResolvedValue(
      new Response(bytes, { status: 200, headers: { "Content-Type": "image/png" } }),
    );

    const res = await api.dbBlobDownload("app.db", "files", "data", { id: 1 });
    expect(res.isNull).toBe(false);
    expect(res.mediaType).toBe("image/png");
    expect(Array.from(new Uint8Array(res.data!))).toEqual([1, 2, 3]);
  });

  it("reports a NULL cell as isNull rather than an empty file", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204, headers: {} }));

    const res = await api.dbBlobDownload("app.db", "files", "data", { id: 2 });
    expect(res.isNull).toBe(true);
    expect(res.byteLength).toBe(0);
  });

  it("sends the key as JSON and threads projectRoot and host", async () => {
    fetchMock.mockResolvedValue(new Response(new Uint8Array(), { status: 200 }));

    // 2^53-1 is the largest integer JavaScript represents exactly. A larger id
    // is already rounded by the time the grid receives it from the server, so
    // it cannot reach this function intact — see the TODO on DBCell keys.
    await api.dbBlobDownload("app.db", "files", "data", { id: 9007199254740991 }, {
      projectRoot: "/proj",
      host: "user@box",
    });

    const url = fetchMock.mock.calls[0][0] as string;
    expect(url).toContain("/api/remote/user%40box/api/db/blob");
    expect(decodeURIComponent(url)).toContain('"id":9007199254740991');
    expect(decodeURIComponent(url)).toContain("project_root=/proj");
  });

  it("raises an ApiError carrying the status", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ error: "cell is not a BLOB" }, 400));

    await expect(api.dbBlobDownload("app.db", "files", "data", { id: 1 })).rejects.toMatchObject({
      status: 400,
    });
  });
});

describe("api.dbBlobUpload", () => {
  const fetchMock = vi.fn();

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockReset();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("posts the raw bytes with no multipart wrapping", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ rows_affected: 1, elapsed_ms: 1 }));

    const payload = new Uint8Array([0xde, 0xad]);
    await api.dbBlobUpload("app.db", "files", "data", { id: 1 }, payload);

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(decodeURIComponent(url)).toContain('key={"id":1}');
    expect(init.method).toBe("POST");
    const headers = new Headers(init.headers as HeadersInit);
    expect(headers.get("Content-Type")).toBe("application/octet-stream");
    // The body is the bytes themselves — not FormData (which would wrap them in
    // multipart framing and change the stored value) and not base64 JSON.
    expect(init.body).toBeInstanceOf(Uint8Array);
    expect(Array.from(init.body as Uint8Array)).toEqual([0xde, 0xad]);
  });

  it("surfaces a 413 as an ApiError so the UI can explain the size limit", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ error: "too large" }, 413));

    await expect(
      api.dbBlobUpload("app.db", "files", "data", { id: 1 }, new Uint8Array(4)),
    ).rejects.toMatchObject({ status: 413 });
  });
});