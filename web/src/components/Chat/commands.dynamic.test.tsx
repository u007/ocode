import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { COMMANDS, loadDynamicCommands, reloadDynamicCommands, useCommands } from "./commands";
import { api } from "../../api/client";

// The slash list is a discovery surface: when a skill is disabled from the chat
// sidebar, reloadDynamicCommands() must drop it WITHOUT a page reload, keep the
// built-in commands, and make every useCommands consumer re-render with the new
// list.
vi.mock("../../api/client", () => ({
  api: {
    listCommands: vi.fn(() => Promise.resolve([])),
    listSkills: vi.fn(() => Promise.resolve([])),
  },
}));

function commandNames(): string[] {
  return COMMANDS.map((c) => c.name);
}

function Probe() {
  const commands = useCommands();
  return <div data-testid="probe">{commands.map((c) => c.name).join(",")}</div>;
}

beforeEach(() => {
  vi.mocked(api.listCommands).mockResolvedValue([]);
  vi.mocked(api.listSkills).mockResolvedValue([]);
});

describe("reloadDynamicCommands", () => {
  it("drops a skill that disappeared from the server list and keeps built-ins", async () => {
    vi.mocked(api.listCommands).mockResolvedValue([{ name: "mycmd", description: "c" }]);
    vi.mocked(api.listSkills).mockResolvedValue([
      { name: "skill-one", description: "s" },
      { name: "skill-two", description: "s" },
    ]);
    await loadDynamicCommands();

    expect(commandNames()).toContain("/mycmd");
    expect(commandNames()).toContain("/skill-one");
    const builtinCount = COMMANDS.filter((c) => c.name === "/help").length;
    expect(builtinCount).toBe(1);

    // The server now reports skill-one as disabled, so /api/skills omits it.
    vi.mocked(api.listSkills).mockResolvedValue([{ name: "skill-two", description: "s" }]);
    await reloadDynamicCommands();

    expect(commandNames()).not.toContain("/skill-one");
    expect(commandNames()).toContain("/skill-two");
    // Custom commands are still listed (only the skill list changed).
    expect(commandNames()).toContain("/mycmd");
    // Built-ins are never removed by a reload.
    expect(commandNames().filter((n) => n === "/help")).toHaveLength(1);
  });

  it("re-renders mounted useCommands consumers with the new list", async () => {
    vi.mocked(api.listSkills).mockResolvedValue([{ name: "skill-one", description: "s" }]);
    await loadDynamicCommands();
    await reloadDynamicCommands();

    render(<Probe />);
    await waitFor(() => {
      expect(screen.getByTestId("probe").textContent).toContain("/skill-one");
    });

    vi.mocked(api.listSkills).mockResolvedValue([]);
    await reloadDynamicCommands();

    await waitFor(() => {
      expect(screen.getByTestId("probe").textContent).not.toContain("/skill-one");
    });
  });
});
