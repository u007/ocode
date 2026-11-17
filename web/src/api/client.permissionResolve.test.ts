import { beforeEach, describe, expect, it, vi } from "vitest";

/** /api/permissions/resolve client body: password is approval-only. */
function jsonResponse(body: unknown, status = 202): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("resolvePermission password body", () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules();
    fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
  });

  it("omits password for an ordinary ask", async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ session_id: "ses_1" }));
    const { api } = await import("./client");
    await api.resolvePermission("call-1", "ses_1", "allow");
    const body = JSON.parse((fetchSpy.mock.calls[0][1] as RequestInit).body as string);
    expect(body).not.toHaveProperty("password");
    expect(body).toMatchObject({ request_id: "call-1", session_id: "ses_1", decision: "allow" });
  });

  it("includes password only when one is supplied for an approval", async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ session_id: "ses_1" }));
    const { api } = await import("./client");
    await api.resolvePermission("call-1", "ses_1", "allow", undefined, undefined, "hunter2-elev-secret");
    const body = JSON.parse((fetchSpy.mock.calls[0][1] as RequestInit).body as string);
    expect(body.password).toBe("hunter2-elev-secret");
  });

  it("routes an elevation resolve with its password through the execution host", async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ session_id: "ses_1" }));
    const { api } = await import("./client");
    await api.resolvePermission("call-1", "ses_1", "allow", "user@remote", undefined, "remote-secret");
    expect(fetchSpy.mock.calls[0][0]).toContain("/api/remote/user%40remote/api/permissions/resolve");
    const body = JSON.parse((fetchSpy.mock.calls[0][1] as RequestInit).body as string);
    expect(body.password).toBe("remote-secret");
  });
});
