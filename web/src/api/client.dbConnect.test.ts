import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, api } from "./client";

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

    await api.dbConnectUnlock("s1", "", undefined, "/proj");

    const body = JSON.parse(String(fetchMock.mock.calls[0][1]?.body));
    expect(body).toEqual({ surface: "s1", password: "", project_path: "/proj" });
    expect(body).not.toHaveProperty("session_id");
  });

  it("normalises null unlocked/failed to empty arrays", async () => {
    stubFetch({ unlocked: null, failed: null });

    await expect(api.dbConnectUnlock("s1", "", undefined, "/proj")).resolves.toEqual({
      unlocked: [],
      failed: [],
    });
  });

  it("keeps per-connection failures instead of failing the whole call", async () => {
    stubFetch({
      unlocked: ["keep"],
      failed: [{ name: "break", error: 'environment variable "DB_URL" is not set' }],
    });

    const res = await api.dbConnectUnlock("s1", "", undefined, "/proj");

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

describe("dbConnectUnlock project scope", () => {
  // The runtime guard is for untyped callers, so the test reaches it through a loose view.
  const unlockUntyped = api.dbConnectUnlock as unknown as (
    surface: string,
    password: string,
    ...scope: unknown[]
  ) => Promise<unknown>;

  it("sends a local project's path to this server with no host header", async () => {
    const fetchMock = stubFetch({ unlocked: [], failed: [] });

    await api.dbConnectUnlock("s1", "", undefined, "/proj");

    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain("/api/dbconnect/unlock");
    expect(String(url)).not.toContain("/api/remote/");
    expect(new Headers(init?.headers).has("X-Ocode-Project")).toBe(false);
  });

  it("routes a remote scope to its host, sending the path as header and body", async () => {
    const fetchMock = stubFetch({ unlocked: [], failed: [] });

    await api.dbConnectUnlock("s1", "", undefined, "/srv/app", { host: "me@box", projectPath: "/srv/app" });

    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain("/api/remote/me%40box/api/dbconnect/unlock");
    expect(new Headers(init?.headers).get("X-Ocode-Project")).toBe("/srv/app");
    expect(JSON.parse(String(init?.body)).project_path).toBe("/srv/app");
  });

  it("throws, without calling the server, for a remote path given without its host", () => {
    const fetchMock = stubFetch({ unlocked: [], failed: [] });

    expect(() => unlockUntyped("s1", "", undefined, "/srv/app", { projectPath: "/srv/app" })).toThrow(
      "without its host",
    );
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("throws, without calling the server, for a remote host given without its path", () => {
    const fetchMock = stubFetch({ unlocked: [], failed: [] });

    expect(() => unlockUntyped("s1", "", undefined, undefined, { host: "me@box", projectPath: "/srv/app" })).toThrow(
      "without its project path",
    );
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("rejects both half-specified remote scopes at compile time", () => {
    // Compile-time contract only: the closure is never invoked.
    const neverCalled = () => {
      // @ts-expect-error a remote host needs its project path
      api.dbConnectUnlock("s1", "", undefined, { host: "me@box", projectPath: "/srv/app" });
      // @ts-expect-error a remote project path needs its host
      api.dbConnectUnlock("s1", "", undefined, "/srv/app", { projectPath: "/srv/app" });
    };
    expect(neverCalled).toBeTypeOf("function");
  });
});

describe("lock and remove keep the HTTP status", () => {
  it("dbConnectRemove throws an ApiError carrying the status and the server's message", async () => {
    stubFetch({ error: "no such connection" }, 404);

    const err: unknown = await api.dbConnectRemove("prod").catch((e: unknown) => e);

    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 404, message: "no such connection" });
  });

  it("dbConnectLock on a remote host keeps the 405 the host answered", async () => {
    stubFetch({ error: "method not allowed" }, 405);

    await expect(
      api.dbConnectLock("s1", { host: "me@box", projectPath: "/srv/app" }),
    ).rejects.toMatchObject({ status: 405, message: "method not allowed" });
  });
});
