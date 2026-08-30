import { describe, it, expect, vi, beforeEach } from "vitest";
import { browseSrc, getBrowseBase, __resetBrowseBaseCache } from "./client";

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
    expect(await getBrowseBase()).toBe("http://127.0.0.1:9");
    expect(await getBrowseBase()).toBe("http://127.0.0.1:9");
    expect(spy).toHaveBeenCalledTimes(1);
    spy.mockRestore();
  });
});
