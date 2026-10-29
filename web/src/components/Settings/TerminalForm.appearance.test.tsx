import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import TerminalForm from "./TerminalForm";
import { api } from "../../api/client";

// The component calls methods on the EXPORTED api object, so that object is what
// has to be mocked (a named-import patch would leave the real fetchJSON running).
vi.mock("../../api/client", () => ({
  api: {
    getTerminalConfig: vi.fn(),
    setTerminalScrollbackLines: vi.fn(),
    setTerminalAppearance: vi.fn(),
    setTerminalShell: vi.fn(),
  },
}));

// refreshTerminalConfig() publishes into the shared config cache, which would
// leak between suites; the form re-loads via getTerminalConfig after saving.
vi.mock("@/hooks/useTerminalConfig", async () => {
  const actual = await vi.importActual<typeof import("@/hooks/useTerminalConfig")>("@/hooks/useTerminalConfig");
  return { ...actual, refreshTerminalConfig: vi.fn() };
});

const mockGet = vi.mocked(api.getTerminalConfig);
const mockSetAppearance = vi.mocked(api.setTerminalAppearance);
const mockSetScrollback = vi.mocked(api.setTerminalScrollbackLines);
const mockSetShell = vi.mocked(api.setTerminalShell);

const baseConfig = {
  available: true,
  scrollback_lines: 9999,
  font_family: `"Fira Code", ui-monospace, monospace`,
  font_size: 12,
  shell: "",
  default_shell: "/bin/zsh",
  available_shells: ["/bin/zsh", "/bin/bash"],
  work_dir: "/proj",
};

const ghosttyFound = {
  found: true,
  paths: ["/home/u/.config/ghostty/config"],
  font_family: `"Fira Code", ui-monospace, monospace`,
  font_size: 12,
  cell_width: {},
  cell_height: { percent: 5 },
  theme_name: "Bright Lights",
  keybinds: [
    { chord: "cmd+plus", action: "increase_font_size", arg: "1" },
    { chord: "cmd+shift+t", action: "new_tab" },
    { chord: "global:ctrl+grave_accent", action: "toggle_quick_terminal", global: true },
  ],
  ignored: [{ key: "background-opacity", value: "0.9", reason: "cells are drawn opaque on a 2D canvas" }],
};

beforeEach(() => {
  vi.clearAllMocks();
  mockSetAppearance.mockResolvedValue({ font_family: "mono", font_size: 12 });
  mockSetScrollback.mockResolvedValue({ scrollback_lines: 9999 });
  mockSetShell.mockResolvedValue({ shell: "" });
});

describe("TerminalForm appearance source", () => {
  it("follows Ghostty by default and shows what it resolved", async () => {
    mockGet.mockResolvedValue({
      ...baseConfig,
      appearance: {
        source: "ghostty",
        requested: "ghostty",
        font_family: `"Fira Code", ui-monospace, monospace`,
        font_size: 12,
        cell_width_adjust: {},
        cell_height_adjust: { percent: 5 },
        theme_name: "Bright Lights",
        settings: { font_family: "Menlo", font_size: 14, cell_width_percent: 100, cell_height_percent: 120 },
      },
      ghostty: ghosttyFound,
      appearance_scope: "local",
    });

    render(<TerminalForm />);
    await screen.findByText("Appearance source");

    expect((screen.getByLabelText(/Follow my Ghostty configuration/) as HTMLInputElement).checked).toBe(true);
    expect(screen.getByText(/Bright Lights/)).toBeTruthy();
    // The ocode fields are disabled while Ghostty is the source, so a save can
    // never silently mix the two.
    const fontInput = screen.getByPlaceholderText(/ui-monospace/) as HTMLInputElement;
    expect(fontInput.disabled).toBe(true);
  });

  it("explains the fallback when no Ghostty config exists", async () => {
    mockGet.mockResolvedValue({
      ...baseConfig,
      appearance: {
        source: "ocode",
        requested: "ghostty",
        font_family: "Menlo",
        font_size: 14,
        cell_width_adjust: {},
        cell_height_adjust: { percent: 10 },
        settings: { font_family: "Menlo", font_size: 14, cell_width_percent: 100, cell_height_percent: 110 },
      },
      ghostty: { found: false },
      appearance_scope: "local",
    });

    render(<TerminalForm />);
    expect(await screen.findByText(/No Ghostty configuration found/)).toBeTruthy();
    expect(screen.getByText(/using its own settings because no Ghostty config exists/)).toBeTruthy();
    // Requested ghostty but effective ocode: the fields must be EDITABLE, since
    // ocode's values are what is on screen.
    expect((screen.getByPlaceholderText(/ui-monospace/) as HTMLInputElement).disabled).toBe(false);
  });

  it("saves the whole appearance block with absolute percentages", async () => {
    mockGet.mockResolvedValue({
      ...baseConfig,
      appearance: {
        source: "ocode",
        requested: "ocode",
        font_family: "Menlo",
        font_size: 14,
        cell_width_adjust: { percent: 0 },
        cell_height_adjust: { percent: 20 },
        settings: { font_family: "Menlo", font_size: 14, cell_width_percent: 100, cell_height_percent: 120 },
      },
      ghostty: ghosttyFound,
      appearance_scope: "local",
    });

    render(<TerminalForm />);
    await screen.findByText("Appearance source");
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(mockSetAppearance).toHaveBeenCalledTimes(1));
    expect(mockSetAppearance.mock.calls[0][0]).toMatchObject({
      appearance_source: "ocode",
      font_family: "Menlo",
      font_size: 14,
      cell_width_percent: 100,
      cell_height_percent: 120,
    });
    expect(mockSetScrollback).toHaveBeenCalledWith(9999);
  });

  it("seeds the ocode fields from the resolved Ghostty values on 'Copy to ocode settings'", async () => {
    mockGet.mockResolvedValue({
      ...baseConfig,
      appearance: {
        source: "ghostty",
        requested: "ghostty",
        font_family: `"Fira Code", ui-monospace, monospace`,
        font_size: 12,
        cell_width_adjust: {},
        cell_height_adjust: { percent: 5 },
        theme_name: "Bright Lights",
        settings: { font_family: "Menlo", font_size: 14, cell_width_percent: 100, cell_height_percent: 110 },
      },
      ghostty: ghosttyFound,
      appearance_scope: "local",
    });

    render(<TerminalForm />);
    fireEvent.click(await screen.findByText("Copy to ocode settings"));
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(mockSetAppearance).toHaveBeenCalledTimes(1));
    expect(mockSetAppearance.mock.calls[0][0]).toMatchObject({
      appearance_source: "ocode",
      font_family: `"Fira Code", ui-monospace, monospace`,
      font_size: 12,
      cell_height_percent: 105,
      theme: "Bright Lights",
    });
  });
});

describe("TerminalForm keybind + portability report", () => {
  it("lists only mappable bindings as applied and explains the rest", async () => {
    mockGet.mockResolvedValue({
      ...baseConfig,
      appearance: {
        source: "ghostty",
        requested: "ghostty",
        font_family: "mono",
        font_size: 12,
        cell_width_adjust: {},
        cell_height_adjust: {},
        settings: { font_family: "mono", font_size: 12, cell_width_percent: 100, cell_height_percent: 100 },
      },
      ghostty: ghosttyFound,
      appearance_scope: "local",
    });

    render(<TerminalForm />);
    expect(await screen.findByText(/Applied:/)).toBeTruthy();
    expect(screen.getByText(/2 bindings not applied/)).toBeTruthy();
    expect(screen.getByText(/background-opacity/)).toBeTruthy();
  });

  it("hides the whole report when Ghostty has nothing to say", async () => {
    mockGet.mockResolvedValue({
      ...baseConfig,
      appearance: {
        source: "ocode",
        requested: "ocode",
        font_family: "mono",
        font_size: 12,
        cell_width_adjust: {},
        cell_height_adjust: {},
        settings: { font_family: "mono", font_size: 12, cell_width_percent: 100, cell_height_percent: 100 },
      },
      ghostty: { found: false },
      appearance_scope: "local",
    });

    render(<TerminalForm />);
    await screen.findByText("Appearance source");
    expect(screen.queryByText(/Applied:/)).toBeNull();
    expect(screen.queryByText(/Not portable from Ghostty/)).toBeNull();
  });
});
