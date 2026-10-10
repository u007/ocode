import { describe, expect, it, vi } from "vitest";
import { COMMANDS, dispatchCommand } from "./commands";

/**
 * /btw suite.
 *
 * /btw starts an INDEPENDENT side query on the server (a child agent with its
 * own client). Nothing is written to the transcript, so the command returns a
 * `btw` effect for App.tsx to open the docked panel — NOT a "Noted:" message.
 * The answer streams over the `btw` bus event.
 */
function ctx(api: Record<string, unknown> = {}, host?: string, sessionId = "s1") {
  return {
    commandName: "/btw",
    args: "",
    api,
    ...(host ? { host } : {}),
    getSessionId: () => sessionId,
  } as never;
}

describe("/btw command", () => {
  it("is listed in the picker", () => {
    expect(COMMANDS.some((c) => c.name === "/btw")).toBe(true);
  });

  it("returns a btw effect and does not add a Noted message", async () => {
    const btwSession = vi.fn(async () => ({ status: "started" }));
    const result = await dispatchCommand("/btw use tabs", ctx({ btwSession }));
    expect(btwSession).toHaveBeenCalledWith("s1", "use tabs", undefined);
    expect(result.btw).toEqual({ sessionId: "s1", question: "use tabs", host: undefined });
    expect(result.messages ?? []).toHaveLength(0);
  });

  it("threads the remote host into the effect", async () => {
    const btwSession = vi.fn(async () => ({ status: "started" }));
    const result = await dispatchCommand("/btw use tabs", ctx({ btwSession }, "devbox"));
    expect(btwSession).toHaveBeenCalledWith("s1", "use tabs", "devbox");
    expect(result.btw).toEqual({ sessionId: "s1", question: "use tabs", host: "devbox" });
  });

  it("keeps the usage message when no question is given", async () => {
    const btwSession = vi.fn();
    const result = await dispatchCommand("/btw", ctx({ btwSession }));
    expect(result.btw).toBeUndefined();
    expect(result.messages?.[0]?.content).toContain("Usage:");
    expect(btwSession).not.toHaveBeenCalled();
  });

  it("reports no active session without calling the API", async () => {
    const btwSession = vi.fn();
    const result = await dispatchCommand("/btw hi", ctx({ btwSession }, undefined, ""));
    expect(result.btw).toBeUndefined();
    expect(result.messages?.[0]?.content).toContain("No active session");
    expect(btwSession).not.toHaveBeenCalled();
  });

  it("surfaces a failure as an assistant message and no effect", async () => {
    const btwSession = vi.fn(async () => {
      throw new Error("boom");
    });
    const result = await dispatchCommand("/btw hi", ctx({ btwSession }));
    expect(result.btw).toBeUndefined();
    expect(result.messages?.[0]?.content).toContain("boom");
  });
});
