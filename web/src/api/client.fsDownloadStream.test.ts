import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./client";

function captureFetch(response: Response) {
  const calls: [string, RequestInit][] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push([String(input), init ?? {}]);
      return response;
    }),
  );
  return calls;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("api.fsDownloadStream", () => {
  it("GETs the file with project and path query args and returns the streaming Response", async () => {
    const calls = captureFetch(new Response("file-bytes", { status: 200, headers: { "Content-Length": "10" } }));

    const res = await api.fsDownloadStream("src/app.ts", "/proj");

    // The body is NOT read into a blob here: callers stream it (saveResponseAsFile).
    expect(res).toBeInstanceOf(Response);
    expect(await res.text()).toBe("file-bytes");
    expect(calls).toHaveLength(1);
    const [url, init] = calls[0];
    expect(url).toContain("/api/fs/download?");
    expect(url).toContain("path=src%2Fapp.ts");
    expect(url).toContain("project=%2Fproj");
    expect(url).not.toContain("host=");
    expect(init.method).toBeUndefined();
  });

  it("carries the host so a remote project's file is read on that host", async () => {
    const calls = captureFetch(new Response("x", { status: 200 }));
    await api.fsDownloadStream("a.txt", "/home/x/proj", "me@host");
    expect(calls[0][0]).toContain("host=me%40host");
  });

  it("surfaces the server's error message instead of a parse failure", async () => {
    captureFetch(
      new Response('{"error":"only regular files can be downloaded"}', {
        status: 400,
        headers: { "Content-Type": "application/json" },
      }),
    );
    await expect(api.fsDownloadStream("folder", "/proj")).rejects.toThrow("only regular files can be downloaded");
  });

  it("surfaces the status text when the error body is not JSON", async () => {
    captureFetch(new Response("upstream exploded", { status: 502, statusText: "Bad Gateway" }));
    await expect(api.fsDownloadStream("a.bin", "/proj")).rejects.toThrow();
  });
});
