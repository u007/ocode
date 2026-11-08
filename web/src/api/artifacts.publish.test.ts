import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * publishArtifact body mapping. `mode` is a legacy alias for `hosting_mode`, so
 * the access mode must travel as access_mode/accessMode only — a regression
 * here silently turns a password-protected publish into a public one.
 */
function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function publishedBody(call: unknown[]): Record<string, unknown> {
  const init = call[1] as RequestInit;
  return JSON.parse(init.body as string) as Record<string, unknown>;
}

describe("publishArtifact access fields", () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules();
    window.history.replaceState(null, "", "/");
    fetchSpy = vi.fn().mockResolvedValue(jsonResponse({ ok: true }));
    vi.stubGlobal("fetch", fetchSpy);
  });

  it("omits access fields when the caller sends none", async () => {
    const { publishArtifact } = await import("./artifacts");
    await publishArtifact("art_1", { notify_on_publish: false });
    const body = publishedBody(fetchSpy.mock.calls[0] as unknown[]);
    expect(body.notify_on_publish).toBe(false);
    expect(body).not.toHaveProperty("access_mode");
    expect(body).not.toHaveProperty("password");
    expect(body).not.toHaveProperty("invite_emails");
  });

  it("maps access_mode without touching hosting_mode", async () => {
    const { publishArtifact } = await import("./artifacts");
    await publishArtifact("art_1", { access_mode: "public" });
    const body = publishedBody(fetchSpy.mock.calls[0] as unknown[]);
    expect(body.access_mode).toBe("public");
    expect(body).not.toHaveProperty("hosting_mode");
  });

  it("maps camelCase accessMode and never treats mode as the access mode", async () => {
    const { publishArtifact } = await import("./artifacts");
    await publishArtifact("art_1", { accessMode: "password", mode: "hub" });
    const body = publishedBody(fetchSpy.mock.calls[0] as unknown[]);
    expect(body.access_mode).toBe("password");
    expect(body.hosting_mode).toBe("hub");
  });

  it("carries the password only when provided", async () => {
    const { publishArtifact } = await import("./artifacts");
    await publishArtifact("art_1", {
      access_mode: "password",
      password: "hunter2hunter2",
    });
    const body = publishedBody(fetchSpy.mock.calls[0] as unknown[]);
    expect(body.password).toBe("hunter2hunter2");
  });

  it("carries invite emails from any supported alias", async () => {
    const { publishArtifact } = await import("./artifacts");
    await publishArtifact("art_1", {
      access_mode: "allowlist",
      emails: ["a@example.com"],
    });
    const body = publishedBody(fetchSpy.mock.calls[0] as unknown[]);
    expect(body.invite_emails).toEqual(["a@example.com"]);
  });
});

describe("ensureArtifact", () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules();
    window.history.replaceState(null, "", "/");
    fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
  });

  function listResponse(artifacts: unknown[]): Response {
    return jsonResponse({ artifacts, total: artifacts.length });
  }

  it("reuses the existing artifact for the project", async () => {
    fetchSpy.mockResolvedValueOnce(
      listResponse([{ id: "art_1", project_path: "/p" }]),
    );
    const { ensureArtifact } = await import("./artifacts");
    const artifact = await ensureArtifact("/p");
    expect(artifact.id).toBe("art_1");
    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });

  it("creates an artifact when the project has none", async () => {
    fetchSpy
      .mockResolvedValueOnce(listResponse([]))
      .mockResolvedValueOnce(jsonResponse({ id: "art_new", project_path: "/p" }));
    const { ensureArtifact } = await import("./artifacts");
    const artifact = await ensureArtifact("/p", undefined, "hub");
    expect(artifact.id).toBe("art_new");
    const createInit = fetchSpy.mock.calls[1][1] as RequestInit;
    expect(createInit.method).toBe("POST");
    expect(JSON.parse(createInit.body as string)).toEqual({
      project: "/p",
      hosting_mode: "hub",
    });
  });

  it("prefers the artifact bound to the given host", async () => {
    fetchSpy.mockResolvedValueOnce(
      listResponse([
        { id: "art_local", project_path: "/p", project_host: "" },
        { id: "art_remote", project_path: "/p", project_host: "box" },
      ]),
    );
    const { ensureArtifact } = await import("./artifacts");
    expect((await ensureArtifact("/p", "box")).id).toBe("art_remote");
  });

  it("refuses an empty project path", async () => {
    const { ensureArtifact } = await import("./artifacts");
    await expect(ensureArtifact("   ")).rejects.toThrow(/no project path/);
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
