import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import SettingsPanel from "./SettingsPanel";

// Only the default group and the group this suite selects need real bodies;
// every other form would drag in chat/config stores just to assert the menu.
vi.mock("./ModelDefaultsForm", () => ({
  default: () => <div data-testid="model-defaults-form">ModelDefaultsForm</div>,
}));
vi.mock("./ThemeForm", () => ({
  default: () => <div data-testid="theme-form">ThemeForm</div>,
}));

function filterInput(): HTMLInputElement {
  return screen.getByLabelText("Filter settings sections") as HTMLInputElement;
}

function sectionButton(label: string): HTMLElement | null {
  return screen.queryByRole("button", { name: label });
}

describe("SettingsPanel section filter", () => {
  it("shows every section before a query is typed", () => {
    render(<SettingsPanel />);
    expect(sectionButton("Backend")).not.toBeNull();
    expect(sectionButton("Theme")).not.toBeNull();
    expect(filterInput().value).toBe("");
  });

  it("narrows the menu to matching labels, case-insensitively", () => {
    render(<SettingsPanel />);
    fireEvent.change(filterInput(), { target: { value: "THEME" } });

    expect(sectionButton("Theme")).not.toBeNull();
    expect(sectionButton("Backend")).toBeNull();
    expect(screen.queryByText("Models & Providers")).toBeNull();
  });

  it("matches a category name and lists that whole category", () => {
    render(<SettingsPanel />);
    fireEvent.change(filterInput(), { target: { value: "voice" } });

    for (const label of [
      "Speech playback",
      "Speech to text",
      "OCR",
      "Audio Generation",
    ]) {
      expect(sectionButton(label)).not.toBeNull();
    }
    expect(sectionButton("Terminal")).toBeNull();
  });

  it("matches a section by its extra keywords, not only its label", () => {
    render(<SettingsPanel />);
    // "Upstash Box" is not called "sandbox" or "remote browser" anywhere but in
    // its keywords, so these queries only find it through the keyword list.
    fireEvent.change(filterInput(), { target: { value: "sandbox" } });
    expect(sectionButton("Upstash Box")).not.toBeNull();
    expect(sectionButton("Backend")).toBeNull();

    fireEvent.change(filterInput(), { target: { value: "remote browser" } });
    expect(sectionButton("Upstash Box")).not.toBeNull();
    expect(sectionButton("Git")).toBeNull();
  });

  it("requires every whitespace-separated term (AND), not any", () => {
    render(<SettingsPanel />);
    fireEvent.change(filterInput(), { target: { value: "chat display" } });

    expect(sectionButton("Chat display")).not.toBeNull();
    // "Chat Connectors" matches `chat` but not `display`.
    expect(sectionButton("Chat Connectors")).toBeNull();
  });

  it("shows an empty state when nothing matches, and the clear button restores the list", () => {
    render(<SettingsPanel />);
    fireEvent.change(filterInput(), { target: { value: "zzzz" } });

    expect(screen.getByText(/No settings match/)).not.toBeNull();
    expect(sectionButton("Backend")).toBeNull();

    fireEvent.click(screen.getByLabelText("Clear settings filter"));
    expect(filterInput().value).toBe("");
    expect(sectionButton("Backend")).not.toBeNull();
  });

  it("selects a filtered section and keeps the query in place", () => {
    render(<SettingsPanel />);
    fireEvent.change(filterInput(), { target: { value: "theme" } });
    fireEvent.click(screen.getByRole("button", { name: "Theme" }));

    expect(screen.getByTestId("theme-form")).not.toBeNull();
    expect(filterInput().value).toBe("theme");
  });

  it("keeps focus while the menu re-renders on each keystroke", () => {
    render(<SettingsPanel />);
    const input = filterInput();
    input.focus();
    fireEvent.change(input, { target: { value: "th" } });

    // Guards the shared `navSections` JSX value: turning it into an inline
    // component would remount the input and drop the focus after every key.
    expect(document.activeElement).toBe(input);
  });

  it("mounts the same filter in the mobile drawer", () => {
    const width = window.innerWidth;
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      writable: true,
      value: 390,
    });
    try {
      render(<SettingsPanel />);
      const drawer = document.getElementById("settings-nav-drawer");
      expect(drawer).not.toBeNull();
      expect(
        drawer!.querySelector('input[aria-label="Filter settings sections"]'),
      ).not.toBeNull();
    } finally {
      Object.defineProperty(window, "innerWidth", {
        configurable: true,
        writable: true,
        value: width,
      });
    }
  });
});
