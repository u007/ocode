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
    expect(mockExportMarkdown).toHaveBeenCalledWith(REAL_ID);
    expect(result.messages?.[0]?.content).toBe("Exported session as Markdown.");
    expect(result.download?.filename).toBe(`ocode_export_${REAL_ID}.md`);
    expect(result.download?.content).toContain("hello");
  });

  it("/export-claude appends a non-empty session", async () => {
    mockExportClaude.mockResolvedValue({ path: "/home/u/.claude/history.jsonl" });
    const result = await dispatchCommand("/export-claude", ctx(REAL_ID));
    expect(mockExportClaude).toHaveBeenCalledWith(REAL_ID);
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
