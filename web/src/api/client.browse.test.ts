import { describe, it, expect, vi, beforeEach } from "vitest";
import { browseSrc, getBrowseBase, getBrowseRemoteMode, mintBrowseGrant, revokeBrowseSession, __resetBrowseBaseCache } from "./client";

describe("browseSrc", () => {
  it("builds the /b/ path from a URL and appends the grant on first load", () => {
    const src = browseSrc("http://127.0.0.1:5000", "GRANT123", "tab:x", "https://example.com/foo?q=1");
    expect(src).toBe("http://127.0.0.1:5000/b/tab:x/https/example.com/foo?q=1&__grant=GRANT123");
  });

  it("omits the grant param when grant is null (already authenticated)", () => {
    const src = browseSrc("http://127.0.0.1:5000", null, "tab:x", "http://localhost:5173/");
    expect(src).toBe("http://127.0.0.1:5000/b/tab:x/http/localhost:5173/");
  });
});

describe("getBrowseBase", () => {
  beforeEach(() => __resetBrowseBaseCache());
  it("fetches once and caches", async () => {
    const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ base_url: "http://127.0.0.1:9" }), { status: 200 }),
    );
    expect(await getBrowseBase("")).toBe("http://127.0.0.1:9");
    expect(await getBrowseBase("")).toBe("http://127.0.0.1:9");
    expect(spy).toHaveBeenCalledTimes(1);
    spy.mockRestore();
  });
});

describe("remote browse routing", () => {
  beforeEach(() => __resetBrowseBaseCache());

  it("asks this machine for a remote host's browse origin and re-reads it on every call", async () => {
    // The host's browse listener binds an ephemeral port that changes when
    // `ocode serve --remote` restarts, so a remote origin is never cached
    // across calls; the local one is.
    let remotePort = 40263;
    const spy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      const base = url.includes("user%40realhost") ? `http://localhost:${remotePort}` : "http://127.0.0.1:9";
      return new Response(JSON.stringify({ base_url: base, remote_mode: url.includes("host=") }), { status: 200 });
    });
    expect(await getBrowseBase("user@realhost")).toBe("http://localhost:40263");
    expect(await getBrowseBase("")).toBe("http://127.0.0.1:9");
    remotePort = 41000;
    expect(await getBrowseBase("user@realhost")).toBe("http://localhost:41000");
    expect(await getBrowseBase("")).toBe("http://127.0.0.1:9");
    expect(spy).toHaveBeenCalledTimes(3);
    expect(String(spy.mock.calls[0][0])).toContain("/api/browse/config?host=user%40realhost");
    expect(getBrowseRemoteMode("user@realhost")).toBe(true);
    expect(getBrowseRemoteMode("")).toBe(false);
    spy.mockRestore();
  });

  it("shares one in-flight read between concurrent callers for the same host", async () => {
    let release: (() => void) | null = null;
    const spy = vi.spyOn(globalThis, "fetch").mockImplementation(
      () => new Promise<Response>((resolve) => {
        release = () => resolve(new Response(JSON.stringify({ base_url: "http://localhost:40263" }), { status: 200 }));
      }),
    );
    const reads = [getBrowseBase("user@realhost"), getBrowseBase("user@realhost"), getBrowseBase("user@realhost")];
    expect(spy).toHaveBeenCalledTimes(1);
    release!();
    expect(await Promise.all(reads)).toEqual(["http://localhost:40263", "http://localhost:40263", "http://localhost:40263"]);
    // The next call after settlement is a fresh read.
    release = null;
    void getBrowseBase("user@realhost");
    expect(spy).toHaveBeenCalledTimes(2);
    release!();
    spy.mockRestore();
  });

  it("rejects when the remote origin cannot be read, never falling back to the local base", async () => {
    const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("nope", { status: 502 }));
    await expect(getBrowseBase("user@realhost")).rejects.toThrow("browse config: 502");
    expect(spy).toHaveBeenCalledTimes(1);
    expect(String(spy.mock.calls[0][0])).toContain("host=user%40realhost");
    spy.mockRestore();
  });

  it("mints and revokes a remote grant on that host's proxied browse API", async () => {
    const spy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/grant")) return new Response(JSON.stringify({ grant: "G" }), { status: 200 });
      return new Response(null, { status: 204 });
    });
    expect(await mintBrowseGrant("side:chat:s1", "user@realhost")).toBe("G");
    await revokeBrowseSession("side:chat:s1", "user@realhost");
    const urls = spy.mock.calls.map((c) => String(c[0]));
    expect(urls[0]).toContain("/api/remote/user%40realhost/api/browse/grant");
    expect(urls[1]).toContain("/api/remote/user%40realhost/api/browse/revoke");
    spy.mockRestore();
  });

  it("revokes on the tab's project host even when this SPA never minted the grant", async () => {
    // After a reload the SPA has no memory of where a tab's grant was minted;
    // the caller's project host, not an in-memory record, addresses the revoke.
    const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await revokeBrowseSession("tab:restored", "user@realhost");
    expect(String(spy.mock.calls[0][0])).toContain("/api/remote/user%40realhost/api/browse/revoke");
    spy.mockRestore();
  });

  it("keeps local grants and revokes on the local browse API", async () => {
    const spy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/grant")) return new Response(JSON.stringify({ grant: "L" }), { status: 200 });
      return new Response(null, { status: 204 });
    });
    expect(await mintBrowseGrant("tab:local", "")).toBe("L");
    await revokeBrowseSession("tab:local", "");
    const urls = spy.mock.calls.map((c) => String(c[0]));
    expect(urls[0]).toMatch(/\/api\/browse\/grant$/);
    expect(urls[1]).toMatch(/\/api\/browse\/revoke$/);
    spy.mockRestore();
  });
});
