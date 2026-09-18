import { beforeEach, describe, expect, it, vi } from "vitest";

const statusBody = {
  host: "user@box",
  connected: true,
  version: "1.2.3",
  local_version: "2.0.0",
  outdated: true,
  pid: 42,
};

const terminalEntry = { id: "t1", title: "shell", pid: 99, started_at: "2026-09-18T00:00:00Z", attached: false };

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("remote host lifecycle client", () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules();
    sessionStorage.clear();
    window.history.replaceState(null, "", "/");
    fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
  });

  it("GETs the local status route and returns the parsed status", async () => {
    fetchSpy.mockResolvedValue(jsonResponse(statusBody));
    const { api } = await import("./client");
    const status = await api.getRemoteHostStatus("user@box");
    expect(fetchSpy.mock.calls[0][0]).toBe("/api/remote/user%40box/status");
    expect((fetchSpy.mock.calls[0][1] as RequestInit).method).toBe("GET");
    expect(status).toEqual(statusBody);
  });

  it("POSTs connect and restart to their local routes", async () => {
    fetchSpy.mockImplementation(() => Promise.resolve(jsonResponse(statusBody)));
    const { api } = await import("./client");
    await api.connectRemoteHost("user@box");
    await api.restartRemoteHost("user@box");
    expect(fetchSpy.mock.calls[0][0]).toBe("/api/remote/user%40box/connect");
    expect((fetchSpy.mock.calls[0][1] as RequestInit).method).toBe("POST");
    expect(fetchSpy.mock.calls[1][0]).toBe("/api/remote/user%40box/restart");
    expect((fetchSpy.mock.calls[1][1] as RequestInit).method).toBe("POST");
  });

  it("throws an error carrying the body's error and stage on 502", async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ error: "kill failed", stage: "remote-kill" }, 502));
    const { api } = await import("./client");
    await expect(api.restartRemoteHost("user@box")).rejects.toThrow("kill failed (stage: remote-kill)");
  });

  it("lists remote terminals through the proxy with project_path and the project header", async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ terminals: [terminalEntry] }));
    const { api } = await import("./client");
    const list = await api.listRemoteTerminals("user@box", "/srv/app");
    expect(fetchSpy.mock.calls[0][0]).toBe("/api/remote/user%40box/api/terminal?project_path=%2Fsrv%2Fapp");
    const headers = (fetchSpy.mock.calls[0][1] as RequestInit).headers as Headers;
    expect(headers.get("X-Ocode-Project")).toBe("/srv/app");
    expect(list).toEqual([terminalEntry]);
  });
});
