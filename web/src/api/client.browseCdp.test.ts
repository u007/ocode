import { describe, it, expect, beforeEach, vi } from "vitest";

// client.ts resolves its auth token at module-eval time, so each case needs a
// fresh module instance (same pattern as client.token.test.ts).
async function load() {
  return await import("./client");
}

function queryOf(url: string): URLSearchParams {
  return new URL(url, "http://localhost").searchParams;
}

describe("browseCdpWsConnection", () => {
  beforeEach(() => {
    vi.resetModules();
    sessionStorage.clear();
    window.history.replaceState(null, "", "/");
  });

  it("rides the query string in local mode (a browser WebSocket cannot set headers)", async () => {
    window.history.replaceState(null, "", "/?token=localtok");
    const { browseCdpWsConnection } = await load();

    const { url, protocols } = browseCdpWsConnection("tab:1", "grant-1", "");

    expect(protocols).toBeUndefined();
    const q = queryOf(url);
    expect(q.get("token")).toBe("localtok");
    expect(q.get("__grant")).toBe("grant-1");
    expect(q.get("host")).toBeNull();
    expect(new URL(url, "http://localhost").pathname).toContain("/api/browse/cdp/tab%3A1");
  });

  it("uses the ocode.bearer subprotocol in remote mode and never leaks the token into the query", async () => {
    window.history.replaceState(null, "", "/#token=remotetok");
    const { browseCdpWsConnection } = await load();

    const { url, protocols } = browseCdpWsConnection("tab:2", "grant-2", "james@host");

    expect(protocols).toEqual(["ocode.bearer.remotetok"]);
    const q = queryOf(url);
    expect(q.get("token")).toBeNull();
    expect(q.get("host")).toBe("james@host");
  });

  it("omits both the token and the host when unauthenticated and local", async () => {
    const { browseCdpWsConnection } = await load();

    const { url, protocols } = browseCdpWsConnection("tab:3", "grant-3", "");

    expect(protocols).toBeUndefined();
    const q = queryOf(url);
    expect(q.get("token")).toBeNull();
    expect(q.get("host")).toBeNull();
    expect(q.get("__grant")).toBe("grant-3");
  });

  it("URL-encodes the state key so a shared tab key cannot break the path", async () => {
    window.history.replaceState(null, "", "/?token=t");
    const { browseCdpWsConnection } = await load();

    const { url } = browseCdpWsConnection("tab:a/b?c", "g", "");

    expect(url).toContain("tab%3Aa%2Fb%3Fc");
  });
});
