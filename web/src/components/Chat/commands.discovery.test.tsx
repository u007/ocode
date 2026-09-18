import { describe, expect, it, vi } from "vitest";
import { dispatchCommand } from "./commands";

const CONFIG = {
  enabled: true,
  embedding_model: "bge-m3",
  embedding_backend: "local",
  local_model_status: "ready",
  local_server_url: "",
  pinned_skills: [] as string[],
  ignore_paths: ["dist/"],
};

function status(over: Partial<Record<string, unknown>> = {}) {
  return {
    ...CONFIG,
    live: true,
    active: true,
    judge_vetoed: 0,
    mcp_total: 0,
    skill_total: 0,
    attached_skills: [],
    attached_mcp: [],
    attached_md: [],
    all_skills: [],
    all_mcp: [],
    all_md: [],
    md_pending: 0,
    ...over,
  } as never;
}

/**
 * /discover regression suite.
 *
 * The web handler used to render config only, so a user could not tell whether
 * discovery was actually indexing anything (the TUI's /discover status shows
 * the live corpus + attached sets). These pin the config half plus the new
 * runtime half, including every degradation path.
 */
function discovery(opts: {
  sessionId?: string | null;
  host?: string;
  getDiscoveryStatus?: (id: string, host?: string) => Promise<unknown>;
  getDiscoveryConfig?: () => Promise<unknown>;
} = {}) {
  const hasSessionId = "sessionId" in opts;
  const setDiscoveryConfig = vi.fn(async (c: unknown) => c);
  const getDiscoveryConfig = opts.getDiscoveryConfig ?? vi.fn(async () => CONFIG);
  return {
    ctx: {
      api: {
        getDiscoveryConfig,
        setDiscoveryConfig,
        ...(opts.getDiscoveryStatus ? { getDiscoveryStatus: opts.getDiscoveryStatus } : {}),
      },
      ...(opts.host ? { host: opts.host } : {}),
      ...(hasSessionId ? { getSessionId: () => opts.sessionId ?? null } : {}),
    } as never,
    getDiscoveryConfig,
    setDiscoveryConfig,
  };
}

describe("/discover command", () => {
  it("shows config plus live runtime status for a real session", async () => {
    const getDiscoveryStatus = vi.fn(async () =>
      status({
        judge: "typesafe/jev-latest",
        judge_vetoed: 2,
        mcp_total: 5,
        skill_total: 2,
        attached_skills: ["kaizen-review"],
        attached_mcp: ["Notion/search"],
        all_skills: ["kaizen-review", "ocode-mem"],
        all_mcp: ["Notion/search", "github/pr"],
        all_md: ["docs/README.md"],
        md_pending: 1,
      }),
    );
    const { ctx } = discovery({ sessionId: "ses_real", getDiscoveryStatus });
    const result = await dispatchCommand("/discover status", ctx);
    const content = result.messages?.[0]?.content ?? "";

    expect(getDiscoveryStatus).toHaveBeenCalledWith("ses_real", undefined);
    expect(content).toContain("## Codebase Discovery");
    expect(content).toContain("**Enabled:** true");
    expect(content).toContain("### Runtime status");
    expect(content).toContain("**Active:** yes");
    expect(content).toContain("typesafe/jev-latest");
    expect(content).toContain("vetoed 2 this session");
    expect(content).toContain("Skills in index:** 2");
    expect(content).toContain("Docs pending summarization:** 1");
    // ● attached / ○ name-only markers mirror the TUI's showDiscoverStatus.
    expect(content).toContain("● kaizen-review");
    expect(content).toContain("○ ocode-mem");
  });

  it("passes the session's host so a remote project's status comes from that host", async () => {
    const getDiscoveryStatus = vi.fn(async () => status());
    const { ctx } = discovery({ sessionId: "ses_real", host: "buildbox", getDiscoveryStatus });
    await dispatchCommand("/discover status", ctx);
    expect(getDiscoveryStatus).toHaveBeenCalledWith("ses_real", "buildbox");
  });

  it("notes the missing live agent instead of inventing an index", async () => {
    const getDiscoveryStatus = vi.fn(async () => status({ live: false, active: false }));
    const { ctx } = discovery({ sessionId: "ses_idle", getDiscoveryStatus });
    const result = await dispatchCommand("/discover status", ctx);
    const content = result.messages?.[0]?.content ?? "";
    expect(content).toContain("No live agent");
    expect(content).not.toContain("### Runtime status\n- **Active:** yes");
  });

  it("degrades to config only when the status endpoint is unavailable", async () => {
    const getDiscoveryStatus = vi.fn(async () => {
      throw new Error("404 not found");
    });
    const { ctx } = discovery({ sessionId: "ses_old", getDiscoveryStatus });
    const result = await dispatchCommand("/discover status", ctx);
    const content = result.messages?.[0]?.content ?? "";
    expect(content).toContain("Runtime status unavailable");
    expect(content).toContain("**Enabled:** true");
  });

  it("does not query runtime status for a temp tab with no real session", async () => {
    const getDiscoveryStatus = vi.fn(async () => status());
    const { ctx } = discovery({ sessionId: "new-1758000000000", getDiscoveryStatus });
    const result = await dispatchCommand("/discover", ctx);
    expect(getDiscoveryStatus).not.toHaveBeenCalled();
    expect(result.messages?.[0]?.content).toContain("## Codebase Discovery");
  });

  it("still enables and disables via the config PUT", async () => {
    const { ctx, setDiscoveryConfig } = discovery({ sessionId: "ses_real" });
    const result = await dispatchCommand("/discover disable", ctx);
    expect(setDiscoveryConfig).toHaveBeenCalledWith({ ...CONFIG, enabled: false });
    expect(result.messages?.[0]?.content).toContain("disabled");
  });
});
