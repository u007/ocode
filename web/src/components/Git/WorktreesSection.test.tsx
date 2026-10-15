import type { ComponentProps } from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { GitWorktree } from "@/api/types";
import WorktreesSection from "./WorktreesSection";

const mocks = vi.hoisted(() => ({
  gitWorktrees: vi.fn(),
  gitWorktreeCreate: vi.fn(),
  gitWorktreeRemove: vi.fn(),
}));

vi.mock("@/api/client", () => ({ api: mocks }));

const main: GitWorktree = {
  path: "/proj",
  head: "aaaaaaa1111",
  branch: "main",
  detached: false,
  locked: false,
  prunable: false,
  is_main: true,
  is_current: true,
};

const linked: GitWorktree = {
  path: "/proj/.worktrees/feature/x",
  head: "bbbbbbb2222",
  branch: "feature/x",
  detached: false,
  locked: false,
  prunable: false,
  is_main: false,
  is_current: false,
};

function renderSection(
  overrides: Partial<ComponentProps<typeof WorktreesSection>> = {},
) {
  const props = {
    projectPath: "/proj",
    projectHost: undefined,
    expanded: true,
    onToggle: vi.fn(),
    onOpenWorktree: vi.fn().mockResolvedValue(undefined),
    className: "",
    ...overrides,
  };
  render(<WorktreesSection {...props} />);
  return props;
}

describe("WorktreesSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.gitWorktrees.mockResolvedValue([main, linked]);
  });

  it("does not fetch while collapsed", () => {
    renderSection({ expanded: false });
    expect(mocks.gitWorktrees).not.toHaveBeenCalled();
  });

  it("lists checkouts with main and open markers", async () => {
    renderSection();
    await screen.findByText("feature/x");
    expect(mocks.gitWorktrees).toHaveBeenCalledWith("/proj");
    expect(screen.getAllByText("main").length).toBeGreaterThan(0);
    expect(screen.getByText("open")).toBeTruthy();
    expect(screen.getByText("(2)")).toBeTruthy();
  });

  it("refuses to remove the main worktree, offering no remove button for it", async () => {
    renderSection();
    await screen.findByText("feature/x");
    expect(screen.queryByRole("button", { name: `Remove worktree ${main.path}` })).toBeNull();
    expect(screen.getByRole("button", { name: `Remove worktree ${linked.path}` })).toBeTruthy();
  });

  it("disables Open on the checkout the project is already on", async () => {
    renderSection();
    const row = (await screen.findByText("feature/x")).closest("div.group") as HTMLElement;
    const mainRow = screen.getByText("/proj").closest("div.group") as HTMLElement;
    const openMain = within(mainRow).getByRole("button", { name: "Open" });
    expect((openMain as HTMLButtonElement).disabled).toBe(true);
    const openLinked = within(row).getByRole("button", { name: "Open" });
    expect((openLinked as HTMLButtonElement).disabled).toBe(false);
  });

  it("opens a linked checkout through the handler", async () => {
    const props = renderSection();
    const row = (await screen.findByText("feature/x")).closest("div.group") as HTMLElement;
    fireEvent.click(within(row).getByRole("button", { name: "Open" }));
    await waitFor(() => expect(props.onOpenWorktree).toHaveBeenCalledWith(linked.path));
  });

  it("shows the reason when opening fails", async () => {
    renderSection({
      onOpenWorktree: vi.fn().mockRejectedValue(new Error("add project: denied")),
    });
    const row = (await screen.findByText("feature/x")).closest("div.group") as HTMLElement;
    fireEvent.click(within(row).getByRole("button", { name: "Open" }));
    expect(await screen.findByText(/add project: denied/)).toBeTruthy();
  });

  it("hides the Open action when no handler is given", async () => {
    renderSection({ onOpenWorktree: undefined });
    await screen.findByText("feature/x");
    expect(screen.queryByRole("button", { name: "Open" })).toBeNull();
  });

  it("does not fetch and shows a notice for a remote project", async () => {
    renderSection({ projectHost: "devbox" });
    expect(await screen.findByText(/local projects only/)).toBeTruthy();
    expect(mocks.gitWorktrees).not.toHaveBeenCalled();
  });

  it("shows a load error inside the section", async () => {
    mocks.gitWorktrees.mockRejectedValue(new Error("not a git repository"));
    renderSection();
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByText(/not a git repository/)).toBeTruthy();
  });

  it("creates a worktree from the dialog and shows the refreshed list", async () => {
    const created: GitWorktree = { ...linked, path: "/proj/.worktrees/new", branch: "new", is_current: false };
    mocks.gitWorktreeCreate.mockResolvedValue([main, linked, created]);
    renderSection();
    await screen.findByText("feature/x");

    fireEvent.click(screen.getByRole("button", { name: /New/ }));
    const input = screen.getByLabelText("Branch name");
    fireEvent.change(input, { target: { value: "  new  " } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => expect(mocks.gitWorktreeCreate).toHaveBeenCalledWith("new", "/proj"));
    expect(await screen.findByText("(3)")).toBeTruthy();
  });

  it("keeps the create dialog open with git's reason when creation is refused", async () => {
    mocks.gitWorktreeCreate.mockRejectedValue(new Error("git worktree add failed: already checked out"));
    renderSection();
    await screen.findByText("feature/x");

    fireEvent.click(screen.getByRole("button", { name: /New/ }));
    fireEvent.change(screen.getByLabelText("Branch name"), { target: { value: "main" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    expect(await screen.findByText(/already checked out/)).toBeTruthy();
    expect(screen.getByLabelText("Branch name")).toBeTruthy();
  });

  it("removes a checkout after confirmation", async () => {
    mocks.gitWorktreeRemove.mockResolvedValue([main]);
    renderSection();
    await screen.findByText("feature/x");

    fireEvent.click(screen.getByRole("button", { name: `Remove worktree ${linked.path}` }));
    fireEvent.click(await screen.findByRole("button", { name: "Remove" }));

    await waitFor(() => expect(mocks.gitWorktreeRemove).toHaveBeenCalledWith(linked.path, "/proj"));
    expect(await screen.findByText("(1)")).toBeTruthy();
  });

  it("keeps the confirm open with git's refusal for a dirty checkout", async () => {
    mocks.gitWorktreeRemove.mockRejectedValue(new Error("contains modified or untracked files"));
    renderSection();
    await screen.findByText("feature/x");

    fireEvent.click(screen.getByRole("button", { name: `Remove worktree ${linked.path}` }));
    fireEvent.click(await screen.findByRole("button", { name: "Remove" }));

    expect(await screen.findByText(/modified or untracked files/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Remove" })).toBeTruthy();
  });
});
