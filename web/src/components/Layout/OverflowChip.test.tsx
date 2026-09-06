import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { OverflowChip, type HiddenPill } from "./UnifiedTabBar";

function pills(): HiddenPill[] {
  return [
    { key: "chat:s1", emoji: "💬", title: "Chat One", onActivate: vi.fn(), onClose: vi.fn() },
    { key: "chat:s2", emoji: "💬", title: "Chat Two", onActivate: vi.fn(), onClose: vi.fn() },
  ];
}

describe("OverflowChip", () => {
  it("renders the '+N' label on a reserved-width chip button", () => {
    render(<OverflowChip label="2+" pills={pills()} hasActive={false} />);
    expect(screen.getByRole("button", { name: /2\+ more tabs/ })).toBeTruthy();
    expect(screen.getByText("2+")).toBeTruthy();
  });

  it("caps the label at 99+", () => {
    render(<OverflowChip label="99+" pills={[]} hasActive={false} />);
    expect(screen.getByText("99+")).toBeTruthy();
  });

  it("opens a popover listing the hidden tabs in order on click", async () => {
    render(<OverflowChip label="2+" pills={pills()} hasActive={false} />);
    fireEvent.click(screen.getByRole("button", { name: /2\+ more tabs/ }));
    await waitFor(() => expect(screen.getByText("Chat One")).toBeTruthy());
    expect(screen.getByText("Chat Two")).toBeTruthy();
  });

  it("activates a hidden-tab row and closes the popover", async () => {
    const hidden = pills();
    render(<OverflowChip label="2+" pills={hidden} hasActive={false} />);
    fireEvent.click(screen.getByRole("button", { name: /2\+ more tabs/ }));
    await waitFor(() => expect(screen.getByText("Chat Two")).toBeTruthy());
    const activate = screen.getAllByText("Chat One")[0].closest("button")!;
    fireEvent.click(activate);
    expect(hidden[0].onActivate).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByText("Chat One")).toBeNull());
  });

  it("closes a tab via the sibling ✕ without activating it", async () => {
    const hidden = pills();
    render(<OverflowChip label="2+" pills={hidden} hasActive={false} />);
    fireEvent.click(screen.getByRole("button", { name: /2\+ more tabs/ }));
    await waitFor(() => expect(screen.getByText("Chat One")).toBeTruthy());
    const close = screen.getByRole("button", { name: /Close Chat One/ });
    fireEvent.click(close);
    expect(hidden[0].onClose).toHaveBeenCalledTimes(1);
    expect(hidden[0].onActivate).not.toHaveBeenCalled();
  });

  it("moves focus between rows with ArrowDown/ArrowUp", async () => {
    render(<OverflowChip label="2+" pills={pills()} hasActive={false} />);
    fireEvent.click(screen.getByRole("button", { name: /2\+ more tabs/ }));
    await waitFor(() => expect(screen.getByText("Chat One")).toBeTruthy());
    const first = screen.getAllByText("Chat One")[0].closest("button")!;
    first.focus();
    fireEvent.keyDown(first, { key: "ArrowDown" });
    expect(document.activeElement?.textContent).toContain("Chat Two");
    fireEvent.keyDown(document.activeElement!, { key: "ArrowUp" });
    expect(document.activeElement?.textContent).toContain("Chat One");
  });

  it("closes on Escape and restores focus to the trigger", async () => {
    render(<OverflowChip label="2+" pills={pills()} hasActive={false} />);
    const trigger = screen.getByRole("button", { name: /2\+ more tabs/ });
    fireEvent.click(trigger);
    await waitFor(() => expect(screen.getByText("Chat One")).toBeTruthy());
    fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    await waitFor(() => expect(screen.queryByText("Chat One")).toBeNull());
  });

  it("auto-closes when the hidden set empties", async () => {
    const { rerender } = render(<OverflowChip label="1+" pills={pills().slice(0, 1)} hasActive={false} />);
    fireEvent.click(screen.getByRole("button", { name: /1\+ more tabs/ }));
    await waitFor(() => expect(screen.getByText("Chat One")).toBeTruthy());
    rerender(<OverflowChip label="1+" pills={[]} hasActive={false} />);
    await waitFor(() => expect(screen.queryByText("Chat One")).toBeNull());
  });
});
