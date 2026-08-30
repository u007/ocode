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

  it("re-enables buttons after a failed onDecide so the user can retry", async () => {
    const onDecide = vi.fn(async () => false);
    const { unmount } = render(
      <PermissionDialog open={true} tool="bash" command="rm -rf build" requestId="call-1" onDecide={onDecide} />,
    );
    const allowBtn = screen.getByText("Allow once") as HTMLButtonElement;
    fireEvent.click(allowBtn);
    await waitFor(() => expect(onDecide).toHaveBeenCalledWith("call-1", "allow"));
    // After failure, loading is cleared — button must be enabled again for retry.
    await waitFor(() => expect(allowBtn.disabled).toBe(false));
    expect(screen.getByText("Deny").closest("button")?.disabled).toBe(false);
    unmount();
  });

  it("keeps buttons disabled after a successful decision until unmount (single permission)", async () => {
    const onDecide = vi.fn(async () => true);
    const { unmount } = render(
      <PermissionDialog open={true} tool="bash" command="rm -rf build" requestId="call-1" onDecide={onDecide} />,
    );
    const allowBtn = screen.getByText("Allow once") as HTMLButtonElement;
    fireEvent.click(allowBtn);
    await waitFor(() => expect(onDecide).toHaveBeenCalled());
    // Success without a new request keeps the dialog disabled — parent unmounts it.
    expect(allowBtn.disabled).toBe(true);
    unmount();
  });

  it("resets loading when a new requestId arrives while the dialog stays mounted (queued permission)", async () => {
    const onDecide = vi.fn(async () => true);
    const { rerender, unmount } = render(
      <PermissionDialog open={true} tool="bash" command="rm -rf build" requestId="call-1" onDecide={onDecide} />,
    );
    fireEvent.click(screen.getByText("Allow once"));
    await waitFor(() => expect(onDecide).toHaveBeenCalledWith("call-1", "allow"));
    // Simulate the queue resurfacing the next ask: same mounted Dialog, new requestId.
    rerender(
      <PermissionDialog open={true} tool="bash" command="ls /tmp" requestId="call-2" onDecide={onDecide} />,
    );
    // New request must not inherit the previous loading/confirming state.
    const newAllowBtn = screen.getByText("Allow once") as HTMLButtonElement;
    expect(newAllowBtn.disabled).toBe(false);
    expect(screen.getByText("Deny").closest("button")?.disabled).toBe(false);
    // Second decision must be sent with the new requestId, not the stale one.
    fireEvent.click(newAllowBtn);
    await waitFor(() => expect(onDecide).toHaveBeenCalledWith("call-2", "allow"));
    unmount();
  });

  it("clears the always-allow confirm UI when requestId changes", async () => {
    const onDecide = vi.fn(async () => true);
    const { rerender, unmount } = render(
      <PermissionDialog open={true} tool="bash" command="rm -rf build" requestId="call-1" onDecide={onDecide} />,
    );
    fireEvent.click(screen.getByText("Always allow rule"));
    expect(screen.getByText("Confirm")).toBeTruthy();
    // Queue promotes the next permission — confirm state must clear.
    rerender(
      <PermissionDialog open={true} tool="bash" command="ls /tmp" requestId="call-2" onDecide={onDecide} />,
    );
    expect(screen.queryByText("Confirm")).toBeNull();
    expect(screen.getByText("Allow once")).toBeTruthy();
    unmount();
  });

  it("dismissing the dialog (close button / Escape / overlay) submits deny when not loading", async () => {
    const onDecide = vi.fn(async () => true);
    const { unmount } = render(
      <PermissionDialog open={true} tool="bash" command="rm -rf build" requestId="call-1" onDecide={onDecide} />,
    );
    // Radix DialogContent includes an accessible Close button (X)
    const closeBtn = document.querySelector('button[aria-label="Close"]') as HTMLButtonElement | null;
    // Fallback to the sr-only Close text if aria-label not present in this version
    const target = closeBtn ?? screen.getByText("Close").closest("button")!;
    fireEvent.click(target);
    await waitFor(() => expect(onDecide).toHaveBeenCalledWith("call-1", "deny"));
    unmount();
  });

  it("does not submit a dismissal while a decision is in-flight (loading guard)", async () => {
    // onDecide that never resolves keeps the dialog in loading=true
    const onDecide = vi.fn(() => new Promise<boolean>(() => {}));
    const { unmount } = render(
      <PermissionDialog open={true} tool="bash" command="rm -rf build" requestId="call-1" onDecide={onDecide} />,
    );
    fireEvent.click(screen.getByText("Allow once"));
    await waitFor(() => expect(onDecide).toHaveBeenCalledWith("call-1", "allow"));
    onDecide.mockClear();
    const closeBtn = document.querySelector('button[aria-label="Close"]') as HTMLButtonElement | null;
    const target = closeBtn ?? screen.getByText("Close").closest("button")!;
    fireEvent.click(target);
    // Dismissal is suppressed while loading — no additional deny should be sent
    expect(onDecide).not.toHaveBeenCalled();
    unmount();
  });

  it("renders a wide dialog with wrapped, bounded content for long unbreakable text", () => {
    const longToken =
      "curl -X POST https://example.com/a/very/long/path/that/never/breaks/across-spaces/segment-9f8e7d6c5b4a3210deadbeef -d " +
      "x".repeat(240);
    const { unmount } = renderDialog({
      command: longToken,
      rule: "bash.prefix.a/very/long/unbreakable/rule/token/".repeat(6),
      summary: "summary with a long token " + "y".repeat(200),
      denyReason: "denied because " + "z".repeat(200),
    });

    const content = screen
      .getByText("Permission Required")
      .closest('[role="dialog"]') as HTMLElement | null;
    expect(content).toBeTruthy();
    // Width expanded beyond the old narrow max-w-md, with a viewport gutter so
    // the dialog can never paint past the window edge.
    expect(content!.className).toContain("sm:max-w-2xl");
    expect(content!.className).not.toContain("max-w-md");
    expect(content!.className).toContain("w-[calc(100%-2rem)]");
    // Tall content stays bounded vertically inside the dialog, and horizontal
    // overflow from any descendant is explicitly clipped (no sideways scroll
    // past the dialog edge).
    expect(content!.className).toContain("max-h-[calc(100vh-2rem)]");
    expect(content!.className).toContain("overflow-y-auto");
    expect(content!.className).toContain("overflow-x-clip");

    // The command block wraps instead of scrolling sideways.
    const commandBlock = screen.getByText(longToken) as HTMLElement;
    expect(commandBlock.className).toContain("whitespace-pre-wrap");
    expect(commandBlock.className).toContain("break-words");
    expect(commandBlock.className).toContain("[overflow-wrap:anywhere]");
    expect(commandBlock.className).toContain("max-h-32");
    expect(commandBlock.className).not.toContain("overflow-x-auto");

    // Dynamic text regions carry wrapping classes so long unbreakable tokens
    // cannot push content past the dialog bounds.
    const ruleSpan = screen.getByText(/bash\.prefix\./) as HTMLElement;
    expect(ruleSpan.className).toContain("[overflow-wrap:anywhere]");
    const summaryBlock = screen.getByText(/summary with a long token/) as HTMLElement;
    expect(summaryBlock.className).toContain("[overflow-wrap:anywhere]");
    const denyBlock = screen.getByText(/denied because/) as HTMLElement;
    expect(denyBlock.className).toContain("[overflow-wrap:anywhere]");

    unmount();
  });

  it("keeps the confirmation explanation bounded and wrapped for long dynamic tokens", () => {
    const longPrefix = "very_long_command_name_with_no_spaces_".repeat(6);
    const first = renderDialog({ scope: "bash_prefix", prefix: longPrefix });

    // Always-rule confirmation step echoes the (unbreakable) bash prefix.
    fireEvent.click(screen.getByText("Always allow rule"));
    expect(screen.getByText("Confirm")).toBeTruthy();
    const prefixExplanation = screen.getByText(/Persist a bash-prefix rule/) as HTMLElement;
    expect(prefixExplanation.className).toContain("max-w-full");
    expect(prefixExplanation.className).toContain("break-words");
    expect(prefixExplanation.className).toContain("[overflow-wrap:anywhere]");
    expect(prefixExplanation.textContent).toContain(longPrefix);
    first.unmount();

    const longTool = "extremely_long_tool_name_without_spaces_".repeat(6);
    const second = renderDialog({ tool: longTool, scope: "tool" });

    // Always-tool confirmation step echoes the tool name.
    fireEvent.click(screen.getByText("Always allow tool"));
    const toolExplanation = screen.getByText(/always allow ALL uses of the/) as HTMLElement;
    expect(toolExplanation.className).toContain("max-w-full");
    expect(toolExplanation.className).toContain("break-words");
    expect(toolExplanation.className).toContain("[overflow-wrap:anywhere]");
    expect(toolExplanation.textContent).toContain(longTool);
    second.unmount();
  });
});
