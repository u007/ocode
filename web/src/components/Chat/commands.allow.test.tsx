import { beforeEach, describe, expect, it, vi } from "vitest";
import { COMMANDS, dispatchCommand } from "./commands";

/**
 * /allow on web + desktop (TUI parity for `runAllowCmd`).
 *
 * The TUI has had `/allow [list|add|remove]` since the permission-rules
 * rework; the web/desktop SPA only had `/ban`, so a prefix could be
 * pre-approved only from a live permission ask or Settings.
 *
 * Rules this suite pins:
 *  - a prefix is joined from the remaining words ("git push"), not split
 *    per word like /ban;
 *  - remove touches an ALLOW rule only (a ban belongs to /ban);
 *  - the server's validation reason (bare `git` allow) is surfaced, because
 *    SetBashPrefixRule silently discards an invalid rule;
 *  - every read/write carries `ctx.host`, so a remote session's rule lands on
 *    the server that actually runs it.
 */
const getPermissions = vi.fn();
const setBashRule = vi.fn();
const setBashRules = vi.fn();

function ctx(opts: { host?: string } = {}) {
  return {
    commandName: "allow",
    args: "",
    getSessionId: () => "ses_allow",
    ...(opts.host ? { host: opts.host } : {}),
    api: { getPermissions, setBashRule, setBashRules },
  } as never;
}

function perms(bashRules: { tool: string; level: string }[]) {
  return {
    mode: "normal",
    auto_allow: false,
    sandbox_supported: true,
    effective_behavior: "normal",
    rules: [],
    bash_rules: bashRules,
  };
}

function text(res: { messages?: { content?: string }[] }): string {
  return res.messages?.[0]?.content ?? "";
}

describe("/allow command", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getPermissions.mockResolvedValue(perms([]));
  });

  it("is registered in the picker (and therefore dispatched, not sent to the LLM)", () => {
    expect(COMMANDS.some((c) => c.name === "/allow")).toBe(true);
  });

  it("list shows allowed prefixes only, never bans or asks", async () => {
    getPermissions.mockResolvedValue(
      perms([
        { tool: "git push", level: "allow" },
        { tool: "rm", level: "deny" },
        { tool: "npm test", level: "allow" },
        { tool: "curl", level: "ask" },
      ]),
    );
    const res = await dispatchCommand("/allow list", ctx());
    expect(res.handled).toBe(true);
    expect(text(res)).toContain("git push");
    expect(text(res)).toContain("npm test");
    expect(text(res)).not.toContain("rm");
    expect(text(res)).not.toContain("curl");
  });

  it("bare /allow reports (none) when nothing is allowed", async () => {
    const res = await dispatchCommand("/allow", ctx());
    expect(res.handled).toBe(true);
    expect(text(res)).toContain("(none)");
  });

  it("add joins the remaining words into ONE multi-word prefix", async () => {
    const res = await dispatchCommand("/allow add git push", ctx());
    expect(setBashRule).toHaveBeenCalledWith("git push", "allow", undefined);
    expect(setBashRules).not.toHaveBeenCalled();
    expect(text(res)).toContain("Allowed bash command prefix");
  });

  it("a bare /allow <cmd…> is shorthand for add", async () => {
    await dispatchCommand("/allow npm test", ctx());
    expect(setBashRule).toHaveBeenCalledWith("npm test", "allow", undefined);
  });

  it("normalizes the prefix to lowercase, matching /ban's rule key", async () => {
    await dispatchCommand("/allow add Git Push", ctx());
    expect(setBashRule).toHaveBeenCalledWith("git push", "allow", undefined);
  });

  it("reports an already-allowed prefix without writing again", async () => {
    getPermissions.mockResolvedValue(perms([{ tool: "git push", level: "allow" }]));
    const res = await dispatchCommand("/allow add git push", ctx());
    expect(setBashRule).not.toHaveBeenCalled();
    expect(text(res)).toContain("already allowed");
  });

  it("says it replaced the ban when the same prefix was denied", async () => {
    getPermissions.mockResolvedValue(perms([{ tool: "sed", level: "deny" }]));
    const res = await dispatchCommand("/allow add sed", ctx());
    expect(setBashRule).toHaveBeenCalledWith("sed", "allow", undefined);
    expect(text(res)).toContain("replaced its ban");
  });

  it("surfaces the server's validation reason instead of a generic failure", async () => {
    // The server refuses a bare `git` allow: it would auto-approve every git
    // subcommand, including revert and stash.
    const boom = Object.assign(
      new Error('"git" cannot be always-allowed: use a two-word rule such as "git status"'),
      { status: 400 },
    );
    setBashRule.mockRejectedValueOnce(boom);
    const res = await dispatchCommand("/allow add git", ctx());
    expect(res.handled).toBe(true);
    expect(text(res)).toContain('"git" cannot be always-allowed');
    expect(text(res)).not.toContain("/allow failed");
  });

  it("remove deletes an allow rule through the batch delta (a real DELETE)", async () => {
    getPermissions.mockResolvedValue(perms([{ tool: "git push", level: "allow" }]));
    const res = await dispatchCommand("/allow remove git push", ctx());
    expect(setBashRules).toHaveBeenCalledWith({ remove: ["git push"] }, undefined);
    expect(setBashRule).not.toHaveBeenCalled();
    expect(text(res)).toContain("Removed bash rule");
  });

  it("remove refuses to touch a ban (that is /ban's job)", async () => {
    getPermissions.mockResolvedValue(perms([{ tool: "sed", level: "deny" }]));
    const res = await dispatchCommand("/allow remove sed", ctx());
    expect(setBashRules).not.toHaveBeenCalled();
    expect(text(res)).toContain("not an allowed bash prefix");
  });

  it("threads the remote host through the read and every write", async () => {
    getPermissions.mockResolvedValue(perms([{ tool: "git push", level: "allow" }]));
    await dispatchCommand("/allow add npm test", ctx({ host: "devbox" }));
    expect(getPermissions).toHaveBeenCalledWith("ses_allow", "devbox");
    expect(setBashRule).toHaveBeenCalledWith("npm test", "allow", "devbox");

    await dispatchCommand("/allow remove git push", ctx({ host: "devbox" }));
    expect(setBashRules).toHaveBeenCalledWith({ remove: ["git push"] }, "devbox");
  });
});
