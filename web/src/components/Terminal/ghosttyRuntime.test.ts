import { describe, it, expect, vi, beforeEach } from "vitest";

const load = vi.fn();
vi.mock("ghostty-web", () => ({ Ghostty: { load: () => load() } }));

// The module caches its instance at module scope, so every test starts from a fresh module.
beforeEach(() => {
  vi.resetModules();
  load.mockReset();
});

describe("loadGhostty", () => {
  it("shares one instance across calls", async () => {
    load.mockImplementation(async () => ({}));
    const { loadGhostty } = await import("./ghosttyRuntime");
    const a = await loadGhostty();
    const b = await loadGhostty();
    expect(a).toBe(b);
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("propagates a failed load", async () => {
    load.mockImplementation(async () => {
      throw new Error("wasm down");
    });
    const { loadGhostty } = await import("./ghosttyRuntime");
    await expect(loadGhostty()).rejects.toThrow("wasm down");
  });

  it("does not cache a failed load: the next call retries and resolves", async () => {
    load
      .mockImplementationOnce(async () => {
        throw new Error("wasm down");
      })
      .mockImplementationOnce(async () => ({ ok: true }));
    const { loadGhostty } = await import("./ghosttyRuntime");
    await expect(loadGhostty()).rejects.toThrow("wasm down");
    await expect(loadGhostty()).resolves.toEqual({ ok: true });
    expect(load).toHaveBeenCalledTimes(2);
  });
});
