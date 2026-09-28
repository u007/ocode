import { describe, expect, it, vi } from "vitest";
import { isSameHostHTTPS, runDesktopStorageMigration, STORAGE_MIGRATION_PATH } from "./desktopStorageMigration";

function memoryStorage(init: Record<string, string> = {}): Storage {
  const m = new Map(Object.entries(init));
  return {
    get length() {
      return m.size;
    },
    key: (i) => Array.from(m.keys())[i] ?? null,
    getItem: (k) => (m.has(k) ? m.get(k)! : null),
    setItem: (k, v) => void m.set(k, String(v)),
    removeItem: (k) => void m.delete(k),
    clear: () => m.clear(),
  };
}

function env(search: string, storage: Storage, fetchImpl: typeof fetch, protocol = "http:") {
  const replace = vi.fn();
  const replaceUrl = vi.fn();
  return {
    env: {
      location: { search, protocol, host: "127.0.0.1:5000", href: "", pathname: "/", hash: "", replace },
      storage,
      fetch: fetchImpl,
      replaceUrl,
    },
    replace,
    replaceUrl,
  };
}

describe("isSameHostHTTPS", () => {
  it("accepts only https on the same host:port", () => {
    expect(isSameHostHTTPS("https://127.0.0.1:5000/?token=x", "127.0.0.1:5000")).toBe(true);
    expect(isSameHostHTTPS("http://127.0.0.1:5000/", "127.0.0.1:5000")).toBe(false);
    expect(isSameHostHTTPS("https://evil.example/", "127.0.0.1:5000")).toBe(false);
    expect(isSameHostHTTPS("https://127.0.0.1:5001/", "127.0.0.1:5000")).toBe(false);
    expect(isSameHostHTTPS("not a url", "127.0.0.1:5000")).toBe(false);
  });
});

describe("runDesktopStorageMigration", () => {
  it("exports every entry with the bearer token, then redirects without rendering", async () => {
    const fetchImpl = vi.fn(async () => new Response(null, { status: 204 }));
    const target = "https://127.0.0.1:5000/?token=tok&storageImport=1";
    const { env: e, replace } = env(
      `?token=tok&migrateTo=${encodeURIComponent(target)}`,
      memoryStorage({ "ocode.ui.tabs.v1": "[1]", other: "x" }),
      fetchImpl as unknown as typeof fetch,
    );
    await expect(runDesktopStorageMigration(e)).resolves.toBe(false);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    const [path, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit];
    expect(path).toBe(STORAGE_MIGRATION_PATH);
    expect(init.method).toBe("POST");
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer tok");
    expect(JSON.parse(init.body as string)).toEqual({ "ocode.ui.tabs.v1": "[1]", other: "x" });
    expect(replace).toHaveBeenCalledWith(target);
  });

  it("still redirects when the export request fails", async () => {
    const fetchImpl = vi.fn(async () => {
      throw new Error("offline");
    });
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const target = "https://127.0.0.1:5000/";
    const { env: e, replace } = env(`?migrateTo=${encodeURIComponent(target)}`, memoryStorage(), fetchImpl as unknown as typeof fetch);
    await expect(runDesktopStorageMigration(e)).resolves.toBe(false);
    expect(replace).toHaveBeenCalledWith(target);
    errSpy.mockRestore();
  });

  it("refuses a migrateTo on another host and renders normally", async () => {
    const fetchImpl = vi.fn();
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const { env: e, replace } = env(`?migrateTo=${encodeURIComponent("https://evil.example/")}`, memoryStorage(), fetchImpl as unknown as typeof fetch);
    await expect(runDesktopStorageMigration(e)).resolves.toBe(true);
    expect(fetchImpl).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
    errSpy.mockRestore();
  });

  it("imports only keys the new origin lacks and strips storageImport", async () => {
    const storage = memoryStorage({ keep: "new" });
    const fetchImpl = vi.fn(async () => new Response(JSON.stringify({ keep: "old", draft: "d" }), { status: 200 }));
    const { env: e, replaceUrl } = env("?token=tok&storageImport=1&windowId=main", storage, fetchImpl as unknown as typeof fetch, "https:");
    await expect(runDesktopStorageMigration(e)).resolves.toBe(true);
    expect(storage.getItem("keep")).toBe("new");
    expect(storage.getItem("draft")).toBe("d");
    expect(replaceUrl).toHaveBeenCalledWith("/?token=tok&windowId=main");
  });

  it("does nothing without migration params", async () => {
    const fetchImpl = vi.fn();
    const { env: e } = env("?token=tok", memoryStorage(), fetchImpl as unknown as typeof fetch, "https:");
    await expect(runDesktopStorageMigration(e)).resolves.toBe(true);
    expect(fetchImpl).not.toHaveBeenCalled();
  });
});
