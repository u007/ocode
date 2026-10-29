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

  it("fetches while collapsed so the header can flag unused checkouts, but renders no rows", async () => {
    mocks.gitWorktrees.mockResolvedValue([
      { ...main, owner_session_count: 1 },
      { ...linked, owner_session_count: 0 },
      { ...linked, path: "/proj/.worktrees/feature/y", branch: "feature/y", owner_session_count: 0 },
    ]);
    renderSection({ expanded: false });
    await waitFor(() => expect(mocks.gitWorktrees).toHaveBeenCalledWith("/proj"));

    const flag = await screen.findByTestId("worktrees-unused-flag");
    expect(flag.textContent).toContain("2 no session");
    expect(flag.getAttribute("title")).toContain("2 linked checkouts have no chat session");
    // Collapsed still means collapsed: the rows themselves are not mounted.
    expect(screen.queryByText("feature/x")).toBeNull();
  });

  it("shows no header flag when every checkout has a session", async () => {
    mocks.gitWorktrees.mockResolvedValue([
      { ...main, owner_session_count: 1 },
      { ...linked, owner_session_count: 2 },
    ]);
    renderSection();
    await screen.findByText("feature/x");
    expect(screen.queryByTestId("worktrees-unused-flag")).toBeNull();
  });

  it("flags a failed list in the collapsed header instead of hiding it", async () => {
    mocks.gitWorktrees.mockRejectedValue(new Error("not a git repository"));
    renderSection({ expanded: false });

    const flag = await screen.findByTestId("worktrees-load-error-flag");
    expect(flag.textContent).toContain("unavailable");
    expect(flag.getAttribute("title")).toContain("not a git repository");
    // Still collapsed: the detailed alert lives in the expanded body.
    expect(screen.queryByRole("alert")).toBeNull();
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

  // ── Derived state ────────────────────────────────────────────────────────

  it("shows the checkout's divergence, dirtiness, owners and last commit", async () => {
    mocks.gitWorktrees.mockResolvedValue([
      main,
      {
        ...linked,
        exists: true,
        dirty: 2,
        ahead: 3,
        behind: 0,
        contained_in_base: false,
        base_ref: "main",
        base_known: true,
        owner_sessions: [{ session_id: "ses_1" }],
        owner_session_count: 1,
        last_commit: "2026-10-28T12:00:00+08:00",
      },
    ]);
    renderSection();
    await screen.findByText("feature/x");

    const stateLine = screen.getByText(/↑3 vs main/);
    expect(stateLine.textContent).toContain("2 changed");
    expect(stateLine.textContent).toContain("1 session");
    expect(stateLine.textContent).toContain("last commit 2026-10-28");
    expect(stateLine.getAttribute("title")).toContain("Not contained in main");
  });

  it("reports containment without ever calling it merged", async () => {
    mocks.gitWorktrees.mockResolvedValue([
      main,
      {
        ...linked,
        ahead: 0,
        behind: 0,
        contained_in_base: true,
        base_ref: "main",
        base_known: true,
        owner_session_count: 1,
      },
    ]);
    renderSection();
    await screen.findByText("feature/x");

    const stateLine = screen.getByText(/contained in main/);
    expect(stateLine.textContent).not.toContain("merged");
    expect(stateLine.getAttribute("title")).toContain("squash/rebase merges are not detected");
  });

  it("marks a linked checkout nobody has used, but never the main checkout", async () => {
    mocks.gitWorktrees.mockResolvedValue([
      { ...main, owner_session_count: 0 },
      { ...linked, owner_session_count: 0 },
    ]);
    renderSection();
    await screen.findByText("feature/x");

    const linkedRow = screen.getByText("/proj/.worktrees/feature/x").closest("div.group") as HTMLElement;
    const mainRow = screen.getByText("/proj").closest("div.group") as HTMLElement;
    expect(within(linkedRow).getByText("no session")).toBeTruthy();
    expect(within(mainRow).queryByText("no session")).toBeNull();
  });

  it("invents no derived state against an older server", async () => {
    // Every new field absent: the row may name the checkout, but it must not
    // claim it is clean, contained or unowned.
    mocks.gitWorktrees.mockResolvedValue([main, linked]);
    renderSection();
    await screen.findByText("feature/x");

    expect(screen.queryByText("no session")).toBeNull();
    expect(screen.queryByText(/changed/)).toBeNull();
    expect(screen.queryByText(/contained/)).toBeNull();
    expect(screen.queryByText(/↑/)).toBeNull();
  });
});
