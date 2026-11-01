import { describe, expect, it } from "vitest";
import { SessionIndex, type SessionRef } from "./identity";

const a: SessionRef = { id: "ses_a", title: "[Excel] Budget.xlsx", created: "2026-10-30T09:00:00Z" };
const b: SessionRef = { id: "ses_b", title: "[Excel] Budget.xlsx", created: "2026-10-31T09:00:00Z" };

/** A fake bridge: records each request and answers with the given status and JSON body. */
function fakeBridge(status: number, body: unknown) {
  const calls: { url: string; method: string; headers: Record<string, string>; body: string | undefined }[] = [];
  const f = (async (input: RequestInfo | URL, init?: RequestInit) => {
    calls.push({ url: String(input), method: init?.method ?? "GET", headers: init?.headers as Record<string, string>, body: init?.body as string | undefined });
    return new Response(status === 204 ? null : JSON.stringify(body), { status });
  }) as typeof fetch;
  return { f, calls };
}

describe("SessionIndex", () => {
  it("add POSTs one ref to the atomic bridge route and returns the index the bridge answers", async () => {
    const { f, calls } = fakeBridge(200, [b, a]);
    const idx = new SessionIndex("https://bridge.test", "k-1", "wb 1", f);
    expect(await idx.add(b)).toEqual([b, a]);
    expect(calls).toHaveLength(1);
    expect(calls[0]).toMatchObject({ url: "https://bridge.test/bridge/sessions/wb%201", method: "POST" });
    expect(calls[0].headers).toMatchObject({ "X-Bridge-Key": "k-1", "Content-Type": "application/json" });
    expect(JSON.parse(calls[0].body!)).toEqual(b);
  });

  it("add sends exactly id, title and created, not any other field the caller carries", async () => {
    const { f, calls } = fakeBridge(200, [a]);
    const idx = new SessionIndex("https://bridge.test", "k-1", "wb1", f);
    const withExtra = { ...a, extra: "x" };
    await idx.add(withExtra);
    expect(JSON.parse(calls[0].body!)).toEqual(a);
  });

  it("add throws with the status when the bridge refuses the ref", async () => {
    const { f } = fakeBridge(400, "created must be an RFC 3339 time");
    const idx = new SessionIndex("https://bridge.test", "k-1", "wb1", f);
    await expect(idx.add(a)).rejects.toThrow("session index write 400");
  });

  it("list GETs the state key with the key header and returns the stored index", async () => {
    const { f, calls } = fakeBridge(200, [b, a]);
    const idx = new SessionIndex("https://bridge.test", "k-1", "wb1", f);
    expect(await idx.list()).toEqual([b, a]);
    expect(calls[0]).toMatchObject({ url: "https://bridge.test/bridge/state/wb1/sessions", method: "GET" });
    expect(calls[0].headers).toMatchObject({ "X-Bridge-Key": "k-1" });
  });

  it("list is empty before the first add (404 from the bridge)", async () => {
    const { f } = fakeBridge(404, "");
    const idx = new SessionIndex("https://bridge.test", "k-1", "wb1", f);
    expect(await idx.list()).toEqual([]);
  });

  it("list throws on any other failure", async () => {
    const { f } = fakeBridge(500, "");
    const idx = new SessionIndex("https://bridge.test", "k-1", "wb1", f);
    await expect(idx.list()).rejects.toThrow("session index 500");
  });
});
