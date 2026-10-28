import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import SettingsPanel from "./SettingsPanel";

// SettingsPanel renders the default (model-defaults) group on mount; stub it so
// the nav-registration assertions don't drag in the whole chat store.
vi.mock("./ModelDefaultsForm", () => ({ default: () => null }));
vi.mock("./GitForm", () => ({
  default: () => <div data-testid="git-form">GitForm</div>,
}));

describe("SettingsPanel git group", () => {
  it("registers the Git group in the nav", () => {
    render(<SettingsPanel />);
    expect(screen.getByRole("button", { name: "Git" })).toBeTruthy();
  });

  it("selecting Git renders GitForm", () => {
    render(<SettingsPanel />);
    fireEvent.click(screen.getByRole("button", { name: "Git" }));
    expect(screen.getByTestId("git-form")).toBeTruthy();
  });
});
