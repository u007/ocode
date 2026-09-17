import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import ComputerUseForm from "./ComputerUseForm";
import SettingsPanel from "./SettingsPanel";
import { api } from "../../api/client";

// SettingsPanel renders the default (model-defaults) group on mount; stub it so
// the nav-registration assertions don't drag in the whole chat store.
vi.mock("./ModelDefaultsForm", () => ({ default: () => null }));

vi.mock("../../api/client", () => ({
  api: {
    getComputerUseConfig: vi.fn(),
    setComputerUseConfig: vi.fn(),
    requestComputerUsePermissions: vi.fn(),
  },
}));

const mockGet = vi.mocked(api.getComputerUseConfig);
const mockSet = vi.mocked(api.setComputerUseConfig);
const mockRequest = vi.mocked(api.requestComputerUsePermissions);

const statusLines = [
  "Computer use: disabled",
  "Backend: macOS: screencapture + CGEvent",
];

beforeEach(() => {
  vi.clearAllMocks();
  mockGet.mockResolvedValue({ enabled: false, status_lines: statusLines } as never);
  mockSet.mockResolvedValue({ enabled: true, status_lines: statusLines } as never);
  mockRequest.mockResolvedValue({
    platform: "darwin",
    granted: false,
    lines: ["Accessibility: not granted yet.", "Opened System Settings → Privacy & Security."],
  } as never);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ComputerUseForm", () => {
  it("loads the persisted setting and renders the shared status lines", async () => {
    render(<ComputerUseForm />);
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(1));

    const toggle = screen.getByRole("checkbox") as HTMLInputElement;
    expect(toggle.checked).toBe(false);
    expect(screen.getByTestId("computer-use-status").textContent).toContain(
      "Backend: macOS: screencapture + CGEvent",
    );
  });

  it("persists the toggle on Save", async () => {
    render(<ComputerUseForm />);
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(1));

    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mockSet).toHaveBeenCalledWith(true));
  });

  it("requests OS permissions and renders the report lines", async () => {
    render(<ComputerUseForm />);
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(1));

    fireEvent.click(screen.getByRole("button", { name: /Request permissions/ }));

    await waitFor(() => expect(mockRequest).toHaveBeenCalledTimes(1));
    const result = await screen.findByTestId("computer-use-permission-result");
    expect(result.textContent).toContain("Opened System Settings");
    expect(result.textContent).toContain("Accessibility: not granted yet.");
  });

  it("surfaces a permission-request failure", async () => {
    mockRequest.mockRejectedValue(new Error("permission route exploded"));
    render(<ComputerUseForm />);
    await waitFor(() => expect(mockGet).toHaveBeenCalledTimes(1));

    fireEvent.click(screen.getByRole("button", { name: /Request permissions/ }));

    expect(await screen.findByText(/permission route exploded/)).toBeDefined();
  });

  it("surfaces a load failure instead of silently showing defaults", async () => {
    mockGet.mockRejectedValue(new Error("config route exploded"));
    render(<ComputerUseForm />);
    expect(await screen.findByText(/config route exploded/)).toBeDefined();
  });
});

describe("SettingsPanel computer-use group", () => {
  it("registers a Computer Use nav entry that mounts the form", async () => {
    render(<SettingsPanel />);
    const navButton = screen.getByRole("button", { name: "Computer Use" });
    fireEvent.click(navButton);
    await waitFor(() => expect(mockGet).toHaveBeenCalled());
    expect(screen.getByRole("checkbox")).toBeDefined();
  });
});
