import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./client";

// Contract tests for the Postgres connector client. The component tests mock
// `api`, so they can only prove the panel agrees with the client — not that the
// client agrees with the Go handlers. These stub `fetch` and pin the WIRE keys.
//
// The bug this exists for: the server sends `from_env`, the interface declared
// `fromEnv`, and nothing mapped between them, so every env-backed connection
// looked sealed (and demanded a master password) in production.

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function stubFetch(body: unknown, status = 200) {
  const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => jsonResponse(body, status));
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("dbConnectList wire mapping", () => {
  it("maps the server's from_env onto fromEnv", async () => {
    stubFetch({
      connections: [
        { name: "env", driver: "postgres", unlocked: false, from_env: true },
        { name: "prod", driver: "postgres", unlocked: true, from_env: false },
      ],
    });

    const res = await api.dbConnectList("db-panel::/proj:p1");

    expect(res.connections).toEqual([
      { name: "env", driver: "postgres", unlocked: false, fromEnv: true },
      { name: "prod", driver: "postgres", unlocked: true, fromEnv: false },
    ]);
  });

  it("treats a missing from_env as a sealed connection, not undefined", async () => {
    stubFetch({ connections: [{ name: "prod", driver: "postgres", unlocked: false }] });

    const res = await api.dbConnectList("s1");

    expect(res.connections[0].fromEnv).toBe(false);
  });

  it("normalises a null connection list to an empty array", async () => {
    stubFetch({ connections: null });

    await expect(api.dbConnectList("s1")).resolves.toEqual({ connections: [] });
  });

  it("sends the surface as a query parameter", async () => {
    const fetchMock = stubFetch({ connections: [] });

    await api.dbConnectList("db-panel:host:/a b:p1");

    const url = String(fetchMock.mock.calls[0][0]);
    expect(url).toContain("/api/dbconnect/connections?surface=");
    expect(url).toContain(encodeURIComponent("db-panel:host:/a b:p1"));
  });
});

describe("dbConnectUnlock wire shape", () => {
  it("sends project_path, never session_id", async () => {
    const fetchMock = stubFetch({ unlocked: ["env"], failed: [] });

    await api.dbConnectUnlock("s1", "", "/proj");

    const body = JSON.parse(String(fetchMock.mock.calls[0][1]?.body));
    expect(body).toEqual({ surface: "s1", password: "", project_path: "/proj" });
    expect(body).not.toHaveProperty("session_id");
  });

  it("normalises null unlocked/failed to empty arrays", async () => {
    stubFetch({ unlocked: null, failed: null });

    await expect(api.dbConnectUnlock("s1", "", "/proj")).resolves.toEqual({
      unlocked: [],
      failed: [],
    });
  });

  it("keeps per-connection failures instead of failing the whole call", async () => {
    stubFetch({
      unlocked: ["keep"],
      failed: [{ name: "break", error: 'environment variable "DB_URL" is not set' }],
    });

    const res = await api.dbConnectUnlock("s1", "", "/proj");

    expect(res.unlocked).toEqual(["keep"]);
    expect(res.failed).toEqual([
      { name: "break", error: 'environment variable "DB_URL" is not set' },
    ]);
  });
});

describe("dbConnectAdd wire shape", () => {
  it("sends exactly name, url and password", async () => {
    const fetchMock = stubFetch({ name: "env" }, 201);

    await api.dbConnectAdd("env", "${DATABASE_URL}", "");

    const body = JSON.parse(String(fetchMock.mock.calls[0][1]?.body));
    expect(body).toEqual({ name: "env", url: "${DATABASE_URL}", password: "" });
  });
});
