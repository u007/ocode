import { afterEach, describe, expect, it, vi } from "vitest";
import { dispatchCommand, type CommandContext } from "./commands";
import { ApiError } from "../../api/client";
import { getActionError, resetActionErrors } from "../../lib/actionErrors";
import { clearCompaction, getCompactionState } from "../../lib/compactionState";

describe("/compact failure reporting", () => {
  afterEach(() => {
    resetActionErrors();
    clearCompaction("sess-compact-error");
  });

  it("keeps the inline error and reports a timeout through the app-wide surface", async () => {
    resetActionErrors();
    const compactSession = vi.fn<CommandContext["api"]["compactSession"]>().mockRejectedValue(
      new ApiError("compaction timed out; transcript unchanged", 504),
    );
    const result = await dispatchCommand("/compact", {
      commandName: "compact",
      args: "",
      api: { compactSession } as unknown as CommandContext["api"],
      getSessionId: () => "sess-compact-error",
    });

    expect(result.messages?.[0]?.content).toContain("compaction timed out");
    expect(getCompactionState("sess-compact-error")).toEqual({
      status: "error",
      error: "compaction timed out; transcript unchanged",
    });
    expect(getActionError()?.message).toContain("compaction timed out");
    expect(getActionError()?.message).toContain("HTTP 504");
  });
});
