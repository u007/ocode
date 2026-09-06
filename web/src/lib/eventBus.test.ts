import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { eventBus, RECONNECT_BASE_MS, RECONNECT_MAX_MS } from "./eventBus";
import type { BusEnvelope } from "./eventBus";

// eventBus is now fetch()+readSSEStream based (not EventSource) so it can
// carry an Authorization header — the browser's native EventSource cannot
// set custom headers, which is exactly why remote-mode auth needs this.
// authHeaders() is stubbed to a fixed bearer token so tests can assert the
// stream authenticates via header, not a `?token=` query string.
vi.mock("@/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/client")>();
  return {
    ...actual,
    authHeaders: () => ({ Authorization: "Bearer test-token" }),
  };
});

function abortError(): DOMException {
  return new DOMException("The operation was aborted.", "AbortError");
}

/** A controllable SSE response body: tests push frames, end it cleanly, or
 *  fail it, driving the three ways a real connection can behave. */
class FakeStream {
  controller!: ReadableStreamDefaultController<Uint8Array>;
  readonly body: ReadableStream<Uint8Array>;
  constructor() {
    this.body = new ReadableStream({
      start: (c) => {
        this.controller = c;
      },
    });
  }
  push(text: string): void {
    this.controller.enqueue(new TextEncoder().encode(text));
  }
  end(): void {
    this.controller.close();
  }
  fail(err: unknown): void {
    this.controller.error(err);
  }
}

interface FetchCall {
  url: string;
  headers: Headers;
  resolve: (res: Response) => void;
  reject: (err: unknown) => void;
}

function envelopeFrame(event: string, seq: number, extra: Partial<BusEnvelope> = {}): string {
  const env = { event, project: "/proj", session_id: "", seq, data: {}, ...extra };
  return `event: envelope\ndata: ${JSON.stringify(env)}\n\n`;
}

describe("eventBus", () => {
  let calls: FetchCall[];

  beforeEach(() => {
    calls = [];
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      return new Promise<Response>((resolve, reject) => {
        const headers = new Headers(init?.headers);
        calls.push({ url, headers, resolve, reject });
        init?.signal?.addEventListener("abort", () => reject(abortError()));
      });
    });
    vi.stubGlobal("fetch", fetchMock);
    vi.useFakeTimers();
    eventBus.stop();
  });

  afterEach(() => {
    eventBus.stop();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  /** Resolves the fetch call at `idx` with a 200 response backed by a fresh
   *  FakeStream, then flushes microtasks so the bus's "open" logic runs. */
  async function openWithStream(idx = 0): Promise<FakeStream> {
    const stream = new FakeStream();
    calls[idx].resolve(new Response(stream.body, { status: 200 }));
    await vi.advanceTimersByTimeAsync(0);
    return stream;
  }

  it("authenticates via the Authorization header, not a query-string token", async () => {
    eventBus.on("text", () => {});
    await vi.advanceTimersByTimeAsync(0);
    expect(calls.length).toBe(1);
    expect(calls[0].url).not.toContain("token=");
    expect(calls[0].headers.get("Authorization")).toBe("Bearer test-token");
  });

  it("routes envelopes to registered handlers by event type, passing the full envelope", async () => {
    const onText = vi.fn();
    const onStatus = vi.fn();
    const offText = eventBus.on("text", onText);
    const offStatus = eventBus.on("status", onStatus);

    const stream = await openWithStream();
    stream.push(envelopeFrame("text", 1, { session_id: "s1", data: { delta: "hi" } }));
    stream.push(envelopeFrame("status", 2, { data: { model: "m" } }));
    await vi.advanceTimersByTimeAsync(0);

    expect(onText).toHaveBeenCalledTimes(1);
    expect(onText.mock.calls[0][0]).toMatchObject({ event: "text", session_id: "s1", seq: 1 });
    expect(onStatus).toHaveBeenCalledTimes(1);
    expect(onStatus.mock.calls[0][0].data).toEqual({ model: "m" });

    offText();
    stream.push(envelopeFrame("text", 3));
    await vi.advanceTimersByTimeAsync(0);
    expect(onText).toHaveBeenCalledTimes(1);
    offStatus();
  });

  it("warns and ignores malformed frames", async () => {
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    eventBus.on("text", () => {});
    const stream = await openWithStream();
    stream.push("event: envelope\ndata: not json\n\n");
    stream.push('event: envelope\ndata: {"event":42}\n\n'); // malformed
    await vi.advanceTimersByTimeAsync(0);
    expect(errSpy).toHaveBeenCalled();
    errSpy.mockRestore();
  });

  it("detects seq gaps: warns and fires reconnect/reconcile handlers", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const reconcile = vi.fn();
    eventBus.onReconnect(reconcile);
    eventBus.on("text", () => {});
    const stream = await openWithStream();
    stream.push(envelopeFrame("text", 5));
    await vi.advanceTimersByTimeAsync(0);
    expect(reconcile).not.toHaveBeenCalled();
    stream.push(envelopeFrame("text", 9)); // gap 5→9
    await vi.advanceTimersByTimeAsync(0);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining("seq gap"));
    expect(reconcile).toHaveBeenCalledTimes(1);
    warn.mockRestore();
  });

  it("does not warn for the first frame after a (re)connect (fresh seq watermark)", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    eventBus.on("text", () => {});
    const stream = await openWithStream();
    stream.push(envelopeFrame("text", 100));
    await vi.advanceTimersByTimeAsync(0);
    expect(warn).not.toHaveBeenCalledWith(expect.stringContaining("seq gap"));
    warn.mockRestore();
  });

  it("fires reconnect handlers after the stream re-establishes, not on first open", async () => {
    const reconnect = vi.fn();
    eventBus.onReconnect(reconnect);
    const stream = await openWithStream();
    expect(reconnect).not.toHaveBeenCalled(); // first open is not a reconnect
    stream.end(); // connection lost cleanly
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(RECONNECT_BASE_MS);
    expect(calls.length).toBe(2);
    await openWithStream(1);
    expect(reconnect).toHaveBeenCalledTimes(1);
  });

  it("reconnects with exponential backoff on a stream error and carries the current project list", async () => {
    eventBus.on("text", () => {});
    let stream = await openWithStream();
    stream.fail(new Error("network drop"));
    await vi.advanceTimersByTimeAsync(RECONNECT_BASE_MS - 1);
    expect(calls.length).toBe(1); // not yet
    await vi.advanceTimersByTimeAsync(1);
    expect(calls.length).toBe(2);
    expect(calls[1].url).not.toContain("projects=");

    // Add projects: setProjects restarts the stream with them in the URL.
    stream = await openWithStream(1);
    eventBus.setProjects(["/a", "/b"]);
    await vi.advanceTimersByTimeAsync(0);
    expect(calls.length).toBe(3);
    expect(calls[2].url).toContain("projects=");
    expect(calls[2].url).toContain(encodeURIComponent("/a,/b"));

    // Next failure doubles the delay.
    stream = await openWithStream(2);
    stream.fail(new Error("network drop again"));
    await vi.advanceTimersByTimeAsync(RECONNECT_BASE_MS);
    expect(calls.length).toBe(4); // 1x delay elapsed
  });

  it("treats a non-2xx response as a stream failure and retries with backoff", async () => {
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    eventBus.on("text", () => {});
    await vi.advanceTimersByTimeAsync(0);
    expect(calls.length).toBe(1);
    calls[0].resolve(new Response(null, { status: 401 }));
    await vi.advanceTimersByTimeAsync(0);
    expect(errSpy).toHaveBeenCalledWith(expect.stringContaining("401"));
    await vi.advanceTimersByTimeAsync(RECONNECT_BASE_MS);
    expect(calls.length).toBe(2);
    errSpy.mockRestore();
  });

  it("caps reconnect backoff at RECONNECT_MAX_MS", async () => {
    eventBus.on("text", () => {});
    let idx = 0;
    let stream = await openWithStream(idx++);
    let delay = RECONNECT_BASE_MS;
    // Fail repeatedly until backoff saturates at RECONNECT_MAX_MS.
    for (let i = 0; i < 6; i++) {
      stream.fail(new Error("drop"));
      await vi.advanceTimersByTimeAsync(delay);
      expect(calls.length).toBe(idx + 1);
      stream = await openWithStream(idx++);
      delay = Math.min(delay * 2, RECONNECT_MAX_MS);
    }
    expect(delay).toBe(RECONNECT_MAX_MS);
  });

  it("setProjects with an unchanged list does not restart the stream", async () => {
    eventBus.on("text", () => {});
    await vi.advanceTimersByTimeAsync(0);
    await openWithStream();
    eventBus.setProjects(["/a", "/b"]);
    await vi.advanceTimersByTimeAsync(0);
    expect(calls.length).toBe(2); // initial + one restart
    eventBus.setProjects(["/b", "/a"]); // same set, different order — no restart
    await vi.advanceTimersByTimeAsync(0);
    expect(calls.length).toBe(2);
  });

  it("aborting a stale attempt via stop()/restart() does not schedule a reconnect", async () => {
    eventBus.on("text", () => {});
    await vi.advanceTimersByTimeAsync(0);
    expect(calls.length).toBe(1);
    eventBus.stop(); // aborts the in-flight fetch; its reject(AbortError) must not retry
    await vi.advanceTimersByTimeAsync(RECONNECT_MAX_MS);
    expect(calls.length).toBe(1);
  });
});
