import { describe, expect, it, vi } from "vitest";
import { dispatchCommand } from "./commands";

function context(overrides: {
  getComputerUseConfig?: () => Promise<{ enabled: boolean; status_lines: string[] }>;
  setComputerUseConfig?: (enabled: boolean) => Promise<{ enabled: boolean; status_lines: string[] }>;
} = {}) {
  return {
    commandName: "computer",
    args: "",
    api: {
      getComputerUseConfig: overrides.getComputerUseConfig ?? (async () => ({
        enabled: false,
        status_lines: ["Computer use: disabled", "Backend: test"],
      })),
      setComputerUseConfig: overrides.setComputerUseConfig ?? (async (enabled: boolean) => ({
        enabled,
        status_lines: [`Computer use: ${enabled ? "enabled" : "disabled"}`],
      })),
    },
  } as never;
}

describe("/computer command", () => {
  it("reports the server-provided status lines", async () => {
    const result = await dispatchCommand("/computer status", context());
    expect(result.handled).toBe(true);
    expect(result.messages?.[0]?.content).toContain("Computer use: disabled");
    expect(result.messages?.[0]?.content).toContain("Backend: test");
  });

  it("persists enable and reports that it applies to new sessions", async () => {
    const setComputerUseConfig = vi.fn(async (enabled: boolean) => ({
      enabled,
      status_lines: ["Computer use: enabled"],
    }));
    const result = await dispatchCommand("/computer enable", context({ setComputerUseConfig }));
    expect(setComputerUseConfig).toHaveBeenCalledWith(true);
    expect(result.messages?.[0]?.content).toContain("Computer use: enabled. Takes effect in new sessions.");
  });

  it("persists disable", async () => {
    const setComputerUseConfig = vi.fn(async (enabled: boolean) => ({
      enabled,
      status_lines: ["Computer use: disabled"],
    }));
    const result = await dispatchCommand("/computer disable", context({ setComputerUseConfig }));
    expect(setComputerUseConfig).toHaveBeenCalledWith(false);
    expect(result.messages?.[0]?.content).toContain("Computer use: disabled. Takes effect in new sessions.");
  });

  it("shows usage for an unknown subcommand", async () => {
    const result = await dispatchCommand("/computer nope", context());
    expect(result.messages?.[0]?.content).toBe("Usage: `/computer [status|enable|disable]`");
  });
});
