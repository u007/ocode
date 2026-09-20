import { describe, expect, it, vi } from "vitest";
import { COMMANDS, dispatchCommand } from "./commands";

/**
 * /reset-id suite.
 *
 * The server re-keys a chat to a new session id and the command must hand the
 * old/new pair back to App.tsx as `rekeyTo` so the open tab, draft and queue
 * follow the new id — the same plumbing a `new-*` tab uses on first send.
 */
function ctx(api: Record<string, unknown> = {}, host?: string) {
  return {
    commandName: "/reset-id",
    args: "",
    api,
    ...(host ? { host } : {}),
    getSessionId: () => "ses_old",
  } as never;
}

describe("/reset-id command", () => {
  it("is listed in the picker", () => {
    expect(COMMANDS.some((c) => c.name === "/reset-id")).toBe(true);
  });

  it("rekeys via the api and returns the old/new pair for App.tsx", async () => {
    const resetSessionId = vi.fn(async () => ({ old_id: "ses_old", new_id: "ses_new" }));
    const result = await dispatchCommand("/reset-id", ctx({ resetSessionId }));
    expect(result.handled).toBe(true);
    expect(resetSessionId).toHaveBeenCalledWith("ses_old", undefined);
    expect(result.rekeyTo).toEqual({ oldId: "ses_old", newId: "ses_new" });
    const content = result.messages?.[0]?.content ?? "";
    expect(content).toContain("ses_old");
    expect(content).toContain("ses_new");
  });

  it("routes the request to the session's remote host", async () => {
    const resetSessionId = vi.fn(async () => ({ old_id: "ses_old", new_id: "ses_new" }));
    await dispatchCommand("/reset-id", ctx({ resetSessionId }, "devbox"));
    expect(resetSessionId).toHaveBeenCalledWith("ses_old", "devbox");
  });

  it("reports no active session without calling the api", async () => {
    const resetSessionId = vi.fn();
    const result = await dispatchCommand("/reset-id", {
      commandName: "/reset-id",
      args: "",
      api: { resetSessionId },
      getSessionId: () => null,
    } as never);
    expect(result.handled).toBe(true);
    expect(resetSessionId).not.toHaveBeenCalled();
    expect(result.rekeyTo).toBeUndefined();
  });

  it("surfaces a failure as a handled error message", async () => {
    const resetSessionId = vi.fn(async () => {
      throw new Error("cannot reset while a turn is active");
    });
    const result = await dispatchCommand("/reset-id", ctx({ resetSessionId }));
    expect(result.handled).toBe(true);
    expect(result.rekeyTo).toBeUndefined();
    expect(result.messages?.[0]?.content ?? "").toContain("turn is active");
  });
});
