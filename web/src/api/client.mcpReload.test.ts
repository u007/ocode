import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { api } from "./client";

/**
 * POST /api/mcp/reload is asynchronous: 202 + {status, generation}, with the
 * outcome read from GET /api/mcp/reload. reloadMCPAndWait is the only caller
 * that blocks, so the poll loop is pinned here: it must stop on a terminal
 * status, carry the failure errors through, and give up after 35s rather than
 * spin forever.
 */
const URL = "/api/mcp/reload";

describe("async MCP reload client", () => {
  const fetchMock = vi.fn();

  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
    window.history.replaceState(null, "", "/");
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  // fetchJSON parses via res.text() on success and res.json() only on !ok.
  const reply = (body: unknown, status = 200) =>
    ({
      ok: status >= 200 && status < 300,
      status,
      text: async () => JSON.stringify(body),
      json: async () => body,
      headers: { get: () => "application/json" },
    }) as unknown as Response;

  const urlOf = (call: unknown[]) => String(call[0]);
  const methodOf = (call: unknown[]) =>
    ((call[1] ?? {}) as RequestInit).method;

  it("POSTs the reload and returns the 202 acknowledgement", async () => {
    fetchMock.mockResolvedValueOnce(
      reply({ status: "reloading", generation: 7 }, 202),
    );

    const ack = await api.reloadMCP();

    expect(urlOf(fetchMock.mock.calls[0])).toBe(URL);
    expect(methodOf(fetchMock.mock.calls[0])).toBe("POST");
    expect(ack).toEqual({ status: "reloading", generation: 7 });
  });

  it("reads the status with a GET", async () => {
    fetchMock.mockResolvedValueOnce(
      reply({ status: "reloaded", generation: 7, tools: 3, errors: [] }),
    );

    const status = await api.getMCPReloadStatus();

    expect(urlOf(fetchMock.mock.calls[0])).toBe(URL);
    expect(methodOf(fetchMock.mock.calls[0])).toBeUndefined();
    expect(status).toEqual({
      status: "reloaded",
      generation: 7,
      tools: 3,
      errors: [],
    });
  });

  it("polls until reloaded, then stops", async () => {
    vi.useFakeTimers();
    fetchMock
      .mockResolvedValueOnce(reply({ status: "reloading", generation: 1 }, 202))
      .mockResolvedValueOnce(
        reply({ status: "reloading", generation: 1, tools: 0, errors: [] }),
      )
      .mockResolvedValueOnce(
        reply({ status: "reloaded", generation: 1, tools: 5, errors: [] }),
      );

    const pending = api.reloadMCPAndWait();
    await vi.advanceTimersByTimeAsync(1_000);
    const res = await pending;

    expect(res).toEqual({ status: "reloaded", tools: 5, errors: [] });
    // One POST + exactly two GETs: the first terminal status ends the poll.
    expect(fetchMock.mock.calls.filter((c) => methodOf(c) === "POST")).toHaveLength(1);
    expect(fetchMock.mock.calls.filter((c) => methodOf(c) === undefined)).toHaveLength(2);
  });

  it("returns the server errors when the reload failed", async () => {
    vi.useFakeTimers();
    fetchMock
      .mockResolvedValueOnce(reply({ status: "reloading", generation: 2 }, 202))
      .mockResolvedValueOnce(
        reply({
          status: "failed",
          generation: 2,
          tools: 1,
          errors: ["alpha: boom", "beta: timeout"],
        }),
      );

    const pending = api.reloadMCPAndWait();
    await vi.advanceTimersByTimeAsync(0);

    await expect(pending).resolves.toEqual({
      status: "failed",
      tools: 1,
      errors: ["alpha: boom", "beta: timeout"],
    });
  });

  it("throws a clear error after 35s of polling", async () => {
    vi.useFakeTimers();
    fetchMock.mockResolvedValue(
      reply({ status: "reloading", generation: 1, tools: 0, errors: [] }),
    );

    const pending = api.reloadMCPAndWait();
    const assertion = expect(pending).rejects.toThrow(/timed out after 35s/);
    await vi.advanceTimersByTimeAsync(35_000);
    await assertion;
  });

  it("surfaces an HTTP failure from the poll", async () => {
    fetchMock
      .mockResolvedValueOnce(reply({ status: "reloading", generation: 3 }, 202))
      .mockResolvedValueOnce(
        reply({ error: "mcp reload status unavailable" }, 500),
      );

    await expect(api.reloadMCPAndWait()).rejects.toThrow(
      "mcp reload status unavailable",
    );
  });

  it("treats a throwing POST as fatal without polling", async () => {
    fetchMock.mockResolvedValueOnce(reply({ error: "reload denied" }, 403));

    await expect(api.reloadMCPAndWait()).rejects.toThrow("reload denied");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("routes both requests to the remote host when one is given", async () => {
    fetchMock
      .mockResolvedValueOnce(reply({ status: "reloading", generation: 4 }, 202))
      .mockResolvedValueOnce(
        reply({ status: "reloaded", generation: 4, tools: 2, errors: [] }),
      );

    await api.reloadMCPAndWait("mac-mini");

    expect(urlOf(fetchMock.mock.calls[0])).toContain(
      `/api/remote/${encodeURIComponent("mac-mini")}${URL}`,
    );
    expect(urlOf(fetchMock.mock.calls[1])).toContain(
      `/api/remote/${encodeURIComponent("mac-mini")}${URL}`,
    );
  });
});
