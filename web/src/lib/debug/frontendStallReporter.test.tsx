import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render } from "@testing-library/react";

const authedFetch = vi.fn(() => Promise.resolve(new Response(null, { status: 204 })));
vi.mock("@/api/client", () => ({ authedFetch: (...a: unknown[]) => authedFetch(...(a as [])) }));
vi.mock("@/lib/desktopShell", () => ({ isDesktopShell: () => true }));
vi.mock("@/lib/windowId", () => ({ getWindowId: () => "main" }));

import FrontendStallReporter from "./frontendStallReporter";

describe("FrontendStallReporter", () => {
  let now = 0;
  beforeEach(() => {
    vi.useFakeTimers();
    now = 0;
    vi.spyOn(performance, "now").mockImplementation(() => now);
    authedFetch.mockClear();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("reports a tick that fires far later than scheduled", () => {
    render(<FrontendStallReporter />);
    now = 500;
    vi.advanceTimersByTime(500);
    expect(authedFetch).not.toHaveBeenCalled();
    now = 500 + 500 + 9800; // main thread blocked ~9.8s
    vi.advanceTimersByTime(500);
    expect(authedFetch).toHaveBeenCalledTimes(1);
    const body = JSON.parse((authedFetch.mock.calls[0] as unknown as [string, { body: string }])[1].body);
    expect(body.window_id).toBe("main");
    expect(body.stall_ms).toBe(9800);
  });
});
