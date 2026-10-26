import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import SettingsPanel from "./SettingsPanel";

vi.mock("./ModelDefaultsForm", () => ({ default: () => null }));
vi.mock("./PromptsForm", () => ({
  default: () => <div data-testid="prompts-form">PromptsForm</div>,
}));

describe("SettingsPanel System Prompts group", () => {
  it("is in the nav and renders its form when selected", () => {
    render(<SettingsPanel />);
    expect(screen.queryByTestId("prompts-form")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "System Prompts" }));
    expect(screen.getByTestId("prompts-form")).toBeTruthy();
  });
});
