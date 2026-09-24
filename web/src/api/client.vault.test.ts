import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./client";

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("vault api", () => {
  it("status encodes the surface", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) =>
      jsonResponse({ exists: true, unlocked: false }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const res = await api.vaultStatus("settings");

    expect(res).toEqual({ exists: true, unlocked: false });
    expect(String(fetchMock.mock.calls[0][0])).toContain("/api/vault/status?surface=settings");
  });

  it("create posts the item with the surface query", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) =>
      jsonResponse({ id: "1", site: "S" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await api.vaultCreate({ site: "S" }, "settings");

    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain("/api/vault/items?surface=settings");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(String(init?.body))).toEqual({ site: "S" });
  });

  it("list forwards sort/limit/offset", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) =>
      jsonResponse({ items: [], total: 0 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await api.vaultList("settings", { sort: "site", limit: 25, offset: 50 });

    const url = String(fetchMock.mock.calls[0][0]);
    expect(url).toContain("surface=settings");
    expect(url).toContain("sort=site");
    expect(url).toContain("limit=25");
    expect(url).toContain("offset=50");
  });
});
