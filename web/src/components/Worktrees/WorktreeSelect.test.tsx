import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { GitWorktree } from "@/api/types";
import WorktreeSelect from "./WorktreeSelect";

const mocks = vi.hoisted(() => ({ gitWorktrees: vi.fn() }));

vi.mock("@/api/client", () => ({ api: mocks }));

// Radix Select (and its popper) expects pointer capture and ResizeObserver,
// neither of which jsdom implements.
beforeAll(() => {
  const proto = HTMLElement.prototype as unknown as Record<string, unknown>;
  proto.hasPointerCapture ??= () => false;
  proto.setPointerCapture ??= () => {};
  proto.releasePointerCapture ??= () => {};
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

function worktree(path: string, overrides: Partial<GitWorktree> = {}): GitWorktree {
  return {
    path,
    head: "aaaaaaa1111",
    branch: "main",
    detached: false,
    locked: false,
    prunable: false,
    is_main: false,
    is_current: false,
    ...overrides,
  };
}

const main = worktree("/wt-basic", { branch: "main", is_main: true, is_current: true });
const linked = worktree("/wt-basic/.worktrees/feature/x", { branch: "feature/x" });
const detached = worktree("/wt-basic/.worktrees/detached", {
  branch: "",
  detached: true,
  head: "deadbee1234",
});
const missing = worktree("/wt-basic/.worktrees/gone", {
  branch: "gone",
  exists: false,
});
const prunable = worktree("/wt-basic/.worktrees/stale", {
  branch: "stale",
  prunable: true,
});

async function openSelect() {
  const trigger = screen.getByRole("combobox");
  await waitFor(() => expect(trigger).not.toBeDisabled());
  fireEvent.keyDown(trigger, { key: "Enter" });
  return trigger;
}

describe("WorktreeSelect", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("lists the main checkout and every linked checkout, and reports the callback path", async () => {
    mocks.gitWorktrees.mockResolvedValue([main, linked, detached]);
    const onChange = vi.fn();

    render(
      <WorktreeSelect projectPath="/wt-basic" value={main.path} onChange={onChange} />,
    );
    await openSelect();

    const mainOption = await screen.findByRole("option", { name: /main/ });
    expect(mainOption.textContent).toContain("/wt-basic");
    expect(mainOption.getAttribute("title")).toBe("/wt-basic");

    const linkedOption = screen.getByRole("option", { name: /feature\/x/ });
    expect(linkedOption.textContent).toContain("/wt-basic/.worktrees/feature/x");
    expect(linkedOption.getAttribute("title")).toBe(
      "/wt-basic/.worktrees/feature/x",
    );

    expect(
      screen.getByRole("option", { name: /detached deadbee/ }).textContent,
    ).toContain("/wt-basic/.worktrees/detached");

    fireEvent.click(linkedOption);
    expect(onChange).toHaveBeenCalledWith(
      "/wt-basic/.worktrees/feature/x",
    );
  });

  it("disables missing and prunable checkouts without selecting them", async () => {
    mocks.gitWorktrees.mockResolvedValue([main, linked, missing, prunable]);
    const onChange = vi.fn();

    render(<WorktreeSelect projectPath="/wt-disabled" onChange={onChange} />);
    await openSelect();

    const missingOption = await screen.findByRole("option", { name: /missing/ });
    const prunableOption = screen.getByRole("option", { name: /prunable/ });
    expect(missingOption).toHaveAttribute("aria-disabled", "true");
    expect(prunableOption).toHaveAttribute("aria-disabled", "true");
    expect(
      screen.getByRole("option", { name: /feature\/x/ }),
    ).not.toHaveAttribute("aria-disabled", "true");

    fireEvent.click(missingOption);
    fireEvent.click(prunableOption);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("renders an explicit error instead of a different checkout", async () => {
    mocks.gitWorktrees.mockRejectedValue(new Error("not a git repository"));
    const onChange = vi.fn();

    render(<WorktreeSelect projectPath="/wt-error" onChange={onChange} />);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Could not load checkouts: not a git repository",
    );
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(onChange).not.toHaveBeenCalled();
  });

  it("renders an explicit empty state when there are no checkouts", async () => {
    mocks.gitWorktrees.mockResolvedValue([]);

    render(<WorktreeSelect projectPath="/wt-empty" onChange={vi.fn()} />);

    expect(await screen.findByText("No Git checkouts found.")).toBeInTheDocument();
    expect(screen.queryByRole("combobox")).toBeNull();
  });
});
