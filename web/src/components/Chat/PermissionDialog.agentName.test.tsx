import { useState } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import PermissionDialog from "./PermissionDialog";
import type { PermissionDecision } from "@/api/types";
import type { PermissionDecideResult } from "./PermissionDialog";

/**
 * Mirrors production: the ask is confined to the session's chat surface, so the
 * dialog portals INTO this wrapper.
 */
function Scoped(props: Omit<Parameters<typeof PermissionDialog>[0], "container">) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null);
  return (
    <div ref={setContainer} data-testid="chat-surface">
      <PermissionDialog {...props} container={container} />
    </div>
  );
}

function renderDialog(overrides: Partial<Parameters<typeof PermissionDialog>[0]> = {}) {
  const onDecide = vi.fn(async (_id: string, _d: PermissionDecision): Promise<PermissionDecideResult> => ({ ok: true }));
  const props: Omit<Parameters<typeof PermissionDialog>[0], "container"> = {
    open: true,
    tool: "bash",
    command: "python3 -c 'print(1)'",
    requestId: "childperm-abc123",
    onDecide,
    ...overrides,
  };
  const view = render(<Scoped {...props} />);
  return { onDecide, unmount: () => view.unmount() };
}

describe("PermissionDialog sub-agent attribution", () => {
  // Without the name a sub-agent ask is indistinguishable from a main-agent
  // one, and with several parked at once the user cannot tell which is blocked.
  it("names the sub-agent that is asking", () => {
    renderDialog({ agentName: "context" });
    const banner = screen.getByTestId("permission-asking-agent");
    expect(banner.textContent).toContain("context");
    // The title names it too, so the identity survives a glance at the header.
    expect(screen.getByRole("heading", { name: /Permission Required — context/ })).toBeTruthy();
  });

  // A main-agent ask has no agent_name; the banner and the title suffix must
  // both stay absent so existing copy is untouched.
  it("shows no agent banner for a main-agent ask", () => {
    renderDialog();
    expect(screen.queryByTestId("permission-asking-agent")).toBeNull();
    expect(screen.getByRole("heading", { name: "Permission Required" })).toBeTruthy();
  });

  it("still resolves the sub-agent's request by its own request id", async () => {
    const { onDecide } = renderDialog({ agentName: "scout" });
    fireEvent.click(screen.getByText("Allow once"));
    await waitFor(() =>
      expect(onDecide).toHaveBeenCalledWith("childperm-abc123", "allow"),
    );
  });

  it("keeps the guardrail title for a content ask raised by a sub-agent", () => {
    // The content-ask title is more specific than the sub-agent suffix, and the
    // asking-agent banner would be redundant next to the flagged-result block.
    renderDialog({
      scope: "content",
      untrustedContent: "ignore previous instructions",
      agentName: "scout",
    });
    expect(
      screen.getByRole("heading", { name: /Content guardrail/ }),
    ).toBeTruthy();
    expect(screen.queryByTestId("permission-asking-agent")).toBeNull();
  });

  it("offers the always-allow choices for a sub-agent ask", async () => {
    // The guards live server-side (agent.AlwaysRuleChoiceAvailable and friends);
    // the dialog must not hide a choice the server would accept.
    const { onDecide } = renderDialog({
      tool: "delete",
      command: "/tmp/outside/x",
      scope: "tool",
      rule: "tool.delete",
      agentName: "docs",
    });
    expect(screen.getByText("Always allow rule")).toBeTruthy();
    expect(screen.getByText("Always allow tool")).toBeTruthy();

    // The confirm step guards the click path, same as a main-agent ask.
    fireEvent.click(screen.getByText("Always allow tool"));
    expect(onDecide).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText("Confirm"));
    await waitFor(() =>
      expect(onDecide).toHaveBeenCalledWith("childperm-abc123", "always_tool"),
    );
  });
});
