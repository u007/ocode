import { describe, expect, it } from "vitest";
import type { ChatDisplayOverride } from "@/api/types";
import {
  DEFAULT_CHAT_VERBOSITY_CONFIG,
  chatPolicyRevision,
  normalizeChatVerbosityConfig,
  resolveChatDisplayPolicy,
} from "./chatVerbosity";

describe("chat verbosity policy", () => {
  it("normalizes omitted values to Full + preset overrides", () => {
    expect(normalizeChatVerbosityConfig({ preset: "full" })).toEqual(DEFAULT_CHAT_VERBOSITY_CONFIG);
  });

  it.each([
    ["full", "expanded", "expanded", "expanded", "expanded", "expanded"],
    // Spec §9: balanced collapses older thinking AND tool-call details.
    ["balanced", "collapsed", "collapsed", "expanded", "expanded", "expanded"],
    // Quiet keeps the latest thinking open only until the answer starts.
    ["quiet", "collapsed", "collapsed", "collapsed", "collapsed", "auto"],
  ] as const)(
    "resolves the %s preset matrix",
    (preset, olderThinking, toolCalls, toolOutput, notices, latestThinking) => {
      const policy = resolveChatDisplayPolicy(normalizeChatVerbosityConfig({ preset }));
      expect(policy).toEqual({
        older_thinking: olderThinking,
        latest_thinking: latestThinking,
        tool_calls: toolCalls,
        tool_output: toolOutput,
        notices,
        status: "expanded",
      });
    },
  );

  describe.each(["full", "balanced", "quiet"] as const)("latest thinking override under %s", (preset) => {
    const presetLatest = preset === "quiet" ? "auto" : "expanded";
    it.each([
      ["preset", presetLatest],
      ["expanded", "expanded"],
      ["collapsed", "collapsed"],
      ["auto", "auto"],
    ] as const)("override %s resolves latest thinking to %s", (override, expected) => {
      const policy = resolveChatDisplayPolicy({ preset, overrides: { latest_thinking: override } });
      expect(policy.latest_thinking).toBe(expected);
    });
  });

  it("accepts auto for latest thinking only", () => {
    expect(() => resolveChatDisplayPolicy({ preset: "full", overrides: { latest_thinking: "auto" } })).not.toThrow();

    const auto = "auto" as unknown as ChatDisplayOverride;
    for (const category of ["older_thinking", "tool_calls", "tool_output", "activity_notices"] as const) {
      expect(() => resolveChatDisplayPolicy({ preset: "full", overrides: { [category]: auto } })).toThrow(
        `Invalid chat display override for ${category}: auto`,
      );
    }
  });

  it("defaults plan_panel to 5 and accepts only its closed set", () => {
    expect(normalizeChatVerbosityConfig({ preset: "full" }).overrides.plan_panel).toBe("5");
    for (const value of ["collapsed", "5", "10", "all"] as const) {
      expect(() => resolveChatDisplayPolicy({ preset: "full", overrides: { plan_panel: value } })).not.toThrow();
    }
    for (const value of ["preset", "expanded", "7"]) {
      expect(() =>
        resolveChatDisplayPolicy({ preset: "full", overrides: { plan_panel: value as "all" } }),
      ).toThrow(`Invalid chat display override for plan_panel: ${value}`);
    }
  });

  it("applies independent overrides while preserving latest thinking", () => {
    // The preset cell is collapsed for both, so each override here re-opens a
    // collapsed default and collapses an expanded one — override precedence in
    // both directions, not a no-op restatement of the preset.
    const policy = resolveChatDisplayPolicy(
      normalizeChatVerbosityConfig({
        preset: "balanced",
        overrides: {
          older_thinking: "expanded",
          tool_calls: "expanded",
          tool_output: "collapsed",
          activity_notices: "expanded",
        },
      }),
    );
    expect(policy.older_thinking).toBe("expanded");
    expect(policy.latest_thinking).toBe("expanded");
    expect(policy.tool_calls).toBe("expanded");
    expect(policy.tool_output).toBe("collapsed");
    expect(policy.notices).toBe("expanded");
  });

  it("changes the policy revision only when effective modes change", () => {
    const base = resolveChatDisplayPolicy(DEFAULT_CHAT_VERBOSITY_CONFIG);
    const same = resolveChatDisplayPolicy({
      ...DEFAULT_CHAT_VERBOSITY_CONFIG,
      preset: "full",
    });
    const changed = resolveChatDisplayPolicy({
      ...DEFAULT_CHAT_VERBOSITY_CONFIG,
      preset: "quiet",
    });
    expect(chatPolicyRevision(base)).toBe(chatPolicyRevision(same));
    expect(chatPolicyRevision(base)).not.toBe(chatPolicyRevision(changed));
  });
});
