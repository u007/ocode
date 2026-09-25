import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import SettingsPanel from "./SettingsPanel";

// SettingsPanel renders the default (model-defaults) group on mount; stub it so
// the nav-registration assertions don't drag in the whole chat store.
vi.mock("./ModelDefaultsForm", () => ({ default: () => null }));
vi.mock("./ChatDisplayForm", () => ({
  default: () => <div data-testid="chat-display-form">ChatDisplayForm</div>,
}));

describe("SettingsPanel chat display group", () => {
  it("registers the Chat display group in the nav", () => {
    render(<SettingsPanel />);
    expect(screen.getByRole("button", { name: "Chat display" })).toBeTruthy();
  });

  it("selecting Chat display renders ChatDisplayForm", () => {
    render(<SettingsPanel />);
    fireEvent.click(screen.getByRole("button", { name: "Chat display" }));
    expect(screen.getByTestId("chat-display-form")).toBeTruthy();
  });
});
