import { afterEach, describe, expect, it, vi } from "vitest";
import { api, type CompactConfig } from "../api/client";
import { EMPTY_COMPACT_CONFIG, compactSummaryDisplay, setCompactConfig, summaryModelPatch } from "./compactConfig";

const BASE: CompactConfig = {
  enabled: true,
  summary_provider: "",
  summary_model: "anthropic/claude-haiku-4-5",
  token_threshold: 0.85,
  keep_recent_turns: 3,
  keep_recent_tokens: 20000,
  min_messages: 8,
  summary_timeout_seconds: 300,
  summary_first_token_timeout_seconds: 300,
  summary_max_retries: 2,
  max_summary_input_tokens: 50000,
};

describe("summaryModelPatch", () => {
  it("stores the picker's canonical provider/model id verbatim", () => {
    // Not split into provider/model halves: the id already names its provider
    // and the server parses "provider/model" itself.
    expect(summaryModelPatch("anthropic/claude-haiku-4-5").summary_model).toBe("anthropic/claude-haiku-4-5");
    expect(summaryModelPatch("openai/gpt-4o-mini").summary_model).toBe("openai/gpt-4o-mini");
  });

  it("clears summary_provider so the stored model resolves on its own provider", () => {
    // The server glues the MAIN model's provider onto a bare summary_model
    // (internal/agent/agent.go compactSummaryClient). A stale summary_provider
    // left over from a hand-edited config would instead win and send the
    // summary to the wrong backend, so picking a model must clear it.
    expect(summaryModelPatch("openai/gpt-4o-mini")).toEqual({
      summary_model: "openai/gpt-4o-mini",
      summary_provider: "",
    });
    expect(summaryModelPatch("")).toEqual({ summary_model: "", summary_provider: "" });
  });

  it("never writes an undefined provider, which JSON.stringify would drop silently", () => {
    const patch = summaryModelPatch("openai/gpt-4o-mini");
    expect("summary_provider" in patch).toBe(true);
    expect(patch.summary_provider).not.toBeUndefined();
  });
});

describe("compactSummaryDisplay", () => {
  it("shows the configured model", () => {
    expect(compactSummaryDisplay(BASE)).toBe("anthropic/claude-haiku-4-5");
  });

  it("explains the auto fallback when no model is set", () => {
    expect(compactSummaryDisplay({ ...BASE, summary_model: "" })).toBe("(auto: small model, then main)");
  });

  it("treats a whitespace-only model as unset", () => {
    expect(compactSummaryDisplay({ ...BASE, summary_model: "   " })).toBe("(auto: small model, then main)");
  });
});

// Spies are restored between tests so each one starts from the real `api`
// methods and its own empty call log.
afterEach(() => {
  vi.restoreAllMocks();
});

describe("setCompactConfig", () => {
  it("sends ONLY the changed fields — the server merges them onto what is on disk", async () => {
    const read = vi.spyOn(api, "getCompactConfig");
    const set = vi.spyOn(api, "setCompactConfig").mockImplementation(async (cfg) => ({ ...BASE, ...cfg }));

    await setCompactConfig({ enabled: false });

    // No GET first, and no echo of the whole block: a read-modify-write here
    // would reintroduce exactly the race the server-side merge removes.
    expect(read).not.toHaveBeenCalled();
    expect(set).toHaveBeenCalledExactlyOnceWith({ enabled: false }, undefined);
  });

  it("threads the host into the write", async () => {
    const set = vi.spyOn(api, "setCompactConfig").mockImplementation(async (cfg) => ({ ...BASE, ...cfg }));

    await setCompactConfig({ summary_model: "openai/gpt-4o-mini" }, "user@remote");

    expect(set).toHaveBeenCalledExactlyOnceWith({ summary_model: "openai/gpt-4o-mini" }, "user@remote");
  });

  it("returns the server's merged block, not the local guess", async () => {
    vi.spyOn(api, "setCompactConfig").mockResolvedValue({ ...BASE, enabled: true, summary_max_retries: 7 });

    const saved = await setCompactConfig({ enabled: false });

    // Another writer's field arrives in the response, which is why callers
    // refresh from it rather than from the patch.
    expect(saved.summary_max_retries).toBe(7);
  });
});

describe("EMPTY_COMPACT_CONFIG", () => {
  it("has every field present so a form save never drops one", () => {
    // Settings → Compact owns the whole block and sends it complete, so the
    // server-side merge behaves as a replace. A missing key would instead be
    // left at whatever is already stored, which is a silent no-op for that
    // field rather than a reset.
    expect(Object.keys(EMPTY_COMPACT_CONFIG).sort()).toEqual(Object.keys(BASE).sort());
  });
});
