import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { eventBus, RECONNECT_BASE_MS } from "./eventBus";
import type { BusEnvelope } from "./eventBus";

/**
 * Mock EventSource: instances record their URL, capture listeners, and let the
 * test drive open/envelope/error events. `vi.stubGlobal` replaces the real
 * EventSource before the bus module creates streams.
 */
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  readyState = 0;
  private listeners = new Map<string, Set<(e: MessageEvent) => void>>();
  onopen: (() => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }
  addEventListener(name: string, fn: (e: MessageEvent) => void) {
    let set = this.listeners.get(name);
    if (!set) {
      set = new Set();
      this.listeners.set(name, set);
    }
    set.add(fn);
  }
  dispatch(name: string, data?: string) {
    this.listeners.get(name)?.forEach((fn) =>
      fn({ data } as MessageEvent),
    );
  }
  open() {
    this.readyState = 1;
    this.dispatch("open");
    this.onopen?.();
  }
  error() {
    // The bus closes the stream itself on error, mirroring browser behavior
    // where EventSource reaches CLOSED after the handler runs.
    this.dispatch("error");
  }
  close() {
    this.readyState = 2;
  }
}

function envelope(event: string, seq: number, extra: Partial<BusEnvelope> = {}): BusEnvelope {
  return { event, project: "/proj", session_id: "", seq, data: {}, ...extra };
}

describe("eventBus", () => {
  beforeEach(() => {
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
    vi.useFakeTimers();
    eventBus.stop();
  });

  afterEach(() => {
    eventBus.stop();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("routes envelopes to registered handlers by event type, passing the full envelope", () => {
    const onText = vi.fn();
    const onStatus = vi.fn();
    const offText = eventBus.on("text", onText);
    const offStatus = eventBus.on("status", onStatus);

    const es = FakeEventSource.instances[0];
    expect(es).toBeDefined();
    es.open();
    es.dispatch("envelope", JSON.stringify(envelope("text", 1, { session_id: "s1", data: { delta: "hi" } })));
    es.dispatch("envelope", JSON.stringify(envelope("status", 2, { data: { model: "m" } })));

    expect(onText).toHaveBeenCalledTimes(1);
    expect(onText.mock.calls[0][0]).toMatchObject({ event: "text", session_id: "s1", seq: 1 });
    expect(onStatus).toHaveBeenCalledTimes(1);
    expect(onStatus.mock.calls[0][0].data).toEqual({ model: "m" });

    offText();
    es.dispatch("envelope", JSON.stringify(envelope("text", 3)));
    expect(onText).toHaveBeenCalledTimes(1);
    offStatus();
  });

  it("warns and ignores malformed frames", () => {
    const warn = vi.spyOn(console, "error").mockImplementation(() => {});
    eventBus.on("text", () => {});
    const es = FakeEventSource.instances[0];
    es.open();
    es.dispatch("envelope", "not json");
    es.dispatch("envelope", JSON.stringify({ event: 42 })); // malformed
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
  });

  it("detects seq gaps: warns and fires reconnect/reconcile handlers", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const reconcile = vi.fn();
    eventBus.onReconnect(reconcile);
    eventBus.on("text", () => {});
    const es = FakeEventSource.instances[0];
    es.open();
    es.dispatch("envelope", JSON.stringify(envelope("text", 5)));
    expect(reconcile).not.toHaveBeenCalled();
    es.dispatch("envelope", JSON.stringify(envelope("text", 9))); // gap 5→9
    expect(warn).toHaveBeenCalledWith(expect.stringContaining("seq gap"));
    expect(reconcile).toHaveBeenCalledTimes(1);
    warn.mockRestore();
  });

  it("does not warn for the first frame after a (re)connect (fresh seq watermark)", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    eventBus.on("text", () => {});
    const es = FakeEventSource.instances[0];
    es.open();
    es.dispatch("envelope", JSON.stringify(envelope("text", 100)));
    expect(warn).not.toHaveBeenCalledWith(expect.stringContaining("seq gap"));
    warn.mockRestore();
  });

  it("fires reconnect handlers after the stream re-establishes, not on first open", () => {
    const reconnect = vi.fn();
    eventBus.onReconnect(reconnect);
    const es = FakeEventSource.instances[0];
    es.open();
    expect(reconnect).not.toHaveBeenCalled(); // first open is not a reconnect
    es.error();
    vi.advanceTimersByTime(RECONNECT_BASE_MS);
    const es2 = FakeEventSource.instances[1];
    expect(es2).toBeDefined();
    es2.open();
    expect(reconnect).toHaveBeenCalledTimes(1);
  });

  it("reconnects with exponential backoff and the current project list", () => {
    eventBus.on("text", () => {});
    let es = FakeEventSource.instances[0];
    es.open();
    es.error();
    vi.advanceTimersByTime(RECONNECT_BASE_MS - 1);
    expect(FakeEventSource.instances.length).toBe(1); // not yet
    vi.advanceTimersByTime(1);
    es = FakeEventSource.instances[1];
    expect(es).toBeDefined();
    expect(es.url).not.toContain("projects=");
    // Add projects: setProjects restarts the stream with them in the URL.
    eventBus.setProjects(["/a", "/b"]);
    const es2 = FakeEventSource.instances[2];
    expect(es2).toBeDefined();
    expect(es2.url).toContain("projects=");
    expect(es2.url).toContain(encodeURIComponent("/a,/b") || "/a%2Cb");
    // Next failure doubles the delay.
    es2.error();
    vi.advanceTimersByTime(RECONNECT_BASE_MS);
    expect(FakeEventSource.instances.length).toBe(3);
    vi.advanceTimersByTime(RECONNECT_BASE_MS); // 2x = 2s total
    expect(FakeEventSource.instances.length).toBe(4);
  });

  it("setProjects with an unchanged list does not restart the stream", () => {
    eventBus.on("text", () => {});
    eventBus.setProjects(["/a", "/b"]);
    expect(FakeEventSource.instances.length).toBe(2); // initial + one restart
    const first = FakeEventSource.instances[1];
    eventBus.setProjects(["/b", "/a"]); // same set, different order — no restart
    expect(FakeEventSource.instances.length).toBe(2);
    expect(first.close).toBeDefined();
  });
});
