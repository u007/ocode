import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./client";

// A pasted OS file travels as RAW BYTES. The trap this pins: `fetchJSON` sets
// Content-Type: application/json by default, so routing the upload through it
// would both JSON-encode the body and mislabel it — the server would reject the
// request or write a JSON wrapper instead of the file.
function jsonResponse(body: string, status = 200): Response {
  return new Response(body, { status, headers: { "Content-Type": "application/json" } });
}

type FetchCall = [string, RequestInit];

function captureFetch(response: Response = jsonResponse('{"success":true,"path":"/proj/a.png"}')) {
  const calls: FetchCall[] = [];
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

describe("api.fsUpload", () => {
  it("posts the File itself with an octet-stream content type and query args", async () => {
    const calls = captureFetch();
    const file = new File(["png-bytes"], "photo.png", { type: "image/png" });

    const res = await api.fsUpload("/proj/src", file, "/proj");

    expect(res).toEqual({ success: true, path: "/proj/a.png" });
    expect(calls).toHaveLength(1);
    const [url, init] = calls[0];
    expect(url).toContain("/api/fs/upload?");
    expect(url).toContain("dest_dir=%2Fproj%2Fsrc");
    expect(url).toContain("name=photo.png");
    expect(url).toContain("project=%2Fproj");
    expect(init.method).toBe("POST");
    // The File is the body, untouched — not a JSON string.
    expect(init.body).toBe(file);
    const headers = new Headers(init.headers as HeadersInit);
    expect(headers.get("Content-Type")).toBe("application/octet-stream");
  });

  it("carries the host so a remote project's upload routes to that host", async () => {
    const calls = captureFetch();
    await api.fsUpload("/home/x/proj", new File(["a"], "a.txt"), "/home/x/proj", "me@host");
    expect(calls[0][0]).toContain("host=me%40host");
  });

  it("surfaces the server's error message instead of a parse failure", async () => {
    captureFetch(jsonResponse('{"error":"name must not contain a path separator"}', 400));
    await expect(api.fsUpload("/proj", new File(["a"], "a.txt"), "/proj")).rejects.toThrow(
      "name must not contain a path separator",
    );
  });

  it("fails loudly on a 200 with no body instead of claiming the paste succeeded", async () => {
    // A 200 from something that is not this endpoint (proxy / SPA fallback) must
    // not be reported as a successful write.
    captureFetch(new Response("", { status: 200 }));
    await expect(api.fsUpload("/proj", new File(["a"], "a.txt"), "/proj")).rejects.toThrow(
      "upload returned an empty response",
    );
  });

  it("hands the abort signal to fetch so a superseded upload can be cancelled", async () => {
    const calls = captureFetch();
    const controller = new AbortController();

    await api.fsUpload("/proj", new File(["a"], "a.txt"), "/proj", undefined, controller.signal);

    expect(calls[0][1].signal).toBe(controller.signal);
  });

  it("rejects with AbortError when the signal aborts the in-flight request", async () => {
    // Mirrors fetch: the request rejects once its signal aborts. If fsUpload
    // dropped the signal, this request would never settle.
    vi.stubGlobal(
      "fetch",
      vi.fn(
        (_input: RequestInfo | URL, init?: RequestInit) =>
          new Promise((_resolve, reject) => {
            init?.signal?.addEventListener("abort", () =>
              reject(new DOMException("The operation was aborted.", "AbortError")),
            );
          }),
      ),
    );
    const controller = new AbortController();

    const pending = api.fsUpload("/proj", new File(["a"], "a.txt"), "/proj", undefined, controller.signal);
    controller.abort();

    await expect(pending).rejects.toMatchObject({ name: "AbortError" });
  });
});
