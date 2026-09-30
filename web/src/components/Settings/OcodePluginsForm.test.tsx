import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import OcodePluginsForm from "./OcodePluginsForm";
import { api } from "../../api/client";

vi.mock("../../api/client", () => ({
  api: {
    getPluginsEnabledConfig: vi.fn(),
    setPluginsEnabledConfig: vi.fn(),
    listPlugins: vi.fn(),
    getLocalModelsConfig: vi.fn(),
    setPluginEnabled: vi.fn(),
    removePlugin: vi.fn(),
    installPlugin: vi.fn(),
  },
}));

const mockList = vi.mocked(api.listPlugins);
const mockToggle = vi.mocked(api.setPluginEnabled);

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.getPluginsEnabledConfig).mockResolvedValue({ ast: false });
  vi.mocked(api.getLocalModelsConfig).mockResolvedValue({} as never);
  mockToggle.mockResolvedValue(undefined as never);
  mockList.mockResolvedValue([
    { name: "superpowers", source: "claude-code", dir: "/h/.claude/plugins/cache/m/superpowers/6", enabled: false },
    { name: "mine", source: "github.com/u/mine", dir: "/h/.config/opencode/plugins/mine", enabled: true },
  ]);
});

function row(name: string): HTMLElement {
  const el = screen.getByText(name).closest("div.flex.items-center.justify-between");
  if (!el) throw new Error(`row for ${name} not found`);
  return el as HTMLElement;
}

describe("OcodePluginsForm with Claude Code plugins", () => {
  it("badges a Claude Code plugin and offers toggle but no remove", async () => {
    render(<OcodePluginsForm />);
    await waitFor(() => expect(screen.getByText("superpowers")).toBeTruthy());

    const cc = row("superpowers");
    expect(within(cc).getByText("Claude Code")).toBeTruthy();
    expect(within(cc).getAllByRole("button")).toHaveLength(1); // toggle only

    const own = row("mine");
    expect(within(own).queryByText("Claude Code")).toBeNull();
    expect(within(own).getAllByRole("button")).toHaveLength(2); // toggle + remove
  });

  it("toggles a Claude Code plugin through ocode's plugin API", async () => {
    render(<OcodePluginsForm />);
    await waitFor(() => expect(screen.getByText("superpowers")).toBeTruthy());

    fireEvent.click(within(row("superpowers")).getByRole("button", { name: "Off" }));
    await waitFor(() => expect(mockToggle).toHaveBeenCalledWith("superpowers", true));
  });
});
