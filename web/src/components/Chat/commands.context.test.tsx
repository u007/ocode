import { describe, expect, it, vi } from "vitest";
import { dispatchCommand } from "./commands";

type SessionContext = {
  session_id: string;
  message_count: number;
  estimated_tokens: number;
  max_tokens?: number;
  model?: string;
  /** The shared /context breakdown (internal/contextbudget). Present only when a
   *  live agent was available and not mid-turn. */
  report?: {
    model: string;
    sections: {
      title: string;
      note?: string;
      rows: { label: string; value?: string; lines?: string[]; raw?: boolean; subhead?: boolean }[];
    }[];
    notes?: string[];
  };
};

/**
 * /context regression suite.
 *
 * A brand-new chat tab carries a client-only temp id (`new-<timestamp>`) until
 * the first message creates a real session on the server. Before this guard,
 * running /context in a fresh tab sent that temp id to
 * GET /api/sessions/:id/context and surfaced the server's raw 404 as a
 * "context command failed: session not found" message.
 */
function context(opts: {
  sessionId?: string | null;
  host?: string;
  getSessionContext?: (id: string, host?: string) => Promise<SessionContext>;
} = {}) {
  const hasSessionId = "sessionId" in opts;
  const getSessionContext =
    opts.getSessionContext ??
    vi.fn(async (id: string) => ({
      session_id: id,
      message_count: 0,
      estimated_tokens: 0,
    }));
  return {
    ctx: {
      commandName: "context",
      args: "",
      api: { getSessionContext },
      ...(opts.host ? { host: opts.host } : {}),
      ...(hasSessionId ? { getSessionId: () => opts.sessionId ?? null } : {}),
    } as never,
    getSessionContext,
  };
}

describe("/context command", () => {
  it("answers a fresh temp tab without calling the server", async () => {
    const { ctx, getSessionContext } = context({ sessionId: "new-1758000000000" });
    const result = await dispatchCommand("/context", ctx);
    expect(result.handled).toBe(true);
    expect(result.messages?.[0]?.content).toContain("send a message first");
    expect(getSessionContext).not.toHaveBeenCalled();
  });

  it("answers when there is no active session at all", async () => {
    const { ctx, getSessionContext } = context();
    const result = await dispatchCommand("/context", ctx);
    expect(result.messages?.[0]?.content).toContain("send a message first");
    expect(getSessionContext).not.toHaveBeenCalled();
  });

  it("reports the token budget for a real session", async () => {
    const getSessionContext = vi.fn(async (id: string) => ({
      session_id: id,
      message_count: 7,
      estimated_tokens: 12345,
      max_tokens: 200000,
      model: "opencode-go/deepseek-v4.1-flash",
    }));
    const { ctx } = context({
      sessionId: "ses_2026-01-02-030405-abcd",
      getSessionContext,
    });
    const result = await dispatchCommand("/context", ctx);
    const content = result.messages?.[0]?.content ?? "";
    expect(getSessionContext).toHaveBeenCalledWith("ses_2026-01-02-030405-abcd", undefined);
    expect(content).toContain("## Context Budget");
    expect(content).toContain("opencode-go/deepseek-v4.1-flash");
    expect(content).toContain("~12,345");
    expect(content).toContain("(6% used)");
  });

  it("passes the session's host so a remote project's budget comes from that host", async () => {
    const getSessionContext = vi.fn(async (id: string) => ({
      session_id: id,
      message_count: 1,
      estimated_tokens: 10,
    }));
    const { ctx } = context({ sessionId: "ses_remote", host: "devbox", getSessionContext });
    await dispatchCommand("/context", ctx);
    expect(getSessionContext).toHaveBeenCalledWith("ses_remote", "devbox");
  });

  it("renders the shared breakdown report when the server returns one", async () => {
    const getSessionContext = vi.fn(async (id: string) => ({
      session_id: id,
      message_count: 3,
      estimated_tokens: 1000,
      max_tokens: 200000,
      model: "openai/gpt-4o",
      report: {
        model: "openai/gpt-4o",
        sections: [
          {
            title: "Base Prompt",
            rows: [
              { label: "Environment", value: "~1.2k tok" },
              {
                label: "Provider prompt (openai/gpt-4o)",
                value: "~80 tok",
                raw: true,
                lines: ["you are a helpful agent"],
              },
            ],
          },
          {
            title: "Session Messages",
            rows: [
              { label: "Usage", value: "In 100  Cache 40 (28.6%)  Out 20", lines: ["$0.0100"] },
            ],
          },
          {
            title: "Discovery — [ocode:discovery] injected block",
            rows: [
              { label: "Corpus (names-index, stable — injected every turn)", subhead: true },
              { label: "Skills in index", value: "3" },
            ],
          },
        ],
        notes: ["no live agent — values are estimated from the persisted transcript"],
      },
    }));
    const { ctx } = context({
      sessionId: "ses_2026-01-02-030405-abcd",
      getSessionContext,
    });
    const result = await dispatchCommand("/context", ctx);
    const content = result.messages?.[0]?.content ?? "";
    // Section headings + rows come straight from the shared Report, so the web
    // shows the same values the TUI's local /context renders.
    expect(content).toContain("### Base Prompt");
    expect(content).toContain("- Environment — `~1.2k tok`");
    expect(content).toContain("- Skills in index — `3`");
    expect(content).toContain("**Corpus (names-index, stable — injected every turn)**");
    // Raw multi-line dumps are fenced.
    expect(content).toContain("```");
    expect(content).toContain("you are a helpful agent");
    // Non-raw detail lines nest as sub-bullets, not a code block.
    expect(content).toContain("  - $0.0100");
    expect(content).toContain("no live agent");
    // The report path must not fall back to the four-field summary.
    expect(content).not.toContain("- **Estimated tokens:**");
  });

  it("explains a missing session instead of leaking the raw 404", async () => {
    const notFound = Object.assign(new Error("session not found"), { status: 404 });
    const { ctx } = context({
      sessionId: "ses_2026-01-02-030405-gone",
      getSessionContext: async () => {
        throw notFound;
      },
    });
    const result = await dispatchCommand("/context", ctx);
    const content = result.messages?.[0]?.content ?? "";
    expect(content).toContain("isn't on the server");
    expect(content).not.toContain("/context failed:");
    expect(content).not.toContain("session not found");
  });

  it("still surfaces non-404 failures verbatim", async () => {
    const boom = Object.assign(new Error("boom"), { status: 500 });
    const { ctx } = context({
      sessionId: "ses_2026-01-02-030405-abcd",
      getSessionContext: async () => {
        throw boom;
      },
    });
    const result = await dispatchCommand("/context", ctx);
    expect(result.messages?.[0]?.content).toContain("**/context failed:** boom");
  });
});

/**
 * Coverage for every built-in command that acts on the *active* session id.
 * Each must treat a `new-*` tab as "no session" and answer without touching
 * its session endpoint (the 404 that produced the original report).
 */
describe("session-scoped commands treat a new-* tab as no session", () => {
  const cases: { cmd: string; expected: string; spy?: string }[] = [
    { cmd: "/context", expected: "No active session yet", spy: "getSessionContext" },
    { cmd: "/compact", expected: "No active session to compact", spy: "compactSession" },
    { cmd: "/recap", expected: "No active session to recap", spy: "recapSession" },
    { cmd: "/share", expected: "No active session to share", spy: "shareSession" },
    { cmd: "/btw hello", expected: "No active session to add a note to", spy: "btwSession" },
    { cmd: "/docs update focus", expected: "No active session — open a chat first", spy: "docsUpdate" },
    // /export, /export-claude and /title call the module-level `api` rather
    // than ctx.api, so assert on the guard's message instead of a spy.
    { cmd: "/export", expected: "the session is empty" },
    { cmd: "/export-claude", expected: "the session is empty" },
    { cmd: "/title My title", expected: "No active session to title" },
  ];

  it.each(cases)("$cmd", async ({ cmd, expected, spy }) => {
    const api = {
      getSessionContext: vi.fn(),
      compactSession: vi.fn(),
      recapSession: vi.fn(),
      shareSession: vi.fn(),
      btwSession: vi.fn(),
      docsUpdate: vi.fn(),
    };
    const ctx = {
      commandName: cmd.split(" ")[0],
      args: cmd.slice(cmd.indexOf(" ") + 1),
      api: api as never,
      getSessionId: () => "new-1758000000000",
    } as never;

    const result = await dispatchCommand(cmd, ctx);
    expect(result.handled).toBe(true);
    expect(result.messages?.[0]?.content).toContain(expected);
    if (spy) {
      expect(api[spy as keyof typeof api]).not.toHaveBeenCalled();
    }
  });
});

/**
 * /agents regression suite: the subagent registry is per server, so a remote
 * session's view must read the host's registry. A host-less read returned the
 * local server's empty list and printed "No active or queued subagents."
 */
describe("/agents command", () => {
  it("reads the run tree from the session's host", async () => {
    const getAgentRuns = vi.fn(async () => [
      { id: "run-1", agent: "explore", status: "running" },
    ]);
    const ctx = {
      commandName: "agents",
      args: "",
      api: { getAgentRuns } as never,
      host: "devbox",
      getSessionId: () => "ses_remote",
    } as never;

    const result = await dispatchCommand("/agents", ctx);
    expect(result.handled).toBe(true);
    expect(getAgentRuns).toHaveBeenCalledWith("devbox");
    expect(result.messages?.[0]?.content).toContain("explore");
  });
});
