import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

// ocodeDebug.memory() combines the Go runtime stats with the renderer
// attribution samples; both fetches must land, a frontend-stats failure
// (404 on non-desktop) degrades to an empty sample list rather than
// rejecting the whole call.

const samples = [
  {
    received_at: "2026-01-01T00:01:00Z",
    window_id: "w1",
    terminal_count: 2,
    terminal_lines: 5000,
    session_count: 3,
    message_count: 120,
    message_bytes: 2048,
    dom_node_count: 4000,
  },
];

vi.mock("./api/client", () => ({
  apiPath: (p: string) => p,
  authHeaders: () => ({}),
}));

describe("ocodeDebug.memory", () => {
  beforeEach(() => {
    vi.spyOn(console, "table").mockImplementation(() => {});
    vi.spyOn(console, "log").mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("fetches both endpoints and returns go + frontend samples", async () => {
    const fetchMock = vi.fn((path: string) =>
      Promise.resolve(
        new Response(
          path.includes("runtime")
            ? JSON.stringify({ heap_alloc_bytes: 1, heap_sys_bytes: 2, sys_bytes: 3, num_goroutine: 4, num_gc: 5, uptime: "1s" })
            : JSON.stringify(samples),
          { status: 200 },
        ),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const mod = (await import("./debug")) as unknown as { ocodeDebug: { memory(): Promise<{ go: { num_goroutine: number; num_gc: number }; frontend: Array<{ window_id: string }> }> } };
    const ocodeDebug = (window as unknown as { ocodeDebug: { memory(): Promise<{ go: { num_goroutine: number; num_gc: number }; frontend: Array<{ window_id: string }> }> } }).ocodeDebug ?? mod.ocodeDebug;
    const res = await ocodeDebug.memory();
    expect(res.go.num_goroutine).toBe(4);
    expect(res.frontend).toHaveLength(1);
    expect(res.frontend[0].window_id).toBe("w1");
    expect(fetchMock).toHaveBeenCalledWith("/api/debug/runtime", expect.anything());
    expect(fetchMock).toHaveBeenCalledWith("/api/debug/frontend-stats", expect.anything());
  });

  it("degrades to empty frontend samples when the endpoint 404s (non-desktop)", async () => {
    const fetchMock = vi.fn((path: string) =>
      Promise.resolve(
        path.includes("frontend-stats")
          ? new Response("not found", { status: 404 })
          : new Response(
              JSON.stringify({ heap_alloc_bytes: 1, heap_sys_bytes: 2, sys_bytes: 3, num_goroutine: 4, num_gc: 5, uptime: "1s" }),
              { status: 200 },
            ),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const mod = (await import("./debug")) as unknown as { ocodeDebug: { memory(): Promise<{ go: { num_goroutine: number; num_gc: number }; frontend: Array<{ window_id: string }> }> } };
    const ocodeDebug = (window as unknown as { ocodeDebug: { memory(): Promise<{ go: { num_goroutine: number; num_gc: number }; frontend: Array<{ window_id: string }> }> } }).ocodeDebug ?? mod.ocodeDebug;
    const res = await ocodeDebug.memory();
    expect(res.frontend).toEqual([]);
    expect(res.go.num_gc).toBe(5);
  });
});
