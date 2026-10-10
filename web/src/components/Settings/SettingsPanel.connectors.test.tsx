import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import SettingsPanel from "./SettingsPanel";

// SettingsPanel renders the default (model-defaults) group on mount; stub it so
// these assertions don't drag in the whole chat store.
vi.mock("./ModelDefaultsForm", () => ({ default: () => null }));

// Render a marker instead of the real form: the form's own behaviour is covered
// by ConnectorsForm.test.tsx, and here we only care that the section is
// reachable and is wired to the right component.
vi.mock("./ConnectorsForm", () => ({
  default: ({ host }: { host?: string }) => (
    <div data-testid="connectors-form" data-host={host ?? "(none)"}>ConnectorsForm</div>
  ),
}));

describe("SettingsPanel connectors group", () => {
  it("renders the Connectors group", () => {
    render(<SettingsPanel />);
    expect(screen.getByRole("button", { name: "Connectors" })).toBeTruthy();
  });

  it("selecting Connectors renders ConnectorsForm", () => {
    render(<SettingsPanel />);
    fireEvent.click(screen.getByRole("button", { name: "Connectors" }));
    expect(screen.getByTestId("connectors-form")).toBeTruthy();
  });

  it("is listed before Profiles so the two credential surfaces read in order", () => {
    render(<SettingsPanel />);
    const connectors = screen.getByRole("button", { name: "Connectors" });
    const profiles = screen.getByRole("button", { name: "Profiles" });
    // compareDocumentPosition: DOCUMENT_POSITION_FOLLOWING === 4. Connectors is
    // the base auth.json store and Profiles is the per-profile overlay, so the
    // general-then-specific order is intentional, not incidental.
    expect(connectors.compareDocumentPosition(profiles) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("passes NO host, because settings are a global surface", () => {
    render(<SettingsPanel />);
    fireEvent.click(screen.getByRole("button", { name: "Connectors" }));
    // Guards a real regression: threading a project host here would point the
    // form at a REMOTE machine's credential store from a GLOBAL panel.
    expect(screen.getByTestId("connectors-form").getAttribute("data-host")).toBe("(none)");
  });
});