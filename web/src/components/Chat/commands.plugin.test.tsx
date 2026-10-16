import { beforeEach, describe, expect, it, vi } from "vitest";
import { dispatchCommand } from "./commands";
import { api } from "../../api/client";

vi.mock("../../api/client", () => ({
  api: { installPlugin: vi.fn() },
}));

const mockInstall = vi.mocked(api.installPlugin);

beforeEach(() => {
  vi.clearAllMocks();
});

function ctx() {
  return { commandName: "", args: "", api: {} } as never;
}

describe("/plugin install routing", () => {
  it("reports a bundled skill install instead of a plugin install", async () => {
    mockInstall.mockResolvedValue({
      skill: "standup",
      report: "installed standup in /h/.claude/skills",
    });
    const result = await dispatchCommand("/plugin install standup", ctx());
    expect(mockInstall).toHaveBeenCalledWith("standup");
    expect(result?.messages?.[0]?.content).toBe(
      "Bundled skill **standup**: installed standup in /h/.claude/skills.",
    );
  });

  it("still reports a git plugin install by name and source", async () => {
    mockInstall.mockResolvedValue({
      name: "mine",
      dir: "/d/mine",
      source: "github.com/u/mine",
    });
    const result = await dispatchCommand("/plugin install github.com/u/mine", ctx());
    expect(result?.messages?.[0]?.content).toBe(
      "Installed plugin **mine** from `github.com/u/mine`.",
    );
  });
});
