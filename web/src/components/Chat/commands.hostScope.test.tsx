import { describe, expect, it, vi } from "vitest";
import { dispatchCommand } from "./commands";

/**
 * Command-context routing suite.
 *
 * A slash command runs against the ACTIVE tab's project, which may be remote
 * (SSH/WSL) or a non-default local project. Every repo/session-scoped read the
 * command context performs must carry `ctx.host` and `ctx.projectPath`;
 * without them the local server answers with its default workdir (or 404s for
 * a remote session's id), which is what made /lsp, /mcp, /session list,
 * /changes and /docs report the wrong project.
 */
function ctx(
  api: Record<string, unknown>,
  opts: { host?: string; projectPath?: string; sessionId?: string } = {},
) {
  return {
    commandName: "",
    args: "",
    api,
    ...(opts.host ? { host: opts.host } : {}),
    ...(opts.projectPath ? { projectPath: opts.projectPath } : {}),
    ...(opts.sessionId ? { getSessionId: () => opts.sessionId } : {}),
  } as never;
}

const HOST = "devbox";
const PROJECT = "/home/j/src";

describe("command-context host/project routing", () => {
  it("/lsp reads the active tab's host", async () => {
    const getLSPStatuses = vi.fn(async () => ({ lsp_servers: [] }));
    await dispatchCommand("/lsp", ctx({ getLSPStatuses }, { host: HOST }));
    expect(getLSPStatuses).toHaveBeenCalledWith(HOST);
  });

  it("/mcp reads the active tab's host", async () => {
    const getMCP = vi.fn(async () => []);
    await dispatchCommand("/mcp", ctx({ getMCP }, { host: HOST }));
    // The session id is threaded after the host so the server can apply the
    // chat's per-session MCP overrides; no session in this ctx, hence undefined.
    expect(getMCP).toHaveBeenCalledWith(HOST, undefined);
  });

  it("/session list reads the active tab's host", async () => {
    const listSessions = vi.fn(async () => []);
    await dispatchCommand(
      "/session list",
      ctx({ listSessions, getSession: vi.fn() }, { host: HOST }),
    );
    expect(listSessions).toHaveBeenCalledWith(HOST);
  });

  it("/changes assembles its prompt from the active tab's project + host", async () => {
    const getCommandContext = vi.fn(async () => ({ prompt: "p" }));
    await dispatchCommand(
      "/changes",
      ctx({ getCommandContext }, { host: HOST, projectPath: PROJECT }),
    );
    expect(getCommandContext).toHaveBeenCalledWith("changes", undefined, PROJECT, HOST);
  });

  it("/docs status reads the active tab's project + host", async () => {
    const getDocsStatus = vi.fn(async () => ({ enabled: true, text: "docs" }));
    await dispatchCommand(
      "/docs status",
      ctx({ getDocsStatus }, { host: HOST, projectPath: PROJECT }),
    );
    expect(getDocsStatus).toHaveBeenCalledWith(PROJECT, HOST);
  });

  it("a local tab passes an undefined host (unchanged local behavior)", async () => {
    const getMCP = vi.fn(async () => []);
    await dispatchCommand("/mcp", ctx({ getMCP }));
    expect(getMCP).toHaveBeenCalledWith(undefined, undefined);
  });

  it("/mcp passes the active session id so per-chat MCP overrides apply", async () => {
    const getMCP = vi.fn(async () => []);
    await dispatchCommand("/mcp", ctx({ getMCP }, { host: HOST, sessionId: "ses_abc" }));
    expect(getMCP).toHaveBeenCalledWith(HOST, "ses_abc");
  });
});
