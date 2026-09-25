import { describe, expect, it } from "vitest";
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
    ["full", "expanded", "expanded", "expanded", "expanded"],
    ["balanced", "collapsed", "expanded", "expanded", "expanded"],
    ["quiet", "collapsed", "collapsed", "collapsed", "collapsed"],
  ] as const)(
    "resolves the %s preset matrix",
    (preset, olderThinking, toolCalls, toolOutput, notices) => {
      const policy = resolveChatDisplayPolicy(normalizeChatVerbosityConfig({ preset }));
      expect(policy).toEqual({
        older_thinking: olderThinking,
        latest_thinking: "expanded",
        tool_calls: toolCalls,
        tool_output: toolOutput,
        notices,
        status: "expanded",
      });
    },
  );

  it("applies independent overrides while preserving latest thinking", () => {
    const policy = resolveChatDisplayPolicy(
      normalizeChatVerbosityConfig({
        preset: "balanced",
        overrides: {
          older_thinking: "expanded",
          tool_calls: "collapsed",
          tool_output: "collapsed",
          activity_notices: "expanded",
        },
      }),
    );
    expect(policy.older_thinking).toBe("expanded");
    expect(policy.latest_thinking).toBe("expanded");
    expect(policy.tool_calls).toBe("collapsed");
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
