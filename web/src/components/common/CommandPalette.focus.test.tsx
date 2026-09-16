import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeAll } from "vitest";

// cmdk's List uses ResizeObserver; jsdom does not ship one.
beforeAll(() => {
  if (!(globalThis as any).ResizeObserver) {
    (globalThis as any).ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});
import CommandPalette from "./CommandPalette";

vi.mock("../Chat/commands", async () => {
  const actual = await vi.importActual<typeof import("../Chat/commands")>("../Chat/commands");
  return {
    ...actual,
    useCommands: vi.fn(() => [
      { name: "/new", description: "New session", run: () => {} },
    ]),
  };
});

// Regression: the command palette (ctrl/cmd+k) must land focus on its search
// input on open.
describe("CommandPalette initial focus", () => {
  it("opens with focus on the command input", async () => {
    render(
      <CommandPalette open onClose={() => {}} onExecute={() => {}} />,
    );
    const input = await screen.findByPlaceholderText("Type a command...");
    await waitFor(() => expect(input).toHaveFocus());
  });
});
