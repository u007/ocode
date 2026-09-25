import { beforeEach, describe, expect, it, vi } from "vitest";

// Phase 06 client contract for the two new git endpoints. The critical
// property is host threading: omitting the host must stay LOCAL, and supplying
// one must route through the remote proxy, exactly like every other git call.
// These tests assert call arity explicitly, so an added optional argument has
// to be passed as undefined rather than omitted.

describe("git conflict + operation API client", () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules();
    sessionStorage.clear();
    window.history.replaceState(null, "", "/");
    fetchSpy = vi.fn(async () => new Response(JSON.stringify({}), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }));
    vi.stubGlobal("fetch", fetchSpy);
  });

  it("resolves a conflict locally when no host is given", async () => {
    const { api } = await import("./client");
    await api.gitResolveConflict({ path: "f.txt", resolution: "ours" }, "/repo", undefined);

    expect(fetchSpy).toHaveBeenCalledWith(
      "/api/git/conflict/resolve?project=%2Frepo",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ path: "f.txt", resolution: "ours" }),
      }),
    );
  });

  // Host travels as a QUERY PARAM, not a URL prefix: every existing git call
  // uses projQuery(project, host) and the local server reverse-proxies on that
  // basis. Prefixing here would have produced a URL no other git call uses.
  it("resolves a conflict with the host threaded as a query param", async () => {
    const { api } = await import("./client");
    await api.gitResolveConflict({ path: "f.txt", resolution: "theirs" }, "/repo", "devbox");

    expect(fetchSpy).toHaveBeenCalledWith(
      "/api/git/conflict/resolve?project=%2Frepo&host=devbox",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ path: "f.txt", resolution: "theirs" }),
      }),
    );
  });

  it("runs an operation locally when no host is given", async () => {
    const { api } = await import("./client");
    await api.gitOperation({ action: "abort", kind: "merge" }, "/repo", undefined);

    expect(fetchSpy).toHaveBeenCalledWith(
      "/api/git/operation?project=%2Frepo",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ action: "abort", kind: "merge" }),
      }),
    );
  });

  it("runs an operation with the host threaded as a query param", async () => {
    const { api } = await import("./client");
    await api.gitOperation({ action: "continue", kind: "rebase" }, "/repo", "devbox");

    expect(fetchSpy).toHaveBeenCalledWith(
      "/api/git/operation?project=%2Frepo&host=devbox",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ action: "continue", kind: "rebase" }),
      }),
    );
  });
});
