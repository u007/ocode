import { describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import SettingsPanel from "./SettingsPanel";
import { OPEN_PULSE_ASSISTANT_SETTINGS_EVENT } from "../../lib/pulseAssistant";

vi.mock("./ModelDefaultsForm", () => ({ default: () => null }));
vi.mock("./PulseAssistantForm", () => ({
  default: () => <div data-testid="pulse-assistant-form">PulseAssistantForm</div>,
}));

describe("SettingsPanel Pulse assistant group", () => {
  it("is in the nav and renders its form when selected", () => {
    render(<SettingsPanel />);
    fireEvent.click(screen.getByRole("button", { name: "Pulse assistant" }));
    expect(screen.getByTestId("pulse-assistant-form")).toBeTruthy();
  });

  it("jumps to the section when the drawer asks for it", () => {
    render(<SettingsPanel />);
    expect(screen.queryByTestId("pulse-assistant-form")).toBeNull();

    act(() => {
      window.dispatchEvent(new CustomEvent(OPEN_PULSE_ASSISTANT_SETTINGS_EVENT));
    });

    expect(screen.getByTestId("pulse-assistant-form")).toBeTruthy();
  });
});
