import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import ReasoningLevelSelector from "./ReasoningLevelSelector";

const hoisted = vi.hoisted(() => ({
  api: {
    setThinkingBudget: vi.fn(async () => ({ budget: 0, level: "off", levels: [] })),
    setSessionThinkingBudget: vi.fn(async () => ({ budget: 0, level: "off", session_id: "" })),
  },
}));

vi.mock("../../api/client", () => ({ api: hoisted.api }));

describe("ReasoningLevelSelector scoping (per-chat-session reasoning level)", () => {
  beforeEach(() => {
    hoisted.api.setThinkingBudget.mockClear();
    hoisted.api.setSessionThinkingBudget.mockClear();
  });

  it("scopes the pick to the session and leaves the global default alone", async () => {
    render(<ReasoningLevelSelector thinkingBudget={0} sessionId="ses_123" host="box" />);
    fireEvent.click(screen.getByRole("button", { name: /Reason:/ }));
    fireEvent.click(screen.getByRole("button", { name: /^MAX$/ }));

    await waitFor(() =>
      expect(hoisted.api.setSessionThinkingBudget).toHaveBeenCalledWith("ses_123", "max", "box"),
    );
    expect(hoisted.api.setThinkingBudget).not.toHaveBeenCalled();
  });

  it("falls back to the global config on a draft tab (no session server-side yet)", async () => {
    render(<ReasoningLevelSelector thinkingBudget={0} sessionId="new-1700000000" />);
    fireEvent.click(screen.getByRole("button", { name: /Reason:/ }));
    fireEvent.click(screen.getByRole("button", { name: /^HIGH$/ }));

    await waitFor(() => expect(hoisted.api.setThinkingBudget).toHaveBeenCalledWith("high", undefined));
    expect(hoisted.api.setSessionThinkingBudget).not.toHaveBeenCalled();
  });

  it("falls back to the global config without any session context", async () => {
    render(<ReasoningLevelSelector thinkingBudget={0} />);
    fireEvent.click(screen.getByRole("button", { name: /Reason:/ }));
    fireEvent.click(screen.getByRole("button", { name: /^LOW$/ }));

    await waitFor(() => expect(hoisted.api.setThinkingBudget).toHaveBeenCalledWith("low", undefined));
    expect(hoisted.api.setSessionThinkingBudget).not.toHaveBeenCalled();
  });

  it("supports keyboard navigation, Escape focus restoration, and Tab close", async () => {
    render(<ReasoningLevelSelector thinkingBudget={0} sessionId="ses_123" host="box" />);
    const trigger = screen.getByRole("button", { name: /Reason:/ });
    fireEvent.click(trigger);

    const off = screen.getByRole("button", { name: /^OFF/ });
    await waitFor(() => expect(document.activeElement).toBe(off));
    fireEvent.keyDown(off, { key: "ArrowDown" });
    const low = screen.getByRole("button", { name: /^LOW/ });
    expect(document.activeElement).toBe(low);
    fireEvent.keyDown(low, { key: "Enter" });
    await waitFor(() =>
      expect(hoisted.api.setSessionThinkingBudget).toHaveBeenCalledWith("ses_123", "low", "box"),
    );
    expect(document.activeElement).toBe(trigger);

    fireEvent.click(trigger);
    const reopened = screen.getByRole("button", { name: /Reason:/ });
    fireEvent.keyDown(screen.getByRole("button", { name: /^LOW/ }), { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("button", { name: /^LOW/ })).toBeNull());
    expect(document.activeElement).toBe(reopened);

    fireEvent.click(reopened);
    fireEvent.keyDown(screen.getByRole("button", { name: /^LOW/ }), { key: "Tab" });
    await waitFor(() => expect(screen.queryByRole("button", { name: /^LOW/ })).toBeNull());
  });
});
