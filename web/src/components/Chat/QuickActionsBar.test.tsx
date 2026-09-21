import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { Archive, Play } from "lucide-react";
import QuickActionsBar, { type QuickActionItem } from "./QuickActionsBar";

const actions: QuickActionItem[] = [
  { id: "compact", label: "Compact", icon: Archive, title: "Compact conversation context (/compact)" },
  { id: "continue", label: "Continue", icon: Play, title: "Send 'continue' to keep the agent going" },
  { id: "recap", label: "Recap", icon: Archive, title: "Generate session recap (/recap)", disabled: true },
];

describe("QuickActionsBar", () => {
  it("renders a labelled toolbar of action pills", () => {
    render(<QuickActionsBar actions={actions} onSelect={vi.fn()} />);
    expect(screen.getByRole("toolbar", { name: "Quick actions" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Compact conversation context (/compact)" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Send 'continue' to keep the agent going" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Generate session recap (/recap)" })).toBeDisabled();
  });

  it("reports the selected action id and ignores disabled entries", () => {
    const onSelect = vi.fn();
    render(<QuickActionsBar actions={actions} onSelect={onSelect} />);
    fireEvent.click(screen.getByRole("button", { name: "Send 'continue' to keep the agent going" }));
    expect(onSelect).toHaveBeenCalledExactlyOnceWith("continue");
    fireEvent.click(screen.getByRole("button", { name: "Generate session recap (/recap)" }));
    expect(onSelect).toHaveBeenCalledTimes(1);
  });
});
