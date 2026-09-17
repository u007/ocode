import { describe, expect, it, vi, beforeEach } from "vitest";
import { dispatchCommand } from "./commands";
import { api } from "../../api/client";

// /export and /export-claude call the module-level `api` (not ctx.api), so the
// client is mocked and the handlers under test reach these spies.
vi.mock("../../api/client", () => ({
  api: {
    exportSessionMarkdown: vi.fn(),
    exportClaudeSession: vi.fn(),
  },
}));

const mockExportMarkdown = vi.mocked(api.exportSessionMarkdown);
const mockExportClaude = vi.mocked(api.exportClaudeSession);

const REAL_ID = "ses_2026-01-02-030405-abcd";

function ctx(sessionId: string) {
  return { commandName: "export", args: "", getSessionId: () => sessionId } as never;
}

describe("/export and /export-claude", () => {
  beforeEach(() => {
    mockExportMarkdown.mockReset();
    mockExportClaude.mockReset();
  });

  it("/export downloads markdown for a non-empty session", async () => {
    mockExportMarkdown.mockResolvedValue("# Hi\n\n## User\n\nhello\n\n");
    const result = await dispatchCommand("/export", ctx(REAL_ID));
    expect(mockExportMarkdown).toHaveBeenCalledWith(REAL_ID, undefined);
    expect(result.messages?.[0]?.content).toBe("Exported session as Markdown.");
    expect(result.download?.filename).toBe(`ocode_export_${REAL_ID}.md`);
    expect(result.download?.content).toContain("hello");
  });

  it("/export-claude appends a non-empty session", async () => {
    mockExportClaude.mockResolvedValue({ path: "/home/u/.claude/history.jsonl" });
    const result = await dispatchCommand("/export-claude", ctx(REAL_ID));
    expect(mockExportClaude).toHaveBeenCalledWith(REAL_ID, undefined);
    expect(result.messages?.[0]?.content).toContain("/home/u/.claude/history.jsonl");
  });

  it("/export on a new-* tab reports an empty session without calling the API", async () => {
    const result = await dispatchCommand("/export", ctx("new-1758000000000"));
    expect(result.messages?.[0]?.content).toContain("the session is empty");
    expect(mockExportMarkdown).not.toHaveBeenCalled();
  });

  it("/export-claude on a new-* tab reports an empty session without calling the API", async () => {
    const result = await dispatchCommand("/export-claude", ctx("new-1758000000000"));
    expect(result.messages?.[0]?.content).toContain("the session is empty");
    expect(mockExportClaude).not.toHaveBeenCalled();
  });

  it("/export surfaces the server's empty-session error for a real session", async () => {
    mockExportMarkdown.mockRejectedValue(new Error("session is empty"));
    const result = await dispatchCommand("/export", ctx(REAL_ID));
    expect(result.messages?.[0]?.content).toBe("**Export failed:** session is empty");
  });

  it("/export-claude surfaces the server's empty-session error for a real session", async () => {
    mockExportClaude.mockRejectedValue(new Error("session is empty"));
    const result = await dispatchCommand("/export-claude", ctx(REAL_ID));
    expect(result.messages?.[0]?.content).toBe("**Claude export failed:** session is empty");
  });

  it("/export reports a real session that is gone", async () => {
    mockExportMarkdown.mockRejectedValue(new Error("session not found"));
    const result = await dispatchCommand("/export", ctx("ses_2026-01-02-030405-gone"));
    expect(result.messages?.[0]?.content).toBe("**Export failed:** session not found");
  });
});

describe("ctx.host threading", () => {
  const HOST = "devbox";

  it("/export passes ctx.host to api.exportSessionMarkdown", async () => {
    mockExportMarkdown.mockResolvedValue("# Exported\n");
    const result = await dispatchCommand("/export", {
      commandName: "export",
      args: "",
      getSessionId: () => REAL_ID,
      host: HOST,
    } as never);
    expect(mockExportMarkdown).toHaveBeenCalledWith(REAL_ID, HOST);
    expect(result.download?.content).toBe("# Exported\n");
  });

  it("/title passes ctx.host to api.setSessionTitle", async () => {
    const mockSetTitle = vi.fn().mockResolvedValue(undefined);
    // /title uses the module-level api, so mock it on the module
    const mod = await import("../../api/client");
    vi.mocked(mod.api).setSessionTitle = mockSetTitle as never;
    const result = await dispatchCommand("/title New Title", {
      commandName: "title",
      args: "New Title",
      getSessionId: () => REAL_ID,
      host: HOST,
    } as never);
    expect(mockSetTitle).toHaveBeenCalledWith(REAL_ID, "New Title", HOST);
    expect(result.messages?.[0]?.content).toContain("Session title set");
  });

  it("/compact passes ctx.host to ctx.api.compactSession", async () => {
    const mockCompact = vi.fn().mockResolvedValue({ original_len: 100, compacted_len: 50 });
    const result = await dispatchCommand("/compact", {
      commandName: "compact",
      args: "",
      getSessionId: () => REAL_ID,
      host: HOST,
      api: { compactSession: mockCompact } as never,
    } as never);
    expect(mockCompact).toHaveBeenCalledWith(REAL_ID, HOST);
    expect(result.messages?.[0]?.content).toContain("100 → 50");
  });

  it("/recap passes ctx.host to ctx.api.recapSession", async () => {
    const mockRecap = vi.fn().mockResolvedValue({ recap: "Summary here" });
    const result = await dispatchCommand("/recap", {
      commandName: "recap",
      args: "",
      getSessionId: () => REAL_ID,
      host: HOST,
      api: { recapSession: mockRecap } as never,
    } as never);
    expect(mockRecap).toHaveBeenCalledWith(REAL_ID, HOST);
    expect(result.messages?.[0]?.content).toContain("Summary here");
  });

  it("/share passes ctx.host to ctx.api.shareSession", async () => {
    const mockShare = vi.fn().mockResolvedValue({ markdown: "# Shared\n" });
    const result = await dispatchCommand("/share", {
      commandName: "share",
      args: "",
      getSessionId: () => REAL_ID,
      host: HOST,
      api: { shareSession: mockShare } as never,
    } as never);
    expect(mockShare).toHaveBeenCalledWith(REAL_ID, HOST);
    expect(result.messages?.[0]?.content).toBe("# Shared\n");
  });

  it("/btw passes ctx.host to ctx.api.btwSession", async () => {
    const mockBtw = vi.fn().mockResolvedValue({ status: "ok" });
    const result = await dispatchCommand("/btw quick note", {
      commandName: "btw",
      args: "quick note",
      getSessionId: () => REAL_ID,
      host: HOST,
      api: { btwSession: mockBtw } as never,
    } as never);
    expect(mockBtw).toHaveBeenCalledWith(REAL_ID, "quick note", HOST);
    expect(result.messages?.[0]?.content).toContain("quick note");
  });
});
