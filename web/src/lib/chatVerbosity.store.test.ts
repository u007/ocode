import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", () => ({
  api: {
    getChatVerbosityConfig: vi.fn(),
    setChatVerbosityConfig: vi.fn(),
  },
}));
vi.mock("@/lib/eventBus", () => ({
  eventBus: {
    on: vi.fn(() => () => {}),
    onReconnect: vi.fn(() => () => {}),
  },
}));

import { api } from "@/api/client";
import {
  __resetChatVerbosityForTests,
  refreshChatVerbosity,
} from "@/lib/chatVerbosity";

const getConfig = vi.mocked(api.getChatVerbosityConfig);

const fullConfig = {
  preset: "full" as const,
  overrides: {
    older_thinking: "preset" as const,
    tool_calls: "preset" as const,
    tool_output: "preset" as const,
    activity_notices: "preset" as const,
  },
};

beforeEach(() => {
  vi.clearAllMocks();
  __resetChatVerbosityForTests();
  getConfig.mockResolvedValue(fullConfig);
  vi.spyOn(console, "warn").mockImplementation(() => {});
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("chat verbosity store", () => {
  it("rejects a malformed server override category instead of silently normalizing it", async () => {
    getConfig.mockResolvedValue({
      preset: "full",
      overrides: { thinking: "preset" },
    } as never);

    const state = await refreshChatVerbosity();
    expect(state.config).toEqual(fullConfig);
    expect(state.error).toContain("Unknown chat display override category: thinking");
    expect(console.warn).toHaveBeenCalledWith(
      "[chat-verbosity] config request failed; using Full compatibility default",
      expect.objectContaining({ reason: "no-cached-policy" }),
    );
  });

  it("retains the last valid policy on a transient refresh failure", async () => {
    await refreshChatVerbosity();
    getConfig.mockRejectedValue(new Error("temporary outage"));
    const state = await refreshChatVerbosity();
    expect(state.config).toEqual(fullConfig);
    expect(state.error).toBe("temporary outage");
    expect(console.warn).toHaveBeenCalledWith(
      "[chat-verbosity] config request failed; retaining cached policy",
      expect.objectContaining({ error: expect.any(Error) }),
    );
  });
});
