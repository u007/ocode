import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import PluginsPanel from "./PluginsPanel";
import { api } from "../../api/client";

vi.mock("../../api/client", () => ({
  api: {
    listPlugins: vi.fn(),
    getPluginsEnabledConfig: vi.fn(),
    setPluginsEnabledConfig: vi.fn(),
    setPluginEnabled: vi.fn(),
    removePlugin: vi.fn(),
    installPlugin: vi.fn(),
  },
}));

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.listPlugins).mockResolvedValue([]);
  vi.mocked(api.getPluginsEnabledConfig).mockResolvedValue({ ast: false } as never);
});

describe("PluginsPanel install", () => {
  it("shows the report when a bare name installs a bundled skill", async () => {
    vi.mocked(api.installPlugin).mockResolvedValue({
      skill: "standup",
      report: "installed standup in /h/.claude/skills",
    });
    render(<PluginsPanel open onOpenChange={() => {}} />);

    fireEvent.change(screen.getByPlaceholderText(/name, git URL/), { target: { value: "standup" } });
    fireEvent.click(screen.getByRole("button", { name: /Install/ }));

    await waitFor(() =>
      expect(screen.getByText("installed standup in /h/.claude/skills")).toBeTruthy(),
    );
    expect(api.installPlugin).toHaveBeenCalledWith("standup");
  });

  it("shows the server error when the install fails", async () => {
    vi.mocked(api.installPlugin).mockRejectedValue(new Error('no bundled skill "nope" (bundled: standup)'));
    render(<PluginsPanel open onOpenChange={() => {}} />);

    fireEvent.change(screen.getByPlaceholderText(/name, git URL/), { target: { value: "nope" } });
    fireEvent.click(screen.getByRole("button", { name: /Install/ }));

    await waitFor(() =>
      expect(screen.getByText('no bundled skill "nope" (bundled: standup)')).toBeTruthy(),
    );
  });
});
