import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import SettingsPanel from "./SettingsPanel";

// SettingsPanel renders the default (model-defaults) group on mount; stub it so
// the nav-registration assertions don't drag in the whole chat store.
vi.mock("./ModelDefaultsForm", () => ({ default: () => null }));

// The vault group is the thing under test, so render a marker instead of the
// real form (which would call the api client).
vi.mock("./VaultForm", () => ({
  default: () => <div data-testid="vault-form">VaultForm</div>,
}));

describe("SettingsPanel vault group", () => {
  it("renders the Passwords group", () => {
    render(<SettingsPanel />);
    expect(screen.getByRole("button", { name: "Passwords" })).toBeTruthy();
  });

  it("selecting Passwords renders VaultForm", () => {
    render(<SettingsPanel />);
    fireEvent.click(screen.getByRole("button", { name: "Passwords" }));
    expect(screen.getByTestId("vault-form")).toBeTruthy();
  });
});
