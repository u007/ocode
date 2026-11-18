import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * Pinned hub-cloud wire contract. These methods are the only bridge the
 * Settings UI has to kakiit-brokered connectors, so a wrong URL, verb, body
 * key or un-encoded query param here silently breaks every project. Project
 * paths contain spaces and slashes — encoding is load-bearing, not cosmetic.
 */
function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("hub cloud connector client", () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules();
    window.history.replaceState(null, "", "/");
    fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
  });

  it("lists hub connections with an encoded project and the host", async () => {
    fetchSpy.mockResolvedValue(jsonResponse([{ provider: "dropbox" }]));
    const { api } = await import("./client");
    await api.listHubCloudConnections("/work/my app", "user@box");
    expect(fetchSpy.mock.calls[0][0]).toBe(
      "/api/remote/user%40box/api/cloud/connections?project=%2Fwork%2Fmy%20app&host=user%40box",
    );
  });

  it("registers the callback with a PUT and snake_case body", async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ ok: true }));
    const { api } = await import("./client");
    await api.registerHubCloudCallback(
      "/work/app",
      "inst-1",
      "http://127.0.0.1:4321/callback",
      "user@box",
    );
    expect(fetchSpy.mock.calls[0][0]).toBe(
      "/api/remote/user%40box/api/cloud/oauth/kakiit/callback/register?project=%2Fwork%2Fapp&host=user%40box",
    );
    const init = fetchSpy.mock.calls[0][1] as RequestInit;
    expect(init.method).toBe("PUT");
    expect(JSON.parse(init.body as string)).toEqual({
      install_id: "inst-1",
      callback_url: "http://127.0.0.1:4321/callback",
    });
  });

  it("starts hub OAuth with the four pinned body keys", async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({ auth_url: "https://dropbox.test/auth" }),
    );
    const { api } = await import("./client");
    await api.startHubCloudOAuth(
      "/work/app",
      {
        provider: "dropbox",
        connector_id: "kakiit",
        install_id: "install-1",
        client_state: "cs-1",
        code_challenge: "chal-1",
      },
      "user@box",
    );
    expect(fetchSpy.mock.calls[0][0]).toBe(
      "/api/remote/user%40box/api/cloud/oauth/kakiit/start?project=%2Fwork%2Fapp&host=user%40box",
    );
    const init = fetchSpy.mock.calls[0][1] as RequestInit;
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({
      provider: "dropbox",
      connector_id: "kakiit",
      install_id: "install-1",
      client_state: "cs-1",
      code_challenge: "chal-1",
    });
  });

  it("redeems a one-time code with the verifier and client state", async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ connection_id: "cx-1" }));
    const { api } = await import("./client");
    await api.redeemHubCloudCode(
      "/work/app",
      { code: "one-time", code_verifier: "verifier", client_state: "cs-1" },
      "user@box",
    );
    expect(fetchSpy.mock.calls[0][0]).toBe(
      "/api/remote/user%40box/api/cloud/oauth/kakiit/return?project=%2Fwork%2Fapp&host=user%40box",
    );
    const init = fetchSpy.mock.calls[0][1] as RequestInit;
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({
      code: "one-time",
      code_verifier: "verifier",
      client_state: "cs-1",
    });
  });

  it("refreshes a token by connection id", async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ access_token: "t" }));
    const { api } = await import("./client");
    await api.refreshHubCloudToken("/work/app", "cx/1", "user@box");
    expect(fetchSpy.mock.calls[0][0]).toBe(
      "/api/remote/user%40box/api/cloud/token/refresh?project=%2Fwork%2Fapp&host=user%40box",
    );
    const init = fetchSpy.mock.calls[0][1] as RequestInit;
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({ connection_id: "cx/1" });
  });

  it("disconnects with a DELETE and both query params encoded", async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ ok: true }));
    const { api } = await import("./client");
    await api.disconnectHubCloud("/work/app", "cx 1", "user@box");
    expect(fetchSpy.mock.calls[0][0]).toBe(
      "/api/remote/user%40box/api/cloud/oauth/kakiit/disconnect?project=%2Fwork%2Fapp&host=user%40box&connection_id=cx%201",
    );
    expect((fetchSpy.mock.calls[0][1] as RequestInit).method).toBe("DELETE");
  });

  it("threads the remote host into the URL query and omits it when local", async () => {
    fetchSpy.mockResolvedValue(jsonResponse([]));
    const { api } = await import("./client");

    await api.listHubCloudConnections("/work/app", "wsl:Ubuntu-22.04");
    expect(fetchSpy.mock.calls[0][0]).toBe(
      "/api/remote/wsl%3AUbuntu-22.04/api/cloud/connections?project=%2Fwork%2Fapp&host=wsl%3AUbuntu-22.04",
    );

    fetchSpy.mockClear();
    fetchSpy.mockResolvedValue(jsonResponse([]));
    await api.listHubCloudConnections("/work/app");
    expect(fetchSpy.mock.calls[0][0]).toBe(
      "/api/cloud/connections?project=%2Fwork%2Fapp",
    );
  });
});
