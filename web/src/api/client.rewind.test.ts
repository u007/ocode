import { beforeEach, describe, expect, it, vi } from "vitest";

const armed = {
  token: "rewind-token",
  session_id: "ses-1",
  status: "armed",
  expires_at: "2026-09-26T12:00:00.000Z",
  target_index: 42,
  user_seq: 7,
  committed_user_seq: 0,
};

describe("pending rewind API client", () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules();
    sessionStorage.clear();
    window.history.replaceState(null, "", "/");
    fetchSpy = vi.fn(async () => new Response(JSON.stringify(armed), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }));
    vi.stubGlobal("fetch", fetchSpy);
  });

  it("prepares through the owning remote host with the camelCase contract", async () => {
    const { api } = await import("./client");
    await api.prepareRewind("ses-1", {
      targetIndex: 42,
      targetContent: "selected request",
      userSeq: 7,
    }, "devbox");

    expect(fetchSpy).toHaveBeenCalledWith(
      "/api/remote/devbox/api/sessions/ses-1/rewinds",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ targetIndex: 42, targetContent: "selected request", userSeq: 7 }),
      }),
    );
  });

  it("gets and cancels the token through the same remote host", async () => {
    fetchSpy
      .mockResolvedValueOnce(new Response(JSON.stringify(armed), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ cancelled: true }), { status: 200 }));
    const { api } = await import("./client");

    await api.getRewind("ses-1", "rewind-token", "devbox");
    await api.cancelRewind("ses-1", "rewind-token", "devbox");

    expect(fetchSpy.mock.calls[0][0]).toBe("/api/remote/devbox/api/sessions/ses-1/rewinds/rewind-token");
    expect((fetchSpy.mock.calls[0][1] as RequestInit).method).toBe("GET");
    expect(fetchSpy.mock.calls[1][0]).toBe("/api/remote/devbox/api/sessions/ses-1/rewinds/rewind-token");
    expect((fetchSpy.mock.calls[1][1] as RequestInit).method).toBe("DELETE");
  });

  it("adds rewindToken only when supplied to a normal session send", async () => {
    fetchSpy.mockImplementation(async () => new Response(JSON.stringify({ sessionId: "ses-1", model: "m" }), {
        status: 202,
        headers: { "Content-Type": "application/json" },
      }));
    const { api } = await import("./client");

    await api.sendMessage("ses-1", "edited", "devbox", "rewind-token");
    await api.sendMessage("ses-1", "ordinary", undefined, undefined);

    const tokenBody = JSON.parse((fetchSpy.mock.calls[0][1] as RequestInit).body as string);
    const ordinaryBody = JSON.parse((fetchSpy.mock.calls[1][1] as RequestInit).body as string);
    expect(tokenBody.rewindToken).toBe("rewind-token");
    expect(ordinaryBody).not.toHaveProperty("rewindToken");
    expect(fetchSpy.mock.calls[0][0]).toBe("/api/remote/devbox/api/sessions/ses-1/message");
  });
});
