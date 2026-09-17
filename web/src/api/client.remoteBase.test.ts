import { describe, it, expect, vi, beforeEach } from "vitest";

// remoteApiBase is a pure helper; import directly.
// fetchJSON / api.chat are tested via mock fetch to verify path rewriting.

describe("remoteApiBase", () => {
  it("returns empty string for falsy host", async () => {
    const { remoteApiBase } = await import("./client");
    expect(remoteApiBase()).toBe("");
    expect(remoteApiBase("")).toBe("");
  });

  it("encodes user@host → /api/remote/user%40host", async () => {
    const { remoteApiBase } = await import("./client");
    expect(remoteApiBase("user@host")).toBe("/api/remote/user%40host");
  });

  it("encodes wsl:Ubuntu → /api/remote/wsl%3AUbuntu", async () => {
    const { remoteApiBase } = await import("./client");
    expect(remoteApiBase("wsl:Ubuntu")).toBe("/api/remote/wsl%3AUbuntu");
  });
});

describe("fetchJSON with host prefix", () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules();
    sessionStorage.clear();
    window.history.replaceState(null, "", "/");
    fetchSpy = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify({ ok: true }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    vi.stubGlobal("fetch", fetchSpy);
  });

  it("prefixes path with /api/remote/<host> when host is given", async () => {
    const { fetchJSON } = await import("./client");
    await fetchJSON("/api/sessions/x", undefined, "h");
    const url = fetchSpy.mock.calls[0][0] as string;
    expect(url).toBe("/api/remote/h/api/sessions/x");
  });

  it("sends X-Ocode-Project header when both host and projectPath are given", async () => {
    const { fetchJSON } = await import("./client");
    await fetchJSON("/api/sessions/x", { headers: {} }, "h", "/my/project");
    const headers = fetchSpy.mock.calls[0][1].headers as Headers;
    expect(headers.get("X-Ocode-Project")).toBe("/my/project");
  });

  it("fetches byte-identical URL without host (no prefix)", async () => {
    const { fetchJSON } = await import("./client");
    await fetchJSON("/api/sessions/x");
    const url = fetchSpy.mock.calls[0][0] as string;
    expect(url).toBe("/api/sessions/x");
  });
});

describe("api.session-scoped methods pass host", () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules();
    sessionStorage.clear();
    window.history.replaceState(null, "", "/");
    fetchSpy = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify({ ok: true }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    vi.stubGlobal("fetch", fetchSpy);
  });

  it("api.chat(..., host) issues request under the prefix", async () => {
    const { api } = await import("./client");
    await api.chat("hello", undefined, undefined, undefined, "/proj", "wsl:Deb");
    const url = fetchSpy.mock.calls[0][0] as string;
    expect(url).toBe("/api/remote/wsl%3ADeb/api/chat");
  });

  it("api.getSession prefixes the URL when host is given", async () => {
    // getSession doesn't take projectPath in the current signature,
    // but the prefix + host should be there.
    const { api } = await import("./client");
    await api.getSession("abc", undefined, "myhost");
    const url = fetchSpy.mock.calls[0][0] as string;
    expect(url).toBe("/api/remote/myhost/api/sessions/abc");
  });

  it("api.listProjectSessions uses prefix instead of ?host= query param", async () => {
    const { api } = await import("./client");
    await api.listProjectSessions("/some/path", "u@remote");
    const url = fetchSpy.mock.calls[0][0] as string;
    expect(url).toBe("/api/remote/u%40remote/api/projects/sessions?path=" + encodeURIComponent("/some/path"));
    // Must NOT contain ?host= in the query
    expect(url).not.toContain("host=");
  });

  it("api.listProjectSessions without host sends no prefix", async () => {
    const { api } = await import("./client");
    await api.listProjectSessions("/some/path");
    const url = fetchSpy.mock.calls[0][0] as string;
    expect(url).toBe("/api/projects/sessions?path=" + encodeURIComponent("/some/path"));
    expect(url).not.toContain("/api/remote/");
  });
});

describe("api session-scoped members forward host", () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules();
    sessionStorage.clear();
    window.history.replaceState(null, "", "/");
    fetchSpy = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify({ ok: true }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    vi.stubGlobal("fetch", fetchSpy);
  });

  // Locks the trailing-host slot contract for every session-scoped member, so a
  // future edit that drops or mis-slots the host argument fails here.
  it("prefixes the URL with /api/remote/<host> for every listed member", async () => {
    const { api } = await import("./client");
    const H = "user@host";
    const cases: Array<[string, () => Promise<unknown>]> = [
      ["getSession", () => api.getSession("s1", undefined, H)],
      ["truncateSession", () => api.truncateSession("s1", 1, H)],
      ["listModels", () => api.listModels(undefined, H)],
      ["listAgentRuns", () => api.listAgentRuns("s1", H)],
      ["setSessionModel", () => api.setSessionModel("s1", "m", H)],
      ["clearSessionModel", () => api.clearSessionModel("s1", H)],
      ["getSessionContext", () => api.getSessionContext("s1", H)],
      ["getSessionState", () => api.getSessionState("s1", H)],
      ["getSessionStatus", () => api.getSessionStatus("s1", H)],
      ["sendMessage", () => api.sendMessage("s1", "hi", H)],
      ["compactSession", () => api.compactSession("s1", H)],
      ["recapSession", () => api.recapSession("s1", H)],
      ["shareSession", () => api.shareSession("s1", H)],
      ["btwSession", () => api.btwSession("s1", "hi", H)],
      ["setSessionTitle", () => api.setSessionTitle("s1", "t", H)],
      ["generateSessionTitle", () => api.generateSessionTitle("s1", H)],
      ["cancelSession", () => api.cancelSession("s1", H)],
      ["closeSession", () => api.closeSession("s1", H)],
      ["resolvePermission", () => api.resolvePermission("r1", "s1", "allow", H)],
      ["answerQuestion", () => api.answerQuestion("r1", "s1", [], H)],
      ["listProjectSessions", () => api.listProjectSessions("/p", H)],
    ];
    for (const [name, call] of cases) {
      fetchSpy.mockClear();
      await call();
      const url = fetchSpy.mock.calls[0]?.[0] as string | undefined;
      expect(url, `${name} should be prefixed with the host`).toContain("/api/remote/user%40host/");
    }
  });

  it("sets X-Ocode-Project only when a host AND a project path are known", async () => {
    const { fetchJSON } = await import("./client");
    // Local request — no header.
    await fetchJSON("/api/sessions/x", { headers: {} });
    expect((fetchSpy.mock.calls[0][1].headers as Headers).get("X-Ocode-Project")).toBeNull();

    // Host but no project path — still no header.
    fetchSpy.mockClear();
    await fetchJSON("/api/sessions/x", { headers: {} }, "h");
    expect((fetchSpy.mock.calls[0][1].headers as Headers).get("X-Ocode-Project")).toBeNull();

    // Both — header set.
    fetchSpy.mockClear();
    await fetchJSON("/api/sessions/x", { headers: {} }, "h", "/proj");
    expect((fetchSpy.mock.calls[0][1].headers as Headers).get("X-Ocode-Project")).toBe("/proj");
  });

  it("keeps the remote prefix when a desktop backendBase is configured", async () => {
    const { fetchJSON, setApiBackendBase } = await import("./client");
    setApiBackendBase("http://localhost:9999");
    try {
      await fetchJSON("/api/sessions/x", undefined, "h");
      // The prefix must be applied BEFORE apiPath, or backendBase would be
      // interleaved with the remote prefix.
      expect(fetchSpy.mock.calls[0][0]).toBe("http://localhost:9999/api/remote/h/api/sessions/x");
    } finally {
      setApiBackendBase(null);
    }
  });
});
