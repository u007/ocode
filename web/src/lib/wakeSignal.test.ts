import { afterEach, describe, expect, it, vi } from "vitest";
import { onWake } from "./wakeSignal";

function setVisibility(state: DocumentVisibilityState): void {
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => state,
  });
}

describe("onWake", () => {
  afterEach(() => {
    vi.useRealTimers();
    setVisibility("visible");
  });

  it("fires on the window online event", () => {
    vi.useFakeTimers();
    const handler = vi.fn();
    const off = onWake(handler);
    window.dispatchEvent(new Event("online"));
    expect(handler).toHaveBeenCalledTimes(1);
    off();
  });

  it("fires on a visibilitychange to visible", () => {
    vi.useFakeTimers();
    setVisibility("hidden");
    const handler = vi.fn();
    const off = onWake(handler);
    setVisibility("visible");
    document.dispatchEvent(new Event("visibilitychange"));
    expect(handler).toHaveBeenCalledTimes(1);
    off();
  });

  it("does not fire on a visibilitychange while hidden", () => {
    vi.useFakeTimers();
    setVisibility("hidden");
    const handler = vi.fn();
    const off = onWake(handler);
    document.dispatchEvent(new Event("visibilitychange"));
    expect(handler).not.toHaveBeenCalled();
    off();
  });

  it("deduplicates a wake that emits both online and visibilitychange", () => {
    vi.useFakeTimers();
    setVisibility("visible");
    const handler = vi.fn();
    const off = onWake(handler);
    window.dispatchEvent(new Event("online"));
    document.dispatchEvent(new Event("visibilitychange"));
    expect(handler).toHaveBeenCalledTimes(1);
    off();
  });

  it("fires again once a second has elapsed", () => {
    vi.useFakeTimers();
    setVisibility("visible");
    const handler = vi.fn();
    const off = onWake(handler);
    window.dispatchEvent(new Event("online"));
    vi.advanceTimersByTime(1001);
    document.dispatchEvent(new Event("visibilitychange"));
    expect(handler).toHaveBeenCalledTimes(2);
    off();
  });

  it("stops firing after unsubscribe", () => {
    vi.useFakeTimers();
    const handler = vi.fn();
    const off = onWake(handler);
    off();
    window.dispatchEvent(new Event("online"));
    expect(handler).not.toHaveBeenCalled();
  });
});
