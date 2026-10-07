import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * /btw client suite: the start (POST) and cancel (DELETE) calls, including
 * remote-host threading and id encoding.
 */
function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("/btw side-query client", () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules();
    window.history.replaceState(null, "", "/");
    fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
  });

  it("POSTs the aside and returns the started status", async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ status: "started", generation: 1 }, 202));
    const { api } = await import("./client");
    const res = await api.btwSession("ses_1", "use tabs");
    expect(fetchSpy.mock.calls[0][0]).toBe("/api/sessions/ses_1/btw");
    const init = fetchSpy.mock.calls[0][1] as RequestInit;
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({ content: "use tabs" });
    expect(res.status).toBe("started");
  });

  it("DELETEs to cancel and threads the remote host", async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ status: "cancelled" }));
    const { api } = await import("./client");
    const res = await api.cancelBtw("ses_1", "user@box");
    expect(fetchSpy.mock.calls[0][0]).toBe("/api/remote/user%40box/api/sessions/ses_1/btw");
    expect((fetchSpy.mock.calls[0][1] as RequestInit).method).toBe("DELETE");
    expect(res.status).toBe("cancelled");
  });

  it("url-encodes the session id on cancel", async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ status: "cancelled" }));
    const { api } = await import("./client");
    await api.cancelBtw("ses/odd id");
    expect(fetchSpy.mock.calls[0][0]).toBe("/api/sessions/ses%2Fodd%20id/btw");
  });
});
