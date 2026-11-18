import { describe, expect, it, vi } from "vitest";
import { dispatchCommand } from "./commands";

/**
 * /docs cleanup is an LLM pass, not a preset list: without --yes the server
 * returns a context-agent audit prompt that the client must send as a turn.
 * --yes stays a local, deterministic delete of already-deprecated docs.
 */
describe("/docs cleanup", () => {
  it("dispatches the audit prompt as a turn", async () => {
    const docsCleanup = vi.fn(async () => ({
      result: "Auditing the knowledge bundle for cleanup candidates...",
      cleanup_prompt: "You are auditing this project's OKF knowledge bundle (docs/)...",
    }));
    const ctx = {
      commandName: "docs",
      args: "cleanup",
      api: { docsCleanup } as never,
      projectPath: "/repo",
      host: "devbox",
      getSessionId: () => "ses_1",
    } as never;

    const result = await dispatchCommand("/docs cleanup", ctx);

    expect(docsCleanup).toHaveBeenCalledWith(false, "/repo", "devbox");
    expect(result.handled).toBe(true);
    expect(result.prompt).toContain("OKF knowledge bundle");
    expect(result.messages?.[0]?.content).toContain("Auditing");
  });

  it("deletes on --yes without dispatching a turn", async () => {
    const docsCleanup = vi.fn(async () => ({
      result: "Deleted 2 deprecated document(s). Index and log updated.",
    }));
    const ctx = {
      commandName: "docs",
      args: "cleanup --yes",
      api: { docsCleanup } as never,
      projectPath: "/repo",
      host: "devbox",
      getSessionId: () => "ses_1",
    } as never;

    const result = await dispatchCommand("/docs cleanup --yes", ctx);

    expect(docsCleanup).toHaveBeenCalledWith(true, "/repo", "devbox");
    expect(result.handled).toBe(true);
    expect(result.prompt).toBeUndefined();
    expect(result.messages?.[0]?.content).toContain("Deleted 2");
  });
});
