import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import SettingsPanel from "./SettingsPanel";

// SettingsPanel renders a different default group on mount; stub it so these
// nav-registration assertions don't drag in the whole chat store.
vi.mock("./ModelDefaultsForm", () => ({ default: () => null }));

// The real form fetches on mount; stub it so this suite asserts only the nav
// wiring. AutoShareForm's own behaviour is covered in AutoShareForm.test.tsx.
vi.mock("./AutoShareForm", () => ({
  default: () => <div data-testid="auto-share-form">AutoShareForm</div>,
}));

describe("SettingsPanel auto-share group", () => {
  // Without this, a toggle that exists and works but was never added to the nav
  // would be unreachable — the panel would render, every form test would pass,
  // and no user could ever find the switch.
  it("registers the Auto Share group in the nav", () => {
    render(<SettingsPanel />);
    expect(screen.getByRole("button", { name: "Auto Share" })).toBeTruthy();
  });

  it("selecting Auto Share renders AutoShareForm", () => {
    render(<SettingsPanel />);
    fireEvent.click(screen.getByRole("button", { name: "Auto Share" }));
    expect(screen.getByTestId("auto-share-form")).toBeTruthy();
  });
});