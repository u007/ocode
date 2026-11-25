import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

const authedFetch = vi.fn(() => Promise.resolve(new Response(null, { status: 204 })));
vi.mock("@/api/client", () => ({ authedFetch: (...a: unknown[]) => authedFetch(...(a as [])) }));
vi.mock("@/lib/desktopShell", () => ({ isDesktopShell: () => true }));
vi.mock("@/lib/windowId", () => ({ getWindowId: () => "main" }));

type UiTiming = typeof import("./uiTiming");

function postedBodies(): Array<Record<string, unknown>> {
  return authedFetch.mock.calls.map(
    (c) => JSON.parse((c as unknown as [string, { body: string }])[1].body) as Record<string, unknown>,
  );
}

describe("uiTiming", () => {
  let now = 0;
  let timing: UiTiming;

  beforeEach(async () => {
    vi.useFakeTimers();
    now = 0;
    vi.spyOn(performance, "now").mockImplementation(() => now);
    // rAF runs its callback immediately so only the setTimeout(0) needs advancing.
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
      cb(0);
      return 0;
    });
    authedFetch.mockClear();
    // Module state is process-wide; load a fresh copy per test.
    vi.resetModules();
    timing = await import("./uiTiming");
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("posts once with action and phase detail when the action takes at least 150ms", () => {
    now = 1000;
    timing.startUiTiming("model-dialog-open");
    now = 1040;
    timing.markUiTiming("commit");
    now = 1300;
    timing.markUiTiming("rows");
    timing.finishUiTimingAfterPaint();
    vi.advanceTimersByTime(0);

    expect(authedFetch).toHaveBeenCalledTimes(1);
    const body = postedBodies()[0];
    expect(body.window_id).toBe("main");
    expect(body.stall_ms).toBe(300);
    expect(body.action).toBe("model-dialog-open");
    expect(body.detail).toBe("commit=40 rows=300");
  });

  it("posts nothing when the action finishes under 150ms", () => {
    now = 1000;
    timing.startUiTiming("model-dialog-close");
    now = 1100;
    timing.markUiTiming("commit");
    timing.finishUiTimingAfterPaint();
    vi.advanceTimersByTime(0);

    expect(authedFetch).not.toHaveBeenCalled();
  });

  it("drops the first report when a newer action starts before it finishes", () => {
    now = 0;
    timing.startUiTiming("first");
    now = 50;
    timing.finishUiTimingAfterPaint();
    // Another action starts before the deferred timer fires.
    now = 60;
    timing.startUiTiming("second");
    vi.advanceTimersByTime(0);
    expect(authedFetch).not.toHaveBeenCalled();

    now = 400;
    timing.finishUiTimingAfterPaint();
    vi.advanceTimersByTime(0);
    expect(authedFetch).toHaveBeenCalledTimes(1);
    expect(postedBodies()[0].action).toBe("second");
  });

  it("describes the last action as empty before any action, then as elapsed time", () => {
    expect(timing.describeLastUiAction()).toBe("");

    now = 1000;
    timing.startUiTiming("model-dialog-open");
    now = 1400;
    expect(timing.describeLastUiAction()).toBe("model-dialog-open 400ms ago");
  });
});
