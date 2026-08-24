import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import PermissionDialog from "./PermissionDialog";
import type { PermissionDecision } from "@/api/types";

function renderDialog(overrides: Partial<Parameters<typeof PermissionDialog>[0]> = {}) {
  const onDecide = vi.fn(async (_id: string, _d: PermissionDecision) => true);
  const props: Parameters<typeof PermissionDialog>[0] = {
    open: true,
    tool: "bash",
    command: "rm -rf build",
    requestId: "call-1",
    onDecide,
    ...overrides,
  };
  const view = render(<PermissionDialog {...props} />);
  return { onDecide, unmount: () => view.unmount() };
}

describe("PermissionDialog", () => {
  it("sends allow and deny decisions", async () => {
    // One dialog per decision: a resolved (ok=true) dialog expects to be
    // unmounted by the parent, so its buttons stay disabled afterwards.
    const first = renderDialog();
    fireEvent.click(screen.getByText("Allow once"));
    await waitFor(() =>
      expect(first.onDecide).toHaveBeenCalledWith("call-1", "allow"),
    );
    first.unmount();

    const deny = renderDialog();
    fireEvent.click(screen.getByText("Deny"));
    await waitFor(() => expect(deny.onDecide).toHaveBeenCalledWith("call-1", "deny"));
  });

  it("hides always-tool for bash asks (TUI parity)", () => {
    renderDialog({ scope: "bash_prefix", prefix: "rm" });
    expect(screen.queryByText("Always allow tool")).toBeNull();
    expect(screen.getByText("Always allow rule")).toBeTruthy();
  });

  it("hides always-rule for git prefixes and shell keywords (TUI parity)", () => {
    const { unmount } = renderDialog({ scope: "bash_prefix", prefix: "git push" });
    expect(screen.queryByText("Always allow rule")).toBeNull();
    unmount();

    renderDialog({ scope: "bash_prefix", prefix: "while" });
    expect(screen.queryByText("Always allow rule")).toBeNull();
  });

  it("offers both always choices for non-bash tools and confirms before sending", async () => {
    const { onDecide } = renderDialog({
      tool: "delete",
      command: "/tmp/outside/x",
      scope: "tool",
    });

    expect(screen.getByText("Always allow rule")).toBeTruthy();
    expect(screen.getByText("Always allow tool")).toBeTruthy();

    // The confirm step guards the click path: first click only shows the
    // description of what will persist.
    fireEvent.click(screen.getByText("Always allow tool"));
    expect(onDecide).not.toHaveBeenCalled();
    expect(screen.getByText(/always allow ALL uses of the/i)).toBeTruthy();

    fireEvent.click(screen.getByText("Confirm"));
    await waitFor(() =>
      expect(onDecide).toHaveBeenCalledWith("call-1", "always_tool"),
    );
  });

  it("describes the out-of-scope path persistence on confirm", async () => {
    renderDialog({
      scope: "bash_prefix",
      prefix: "tail",
      outOfScopePath: "/var/log/system.log",
    });

    fireEvent.click(screen.getByText("Always allow rule"));
    expect(
      screen.getByText(/extra_allowed_paths/),
    ).toBeTruthy();
    expect(screen.getByText(/\/var\/log\/system\.log/)).toBeTruthy();
  });
});
