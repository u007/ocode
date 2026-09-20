import { describe, expect, it, vi } from "vitest";
import { dispatchCommand } from "./commands";

/**
 * Subcommand-parity suite.
 *
 * These commands accepted arguments in the TUI but the web handler used to
 * ignore them entirely — the same silent-drop shape as the reported
 * `/fake-agent` bug, but one layer down. The worst case was `/recap status`,
 * which fell through to the default branch and ran a real recap: an LLM call
 * that spends tokens when the user only asked what the settings were.
 */
function ctx(api: Record<string, unknown> = {}, extra: Record<string, unknown> = {}) {
  return { commandName: "test", args: "", api, ...extra } as never;
}

const RECAP_CFG = {
  recap_model: "anthropic/claude-sonnet-4-5",
  recap_model_enabled: true,
  recap_timeout_seconds: 30,
};

describe("/recap subcommands", () => {
  it("`status` reports settings and NEVER runs a recap", async () => {
    const recapSession = vi.fn(async () => ({ recap: "SHOULD NOT RUN" }));
    const result = await dispatchCommand(
      "/recap status",
      ctx({ getRecapConfig: vi.fn(async () => RECAP_CFG), setRecapConfig: vi.fn(), recapSession }),
    );
    expect(recapSession).not.toHaveBeenCalled();
    const content = result.messages?.[0]?.content ?? "";
    expect(content).toContain("Recap Status");
    expect(content).toContain("anthropic/claude-sonnet-4-5");
    expect(content).toContain("30s");
  });

  it("`enable`/`disable` toggle the gate and preserve the model + timeout", async () => {
    const setRecapConfig = vi.fn(async (m: string, e: boolean, t: number) => ({
      recap_model: m,
      recap_model_enabled: e,
      recap_timeout_seconds: t,
    }));
    const c = ctx({
      getRecapConfig: vi.fn(async () => RECAP_CFG),
      setRecapConfig,
      recapSession: vi.fn(),
    });

    const off = await dispatchCommand("/recap disable", c);
    expect(setRecapConfig).toHaveBeenCalledWith(
      "anthropic/claude-sonnet-4-5",
      false,
      30,
      undefined,
    );
    expect(off.messages?.[0]?.content).toContain("disabled");

    const on = await dispatchCommand("/recap enable", c);
    expect(on.messages?.[0]?.content).toContain("enabled");
  });

  it("no-arg `model` opens the recap picker; `model auto` clears the override", async () => {
    const setRecapConfig = vi.fn(async (m: string, e: boolean, t: number) => ({
      recap_model: m,
      recap_model_enabled: e,
      recap_timeout_seconds: t,
    }));
    const c = ctx({
      getRecapConfig: vi.fn(async () => RECAP_CFG),
      setRecapConfig,
      recapSession: vi.fn(),
    });

    const picker = await dispatchCommand("/recap model", c);
    expect(picker.openModelPicker).toBe(true);
    expect(picker.modelPickerPurpose).toBe("recap");
    expect(setRecapConfig).not.toHaveBeenCalled();

    await dispatchCommand("/recap model auto", c);
    expect(setRecapConfig).toHaveBeenCalledWith("", true, 30, undefined);
  });

  it("top-level `auto` clears the override and NEVER runs a recap", async () => {
    const setRecapConfig = vi.fn(async (m: string, e: boolean, t: number) => ({
      recap_model: m,
      recap_model_enabled: e,
      recap_timeout_seconds: t,
    }));
    const recapSession = vi.fn(async () => ({ recap: "SHOULD NOT RUN" }));
    const c = ctx({
      getRecapConfig: vi.fn(async () => RECAP_CFG),
      setRecapConfig,
      recapSession,
    });

    const result = await dispatchCommand("/recap auto", c);
    expect(recapSession).not.toHaveBeenCalled();
    expect(setRecapConfig).toHaveBeenCalledWith("", true, 30, undefined);
    expect(result.messages?.[0]?.content ?? "").toContain("cleared");
  });

  it("bare /recap still runs the recap (default branch unchanged)", async () => {
    const recapSession = vi.fn(async () => ({ recap: "the summary" }));
    const result = await dispatchCommand(
      "/recap",
      {
        commandName: "/recap",
        args: "",
        api: {
          getRecapConfig: vi.fn(),
          setRecapConfig: vi.fn(),
          recapSession,
        },
        getSessionId: () => "sess-1",
      } as never,
    );
    expect(recapSession).toHaveBeenCalled();
    expect(result.messages?.[0]?.content).toContain("the summary");
  });
});

describe("/agents limit", () => {
  const LIMITS = { max_steps: 40, image_max_dim: 2000, max_concurrent_agents: 4, undo_max_age_delta: 10 };

  it("bare `limit` reports the current ceiling", async () => {
    const result = await dispatchCommand(
      "/agents limit",
      ctx({ getLimitsConfig: vi.fn(async () => LIMITS), setLimitsConfig: vi.fn() }),
    );
    const content = result.messages?.[0]?.content ?? "";
    expect(content).toContain("4");
    expect(content).toContain("limit");
  });

  it("`limit <n>` persists max_concurrent_agents and preserves other fields", async () => {
    const setLimitsConfig = vi.fn(async (f: unknown) => f);
    await dispatchCommand(
      "/agents limit 0",
      ctx({ getLimitsConfig: vi.fn(async () => LIMITS), setLimitsConfig }),
    );
    expect(setLimitsConfig).toHaveBeenCalledWith({ ...LIMITS, max_concurrent_agents: 0 });
  });

  it("rejects a non-numeric limit without writing", async () => {
    const setLimitsConfig = vi.fn();
    const result = await dispatchCommand(
      "/agents limit abc",
      ctx({ getLimitsConfig: vi.fn(async () => LIMITS), setLimitsConfig }),
    );
    expect(setLimitsConfig).not.toHaveBeenCalled();
    expect(result.messages?.[0]?.content).toContain("Invalid number");
  });
});

describe("/cron subcommands", () => {
  it("`describe <id>` prints the job detail", async () => {
    const getCronJob = vi.fn(async (id: string) => ({
      id,
      name: "nightly",
      schedule: { kind: "cron", expr: "0 9 * * *" },
      payload: { message: "hi" },
      state: {},
      created_at_ms: 1,
      enabled: true,
    }));
    const result = await dispatchCommand("/cron describe j1", ctx({ getCronJob }));
    expect(getCronJob).toHaveBeenCalledWith("j1");
    expect(result.messages?.[0]?.content).toContain("nightly");
  });

  it("`describe` without an id shows usage and does not call the API", async () => {
    const getCronJob = vi.fn();
    const result = await dispatchCommand("/cron describe", ctx({ getCronJob }));
    expect(getCronJob).not.toHaveBeenCalled();
    expect(result.messages?.[0]?.content).toContain("Usage");
  });

  it("`remove <id>` deletes; without an id it does not", async () => {
    const deleteCronJob = vi.fn(async () => ({}));
    const withId = await dispatchCommand("/cron remove j1", ctx({ deleteCronJob }));
    expect(deleteCronJob).toHaveBeenCalledWith("j1");
    expect(withId.messages?.[0]?.content).toContain("Removed");

    deleteCronJob.mockClear();
    const noId = await dispatchCommand("/cron remove", ctx({ deleteCronJob }));
    expect(deleteCronJob).not.toHaveBeenCalled();
    expect(noId.messages?.[0]?.content).toContain("Usage");
  });

  it("`add` points at the Cron tab instead of silently listing", async () => {
    const result = await dispatchCommand("/cron add 60000 do a thing", ctx({}));
    expect(result.handled).toBe(true);
    expect(result.messages?.[0]?.content).toContain("Cron");
  });

  it("bare /cron still lists jobs", async () => {
    const getCronJobs = vi.fn(async () => [{ id: "j1", name: "nightly", next_run: "09:00" }]);
    const result = await dispatchCommand("/cron", ctx({ getCronJobs }));
    expect(getCronJobs).toHaveBeenCalled();
    expect(result.messages?.[0]?.content).toContain("nightly");
  });
});

describe("/compact focus", () => {
  it("forwards the focus so the summary can be steered (TUI parity)", async () => {
    const compactSession = vi.fn(async () => ({ original_len: 10, compacted_len: 2 }));
    await dispatchCommand(
      "/compact the auth refactor",
      {
        commandName: "/compact",
        args: "the auth refactor",
        api: { compactSession },
        getSessionId: () => "sess-1",
      } as never,
    );
    expect(compactSession).toHaveBeenCalledWith("sess-1", undefined, "the auth refactor");
  });

  it("bare /compact passes no focus", async () => {
    const compactSession = vi.fn(async () => ({ original_len: 10, compacted_len: 2 }));
    await dispatchCommand(
      "/compact",
      {
        commandName: "/compact",
        args: "",
        api: { compactSession },
        getSessionId: () => "sess-1",
      } as never,
    );
    expect(compactSession).toHaveBeenCalledWith("sess-1", undefined, undefined);
  });
});

describe("/mcp-auth", () => {
  it("returns immediately and reports success via notify after the flow settles", async () => {
    vi.useFakeTimers();
    try {
      const startMCPAuth = vi.fn(async () => ({
        job_id: "job-1",
        server: "linear",
        status: "running",
        browser_note: "A browser window will open.",
      }));
      const getMCPAuthStatus = vi
        .fn()
        .mockResolvedValueOnce({ job_id: "job-1", server: "linear", status: "running" })
        .mockResolvedValueOnce({ job_id: "job-1", server: "linear", status: "done" });
      const notify = vi.fn();

      const result = await dispatchCommand(
        "/mcp-auth linear",
        ctx({ startMCPAuth, getMCPAuthStatus }, { notify }),
      );

      // The handler must NOT await the poll (that blocks the composer): it
      // returns a "started" acknowledgement straight away.
      expect(startMCPAuth).toHaveBeenCalledWith("linear", undefined);
      expect(result.messages?.[0]?.content ?? "").toContain("started");
      expect(notify).not.toHaveBeenCalled();

      // Drive the poll interval(s); the outcome arrives out-of-band.
      await vi.advanceTimersByTimeAsync(2000);
      await vi.advanceTimersByTimeAsync(2000);

      expect(notify).toHaveBeenCalled();
      const calls = notify.mock.calls;
      const reported = calls[calls.length - 1]?.[0] ?? "";
      expect(reported).toContain("successful");
      expect(reported).toContain("linear");
    } finally {
      vi.useRealTimers();
    }
  });

  it("reports the server's refusal verbatim (remote session cannot run it)", async () => {
    const startMCPAuth = vi.fn(async () => {
      throw new Error("MCP OAuth must run where the browser can reach the callback (127.0.0.1:8085).");
    });
    const result = await dispatchCommand("/mcp-auth linear", ctx({ startMCPAuth }));
    const content = result.messages?.[0]?.content ?? "";
    expect(content).toContain("127.0.0.1:8085");
  });

  it("surfaces a job error via notify", async () => {
    vi.useFakeTimers();
    try {
      const startMCPAuth = vi.fn(async () => ({ job_id: "j", server: "linear", status: "running" }));
      const getMCPAuthStatus = vi.fn(async () => ({
        job_id: "j",
        server: "linear",
        status: "error",
        error: "oauth state mismatch",
      }));
      const notify = vi.fn();
      await dispatchCommand(
        "/mcp-auth linear",
        ctx({ startMCPAuth, getMCPAuthStatus }, { notify }),
      );
      await vi.advanceTimersByTimeAsync(2000);
      const calls = notify.mock.calls;
      const reported = calls[calls.length - 1]?.[0] ?? "";
      expect(reported).toContain("oauth state mismatch");
    } finally {
      vi.useRealTimers();
    }
  });

  it("without a status endpoint or notify, reports started without promising an outcome", async () => {
    const startMCPAuth = vi.fn(async () => ({
      job_id: "j",
      server: "linear",
      status: "running",
      browser_note: "A browser window will open.",
    }));
    const result = await dispatchCommand("/mcp-auth linear", ctx({ startMCPAuth }));
    const content = result.messages?.[0]?.content ?? "";
    expect(content).toContain("MCP OAuth started");
    expect(content).toContain("re-run `/mcp`");
  });

  it("without a name shows usage and does not start a flow", async () => {
    const startMCPAuth = vi.fn();
    const result = await dispatchCommand("/mcp-auth", ctx({ startMCPAuth }));
    expect(startMCPAuth).not.toHaveBeenCalled();
    expect(result.messages?.[0]?.content).toContain("Usage");
  });
});
