import { describe, expect, it, vi, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ReaderChatHeader } from "./ReaderChatHeader";

afterEach(cleanup);

describe("ReaderChatHeader", () => {
  it("shows the chat label with the sidebar and exit controls", () => {
    render(<ReaderChatHeader onToggleSidebar={() => {}} onExit={() => {}} />);
    expect(screen.getByText("Chat")).toBeDefined();
    expect(screen.getByRole("button", { name: "Show sessions sidebar" })).toBeDefined();
    expect(screen.getByRole("button", { name: "Exit reader mode" })).toBeDefined();
  });

  it("calls onToggleSidebar from the menu button", () => {
    const onToggleSidebar = vi.fn();
    const onExit = vi.fn();
    render(<ReaderChatHeader onToggleSidebar={onToggleSidebar} onExit={onExit} />);
    fireEvent.click(screen.getByRole("button", { name: "Show sessions sidebar" }));
    expect(onToggleSidebar).toHaveBeenCalledTimes(1);
    expect(onExit).not.toHaveBeenCalled();
  });

  it("calls onExit from the minimize button", () => {
    const onToggleSidebar = vi.fn();
    const onExit = vi.fn();
    render(<ReaderChatHeader onToggleSidebar={onToggleSidebar} onExit={onExit} />);
    fireEvent.click(screen.getByRole("button", { name: "Exit reader mode" }));
    expect(onExit).toHaveBeenCalledTimes(1);
    expect(onToggleSidebar).not.toHaveBeenCalled();
  });
});
