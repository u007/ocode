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
    listIntentSkills: vi.fn(),
  },
}));

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.listPlugins).mockResolvedValue([]);
  vi.mocked(api.getPluginsEnabledConfig).mockResolvedValue({ ast: false } as never);
});

describe("PluginsPanel install", () => {
  it("stacks the intent checklist over the panel; Escape closes only the checklist, and closing the panel leaves the page interactive", async () => {
    vi.mocked(api.listIntentSkills).mockResolvedValue({
      version: "0.5.6",
      skills: [{ name: "generate-skill", description: "Use when generating skills." }],
    });
    const onOpenChange = vi.fn();
    const { rerender } = render(<PluginsPanel open onOpenChange={onOpenChange} />);

    fireEvent.change(screen.getByPlaceholderText(/name, git URL/), { target: { value: "@tanstack/intent" } });
    fireEvent.click(screen.getByRole("button", { name: /Install/ }));
    expect(await screen.findByText("generate-skill")).toBeTruthy();

    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(screen.queryByText("generate-skill")).toBeNull());
    expect(screen.getByPlaceholderText(/name, git URL/)).toBeTruthy();
    expect(onOpenChange).not.toHaveBeenCalledWith(false);

    rerender(<PluginsPanel open={false} onOpenChange={onOpenChange} />);
    await waitFor(() => expect(document.body.style.pointerEvents).not.toBe("none"));
  });

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

describe("PluginsPanel keyword filter", () => {
  it("filters installed and builtin plugins by keyword and reports no match", async () => {
    vi.mocked(api.listPlugins).mockResolvedValue([
      { name: "deploy-kit", source: "github.com/u/deploy-kit", dir: "/d", enabled: true, description: "Ship containers" },
      { name: "notes", source: "github.com/u/notes", dir: "/n", enabled: false, description: "Meeting notes" },
    ]);
    render(<PluginsPanel open onOpenChange={() => {}} />);
    await waitFor(() => expect(screen.getByText("deploy-kit")).toBeTruthy());

    const filter = screen.getByLabelText("Filter plugins");
    fireEvent.change(filter, { target: { value: "containers" } });
    expect(screen.getByText("deploy-kit")).toBeTruthy();
    expect(screen.queryByText("notes")).toBeNull();
    expect(screen.queryByText("ast")).toBeNull();

    fireEvent.change(filter, { target: { value: "AST" } });
    expect(screen.getByText("ast")).toBeTruthy();
    expect(screen.queryByText("deploy-kit")).toBeNull();

    fireEvent.change(filter, { target: { value: "zzz" } });
    expect(screen.getByText(/No plugins match/)).toBeTruthy();
    expect(screen.queryByText("ast")).toBeNull();
  });
});
