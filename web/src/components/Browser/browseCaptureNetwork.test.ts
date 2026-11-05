// Network telemetry from the embedded-browser capture bundle, in LOCAL mode.
//
// Local mode is a reverse-proxy iframe: there is no CDP session, so the page's
// own fetch/XHR wrappers are the only source of request/response headers and
// bodies. Two failure classes this file pins:
//   1. a row that carries no requestId/headers cannot expand in the DevConsole
//      (the reported "bottom network row cannot expand" bug);
//   2. Resource Timing reports the SAME request the wrappers already reported,
//      but under the rerouted /b/ URL — a raw string dedupe never matches it and
//      every fetch appears twice.
//
// The bundle is Go-embedded (internal/browse/capture.js) and loaded with `?raw`
// so a broken hook cannot pass by matching strings elsewhere. It is evaluated
// ONCE (it wraps fetch/XHR on every evaluation, so re-evaluating would double
// every post) with stubs installed first.
import { beforeAll, afterAll, beforeEach, describe, expect, it, vi } from "vitest";
import captureBundle from "../../../../internal/browse/capture.js?raw";

const STATE_KEY = "side:chat:abc";
const DOC_PATH = `/b/${STATE_KEY}/https/example.test/`;
const PAGE_URL = "https://api.example.test/collect";

type Post = Record<string, unknown>;

const posts: Post[] = [];
/** Every call the capture wrapper forwarded to the page's own sendBeacon stub. */
const beacons: Array<{ url: string; data: unknown }> = [];
let fetchResponse: unknown = null;
// When set, the stubbed fetch holds its response until the gate resolves.
let fetchGate: Promise<void> | null = null;
const originalFetch = window.fetch;
const originalXHR = window.XMLHttpRequest;

/** Absolute route-URL Resource Timing reports for an upstream request. */
function routed(url: string): string {
  const u = new URL(url);
  return `${location.origin}/b/${STATE_KEY}/${u.protocol.replace(":", "")}/${u.host}${u.pathname}${u.search}`;
}

interface FakeHeadersInit {
  ct?: string;
  headers?: Record<string, string>;
  body?: string;
  /** Body stream of each clone; the chunked (no content-length) read path. */
  bodyStream?: () => ReadableStream<Uint8Array>;
}

const encoder = new TextEncoder();

/** A stream that delivers the given chunks and then closes. */
function streamOf(chunks: string[]): ReadableStream<Uint8Array> {
  return new ReadableStream<Uint8Array>({
    start(c) {
      for (const ch of chunks) c.enqueue(encoder.encode(ch));
      c.close();
    },
  });
}

function fakeResponse({ ct = "application/json", headers = {}, body = '{"ok":true}', bodyStream }: FakeHeadersInit = {}) {
  const map: Record<string, string> = { ...headers };
  if (ct) map["content-type"] = ct;
  // Every clone reads through the stream (the bounded readCapped path); the
  // default is the whole body as one chunk.
  const clone = vi.fn(() => ({ body: bodyStream ? bodyStream() : streamOf([body]) }));
  return {
    status: 200,
    type: "default",
    headers: {
      get: (name: string) => map[name.toLowerCase()] ?? null,
      forEach: (cb: (v: string, k: string) => void) => {
        for (const [k, v] of Object.entries(map)) cb(v, k);
      },
    },
    clone,
  };
}

function opaqueResponse() {
  return {
    status: 0,
    type: "opaque",
    headers: { get: () => null, forEach: () => {} },
    clone: vi.fn(() => {
      throw new Error("an opaque response body must not be read");
    }),
  };
}

class FakeXHR {
  method = "";
  url = "";
  sent: unknown = undefined;
  status = 200;
  responseType = "";
  responseText = "";
  private headers: Record<string, string> = {};
  private listeners: Record<string, Array<() => void>> = {};
  private responseHeaders: Record<string, string> = { "content-type": "application/json", "x-req": "7", "content-length": "12" };

  open(method: string, url: string) {
    this.method = method;
    this.url = url;
  }
  setRequestHeader(k: string, v: string) {
    this.headers[k] = v;
  }
  send(body: unknown) {
    this.sent = body;
  }
  addEventListener(type: string, fn: () => void) {
    (this.listeners[type] ??= []).push(fn);
  }
  getAllResponseHeaders() {
    return Object.entries(this.responseHeaders)
      .map(([k, v]) => `${k}: ${v}`)
      .join("\r\n");
  }
  getResponseHeader(name: string) {
    return this.responseHeaders[name.toLowerCase()] ?? null;
  }
  fire(type: string) {
    for (const fn of this.listeners[type] ?? []) fn();
  }
}

class FakePerformanceObserver {
  static instances: FakePerformanceObserver[] = [];
  private cb: (list: { getEntries: () => unknown[] }) => void;
  observed: Array<Record<string, unknown>> = [];
  constructor(cb: (list: { getEntries: () => unknown[] }) => void) {
    this.cb = cb;
    FakePerformanceObserver.instances.push(this);
  }
  observe(opts: Record<string, unknown>) {
    this.observed.push(opts);
  }
  emit(entries: unknown[]) {
    this.cb({ getEntries: () => entries });
  }
}

function resourceEntry(name: string, initiatorType = "img") {
  return {
    entryType: "resource",
    name,
    initiatorType,
    duration: 12.4,
    transferSize: 512,
    encodedBodySize: 400,
    startTime: 3,
    responseStatus: 200,
  };
}

/** A PerformanceNavigationTiming entry. Chromium delivers it more than once: a
 *  loading one (duration/loadEventEnd 0, but responseEnd already set) and the
 *  finished one. */
function navigationEntry(name: string, over: Record<string, unknown> = {}) {
  return {
    entryType: "navigation",
    name,
    initiatorType: "navigation",
    duration: 0,
    transferSize: 300,
    encodedBodySize: 0,
    startTime: 0,
    responseStatus: 200,
    responseStart: 8,
    responseEnd: 5,
    loadEventEnd: 0,
    ...over,
  };
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

beforeAll(() => {
  window.history.replaceState({}, "", DOC_PATH);
  (window as unknown as { __OCODE_BROWSE__: { stateKey: string; spaOrigin: string } }).__OCODE_BROWSE__ = {
    stateKey: STATE_KEY,
    spaOrigin: window.location.origin,
  };
  // capture.js posts through window.parent.postMessage; in jsdom the parent IS
  // this window, so a spy on postMessage captures every payload synchronously.
  vi.spyOn(window, "postMessage").mockImplementation(((msg: Post) => {
    posts.push(msg);
  }) as unknown as typeof window.postMessage);
  window.fetch = (async () => {
    if (fetchGate) await fetchGate;
    return fetchResponse;
  }) as unknown as typeof window.fetch;
  window.XMLHttpRequest = FakeXHR as unknown as typeof XMLHttpRequest;
  // capture.js wraps navigator.sendBeacon at eval time, so the stub must exist
  // first. jsdom does not implement the real one; this records what reached it.
  Object.defineProperty(navigator, "sendBeacon", {
    configurable: true,
    writable: true,
    value: (url: string, data?: unknown) => {
      beacons.push({ url, data });
      return true;
    },
  });
  (window as unknown as { PerformanceObserver: unknown }).PerformanceObserver = FakePerformanceObserver;
  (0, eval)(captureBundle);
});

afterAll(() => {
  window.fetch = originalFetch;
  window.XMLHttpRequest = originalXHR;
});

beforeEach(() => {
  posts.length = 0;
});

describe("browse capture network telemetry (local mode)", () => {
  it("reports request/response headers and the request body, then the text response body as its own message", async () => {
    fetchResponse = fakeResponse({ headers: { "x-req": "1", "content-length": "11" }, body: '{"ok":true}' });
    await window.fetch(PAGE_URL, {
      method: "POST",
      headers: { "content-type": "application/json", "x-trace": "abc" },
      body: '{"n":1}',
    });
    await flush();

    expect(posts).toHaveLength(2);
    const p = posts[0];
    expect(p.type).toBe("ocode:browse:network");
    expect(String(p.requestId)).toMatch(/^local-/);
    expect(p.method).toBe("POST");
    expect(p.url).toBe(PAGE_URL);
    expect(p.status).toBe(200);
    expect(p.requestHeaders).toMatchObject({ "content-type": "application/json", "x-trace": "abc" });
    expect(p.responseHeaders).toMatchObject({ "x-req": "1" });
    expect(p.postData).toBe('{"n":1}');
    expect(p.responseBody).toBeUndefined();
    expect(p.contentType).toBe("application/json");
    // The Size column reads content-length; it is absent on a chunked response.
    expect(p.size).toBe(11);

    expect(posts[1]).toMatchObject({
      type: "ocode:browse:networkbody",
      requestId: p.requestId,
      responseBody: '{"ok":true}',
      responseBodyTruncated: false,
    });
  });

  it("posts the row before the response body has been read", async () => {
    const clone = vi.fn(() => ({ body: new ReadableStream<Uint8Array>({ pull: () => new Promise<void>(() => {}) }) }));
    fetchResponse = { ...fakeResponse({ headers: { "content-length": "11" } }), clone };
    await window.fetch(PAGE_URL);

    expect(posts).toHaveLength(1);
    expect(posts[0].type).toBe("ocode:browse:network");
    expect(posts[0].status).toBe(200);
    expect(clone).toHaveBeenCalledTimes(1);
  });

  it("never clones a text/event-stream response, even with a content-length", async () => {
    const res = fakeResponse({ ct: "text/event-stream", headers: { "content-length": "11" }, body: "data: hi\n\n" });
    fetchResponse = res;
    await window.fetch(PAGE_URL);
    await flush();

    expect(posts).toHaveLength(1);
    expect(posts[0].type).toBe("ocode:browse:network");
    expect(posts[0].contentType).toBe("text/event-stream");
    expect(res.clone).not.toHaveBeenCalled();
  });

  it("captures a chunked JSON body that fits under the cap, as its own message", async () => {
    const url = "https://api.example.test/chunked-small";
    const res = fakeResponse({ ct: "application/json", bodyStream: () => streamOf(['{"chunk":', "1}"]) });
    fetchResponse = res;
    await window.fetch(url);
    await vi.waitFor(() => expect(posts).toHaveLength(2));

    expect(posts[0].type).toBe("ocode:browse:network");
    expect(posts[0].responseBody).toBeUndefined();
    expect(posts[1]).toMatchObject({
      type: "ocode:browse:networkbody",
      requestId: posts[0].requestId,
      responseBody: '{"chunk":1}',
      responseBodyTruncated: false,
    });
    expect(res.clone).toHaveBeenCalledTimes(1);
  });

  it("stops a chunked body at the cap, marks it truncated, and cancels the rest of the stream", async () => {
    const url = "https://api.example.test/chunked-endless";
    let pulled = 0;
    let cancelled = false;
    const endless = () =>
      new ReadableStream<Uint8Array>({
        pull(c) {
          pulled++;
          c.enqueue(encoder.encode("a".repeat(64 * 1024)));
        },
        cancel() {
          cancelled = true;
        },
      });
    fetchResponse = fakeResponse({ ct: "text/plain", bodyStream: endless });
    await window.fetch(url);
    await vi.waitFor(() => expect(posts).toHaveLength(2));

    const body = posts[1];
    expect(body.type).toBe("ocode:browse:networkbody");
    expect(String(body.responseBody)).toHaveLength(256 * 1024);
    expect(body.responseBodyTruncated).toBe(true);
    expect(cancelled).toBe(true);
    // Five 64 KB chunks pass the cap; the reader must not keep pulling.
    expect(pulled).toBeLessThanOrEqual(7);
  });

  it("posts the row of a chunked response whose body never arrives", async () => {
    const res = fakeResponse({
      ct: "application/json",
      bodyStream: () => new ReadableStream<Uint8Array>({ pull: () => new Promise<void>(() => {}) }),
    });
    fetchResponse = res;
    await window.fetch("https://api.example.test/chunked-stalled");
    await flush();

    expect(posts).toHaveLength(1);
    expect(posts[0].type).toBe("ocode:browse:network");
    expect(res.clone).toHaveBeenCalledTimes(1);
  });

  it.each(["text/event-stream", "application/x-ndjson", "application/stream+json"])(
    "never reads a %s response, even without a content-length",
    async (ct) => {
      const res = fakeResponse({ ct, bodyStream: () => streamOf(["data: hi\n\n"]) });
      fetchResponse = res;
      await window.fetch(`https://api.example.test/stream/${encodeURIComponent(ct)}`);
      await flush();

      expect(posts).toHaveLength(1);
      expect(posts[0].type).toBe("ocode:browse:network");
      expect(posts[0].contentType).toBe(ct);
      expect(res.clone).not.toHaveBeenCalled();
    },
  );

  it("serializes URLSearchParams, FormData, Blob and binary request bodies honestly", async () => {
    fetchResponse = fakeResponse();
    const form = new FormData();
    form.append("k", "v");
    form.append("f", new File(["abc"], "a.txt"));
    const cases: Array<[unknown, string]> = [
      [new URLSearchParams({ a: "1", b: "2" }), "a=1&b=2"],
      [form, "k=v&f=[File a.txt 3]"],
      [new Blob(["hello"], { type: "text/plain" }), "[Blob text/plain 5]"],
      [new ArrayBuffer(4), "[binary 4 bytes]"],
      [new Uint8Array([1, 2, 3]), "[binary 3 bytes]"],
      [{ plain: true }, '{"plain":true}'],
    ];
    for (const [body, expected] of cases) {
      posts.length = 0;
      await window.fetch(PAGE_URL, { method: "POST", body: body as BodyInit });
      await flush();
      expect(posts[0].postData).toBe(expected);
    }
  });

  it("reads a [name, value] pair header list and shows a stream body as a placeholder", async () => {
    fetchResponse = fakeResponse();
    await window.fetch(PAGE_URL, {
      method: "POST",
      headers: [["x-pair", "1"]],
      body: new ReadableStream<Uint8Array>() as unknown as BodyInit,
    });
    await flush();
    expect(posts[0].requestHeaders).toEqual({ "x-pair": "1" });
    expect(posts[0].postData).toBe("[stream]");
  });

  it("logs a request body it cannot render and posts the row without one", async () => {
    const debug = vi.spyOn(console, "debug").mockImplementation(() => {});
    fetchResponse = fakeResponse();
    const cyclic: Record<string, unknown> = {};
    cyclic.self = cyclic;
    await window.fetch(PAGE_URL, { method: "POST", body: cyclic as unknown as BodyInit });
    await flush();
    expect(posts[0].postData).toBeUndefined();
    expect(debug).toHaveBeenCalledWith(expect.stringContaining(`POST ${PAGE_URL}`), expect.any(TypeError));
    debug.mockRestore();
  });

  it("logs a response body that fails mid-stream and leaves the row without a body", async () => {
    const debug = vi.spyOn(console, "debug").mockImplementation(() => {});
    fetchResponse = fakeResponse({
      ct: "text/plain",
      bodyStream: () =>
        new ReadableStream<Uint8Array>({
          pull: (c) => {
            c.error(new Error("connection reset"));
          },
        }),
    });
    await window.fetch(PAGE_URL);
    await flush();
    expect(posts.map((p) => p.type)).toEqual(["ocode:browse:network"]);
    expect(debug).toHaveBeenCalledWith(expect.stringContaining(`GET ${PAGE_URL}`), expect.any(Error));
    debug.mockRestore();
  });

  it("redacts credential headers and bounds header values and count", async () => {
    fetchResponse = fakeResponse();
    const long = "v".repeat(400);
    const headers: Record<string, string> = {
      authorization: "Bearer super-secret",
      "x-api-key": "key-123",
      cookie: "sid=abc",
      "x-long": long,
    };
    for (let i = 0; i < 30; i++) headers[`x-filler-${i}`] = "f";
    await window.fetch(PAGE_URL, { headers });
    await flush();

    const req = posts[0].requestHeaders as Record<string, string>;
    expect(req.authorization).toBe("[redacted]");
    expect(req["x-api-key"]).toBe("[redacted]");
    expect(req.cookie).toBe("[redacted]");
    expect(req["x-long"].length).toBeLessThanOrEqual(201);
    expect(Object.keys(req).length).toBeLessThanOrEqual(20);
  });

  it("keeps the content type but does not decode a binary response body", async () => {
    const res = fakeResponse({ ct: "image/png", body: "not-read" });
    fetchResponse = res;
    await window.fetch("https://cdn.example.test/pic.png");
    await flush();

    expect(posts).toHaveLength(1);
    expect(posts[0].contentType).toBe("image/png");
    expect(posts[0].responseBody).toBeUndefined();
    expect(res.clone).not.toHaveBeenCalled();
  });

  it("reports an opaque cross-origin response without inventing headers or a body", async () => {
    fetchResponse = opaqueResponse();
    await window.fetch("https://ads.example.test/collect", { method: "POST", body: "x=1" });
    await flush();

    expect(posts).toHaveLength(1);
    expect(posts[0].status).toBe(0);
    expect(posts[0].responseHeaders).toBeUndefined();
    expect(posts[0].responseBody).toBeUndefined();
    // The request body is known locally even when the response is opaque.
    expect(posts[0].postData).toBe("x=1");
  });

  it("does not duplicate a wrapped fetch as a Resource Timing row", async () => {
    fetchResponse = fakeResponse();
    const url = "https://api.example.test/deduped";
    await window.fetch(url);
    await flush();
    const networkRows = () => posts.filter((p) => p.type === "ocode:browse:network");
    expect(networkRows()).toHaveLength(1);

    // Resource Timing reports the URL actually fetched — the rerouted one.
    const observer = FakePerformanceObserver.instances[0];
    observer.emit([resourceEntry(routed(url), "fetch")]);
    expect(networkRows()).toHaveLength(1);
  });

  it("reports a resource the wrappers cannot see (script/img markup)", () => {
    const observer = FakePerformanceObserver.instances[0];
    const url = routed("https://cdn.example.test/markup-only.js");
    observer.emit([resourceEntry(url, "script")]);

    expect(posts).toHaveLength(1);
    const p = posts[0];
    expect(p.url).toBe(url);
    // Resource Timing carries no method; the row says unknown, not a guessed one.
    expect(p.method).toBe("-");
    expect(p.status).toBe(200);
    expect(p.size).toBe(512);
  });

  it("reports the document once, from the finished navigation entry", () => {
    const observer = FakePerformanceObserver.instances[0];
    const docUrl = routed("https://example.test/");
    // The still-loading delivery already carries responseEnd, but it has no
    // meaningful duration yet; posting it would show the page as a 0 ms row.
    observer.emit([navigationEntry(docUrl)]);
    expect(posts).toHaveLength(0);

    observer.emit([
      navigationEntry(docUrl, { duration: 17, transferSize: 509, encodedBodySize: 209, responseEnd: 9, loadEventEnd: 17 }),
    ]);
    expect(posts).toHaveLength(1);
    expect(posts[0]).toMatchObject({
      type: "ocode:browse:network",
      method: "-",
      url: docUrl,
      status: 200,
      durationMs: 17,
      size: 509,
    });

    // A later delivery of the same entry must not add a second document row.
    observer.emit([
      navigationEntry(docUrl, { duration: 20, transferSize: 509, encodedBodySize: 209, responseEnd: 9, loadEventEnd: 20 }),
    ]);
    expect(posts).toHaveLength(1);
  });

  it("reports XHR headers, request body and text response body", async () => {
    const xhr = new window.XMLHttpRequest() as unknown as FakeXHR;
    xhr.open("POST", PAGE_URL);
    xhr.setRequestHeader("content-type", "application/json");
    xhr.setRequestHeader("authorization", "Bearer xhr-secret");
    xhr.send('{"xhr":true}');
    xhr.responseText = '{"done":true}';
    xhr.fire("loadend");

    expect(posts).toHaveLength(1);
    const p = posts[0];
    expect(String(p.requestId)).toMatch(/^local-/);
    expect(p.method).toBe("POST");
    expect(p.url).toBe(PAGE_URL);
    expect(p.requestHeaders).toMatchObject({ "content-type": "application/json", authorization: "[redacted]" });
    expect(p.responseHeaders).toMatchObject({ "content-type": "application/json", "x-req": "7" });
    expect(p.postData).toBe('{"xhr":true}');
    expect(p.responseBody).toBe('{"done":true}');
    expect(p.size).toBe(12);

    // The rerouted URL is what the browser actually requests.
    expect(xhr.url).toBe(routed(PAGE_URL));
  });

  it("serializes a non-string XHR request body honestly", () => {
    const xhr = new window.XMLHttpRequest() as unknown as FakeXHR;
    xhr.open("POST", PAGE_URL);
    xhr.send(new URLSearchParams({ q: "x" }));
    xhr.fire("loadend");

    expect(posts).toHaveLength(1);
    expect(posts[0].postData).toBe("q=x");
  });

  describe("navigator.sendBeacon", () => {
    const BEACON_URL = "https://analytics.example.test/g/collect?v=2&tid=G-TEST";

    it("reports a beacon with its real method and body, and still forwards the call", () => {
      beacons.length = 0;
      const sent = navigator.sendBeacon(BEACON_URL, "en=page_view&ep.x=1");

      expect(sent).toBe(true);
      // The page's own beacon must still be the one that actually goes out.
      expect(beacons).toEqual([{ url: BEACON_URL, data: "en=page_view&ep.x=1" }]);

      expect(posts).toHaveLength(1);
      const p = posts[0];
      expect(p.type).toBe("ocode:browse:network");
      expect(String(p.requestId)).toMatch(/^local-/);
      expect(p.method).toBe("POST");
      expect(p.url).toBe(BEACON_URL);
      // A beacon's response is discarded by the browser; the row says so
      // instead of inventing a status or headers.
      expect(p.status).toBe(0);
      expect(p.requestHeaders).toBeUndefined();
      expect(p.responseHeaders).toBeUndefined();
      expect(p.responseBody).toBeUndefined();
      expect(p.postData).toBe("en=page_view&ep.x=1");
    });

    it("does not report the beacon a second time as an anonymous Resource Timing row", () => {
      beacons.length = 0;
      navigator.sendBeacon(BEACON_URL, "ping");
      expect(posts).toHaveLength(1);

      // Chrome reports a beacon's timing entry with initiatorType "beacon".
      FakePerformanceObserver.instances[0].emit([resourceEntry(BEACON_URL, "beacon")]);
      expect(posts).toHaveLength(1);
    });
  });

  describe("Resource Timing dedupe", () => {
    const networkRows = () => posts.filter((p) => p.type === "ocode:browse:network");
    const observer = () => FakePerformanceObserver.instances[0];
    /** The URL Resource Timing names for a bare-relative request, as the browser resolves it. */
    const resolved = (url: string) => new URL(url, location.href).href;

    it("dedupes the timing entry of a fetch that runs longer than 5 s", async () => {
      vi.useFakeTimers({ toFake: ["Date"] });
      try {
        const url = "https://api.example.test/long-fetch";
        let release!: () => void;
        fetchGate = new Promise<void>((resolve) => {
          release = resolve;
        });
        fetchResponse = fakeResponse({ body: '{"slow":true}' });
        const pending = window.fetch(url);
        vi.setSystemTime(Date.now() + 6000);
        release();
        await pending;
        await flush();
        expect(networkRows()).toHaveLength(1);

        // The entry is queued when the response ends, after the wrapper completed.
        observer().emit([resourceEntry(routed(url), "fetch")]);
        expect(networkRows()).toHaveLength(1);
      } finally {
        fetchGate = null;
        vi.useRealTimers();
      }
    });

    it("shows a later request to the same URL after a fetch's timing entry was consumed", async () => {
      const url = "api/consumed-then-markup.js";
      fetchResponse = fakeResponse({ ct: "text/plain", body: "x" });
      await window.fetch(url);
      await flush();
      observer().emit([resourceEntry(resolved(url), "fetch")]);
      expect(networkRows()).toHaveLength(1);

      // A markup request (<img>/<script>) for the same URL is a new request.
      observer().emit([resourceEntry(resolved(url), "script")]);
      expect(networkRows()).toHaveLength(2);
      expect(networkRows()[1].method).toBe("-");
      expect(networkRows()[1].url).toBe(resolved(url));
    });

    it("does not swallow a markup request for a URL a fetch is still loading", async () => {
      const url = "api/in-flight.js";
      let release!: () => void;
      fetchGate = new Promise<void>((resolve) => {
        release = resolve;
      });
      fetchResponse = fakeResponse({ ct: "text/plain", body: "x" });
      try {
        const pending = window.fetch(url);
        observer().emit([resourceEntry(resolved(url), "img")]);
        expect(networkRows()).toHaveLength(1);

        release();
        await pending;
        await flush();
        expect(networkRows()).toHaveLength(2);

        // The fetch's own entry is still matched by its wrapper record.
        observer().emit([resourceEntry(resolved(url), "fetch")]);
        expect(networkRows()).toHaveLength(2);
      } finally {
        fetchGate = null;
      }
    });

    it("stops matching a completed fetch once the grace period after completion has passed", async () => {
      vi.useFakeTimers({ toFake: ["Date"] });
      try {
        const url = "api/grace-expired.json";
        fetchResponse = fakeResponse({ ct: "text/plain", body: "x" });
        await window.fetch(url);
        await flush();
        expect(networkRows()).toHaveLength(1);

        vi.setSystemTime(Date.now() + 3000);
        observer().emit([resourceEntry(resolved(url), "fetch")]);
        expect(networkRows()).toHaveLength(2);
      } finally {
        vi.useRealTimers();
      }
    });
  });
});
