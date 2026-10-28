import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import GitForm from "./GitForm";

// Only the two git-config calls are stubbed; the rest of the api module stays
// real so the component's import graph is the production one.
const getGitConfig = vi.fn();
const setGitConfig = vi.fn();
vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getGitConfig: (...args: unknown[]) => getGitConfig(...args),
      setGitConfig: (...args: unknown[]) => setGitConfig(...args),
    },
  };
});

const toggle = () =>
  screen.findByLabelText(/Merge instead of rebase/i) as Promise<HTMLInputElement>;

describe("GitForm pull strategy", () => {
  beforeEach(() => {
    getGitConfig.mockReset();
    setGitConfig.mockReset();
  });

  it("defaults to rebase and saves the rebase strategy", async () => {
    getGitConfig.mockResolvedValue({ pull_strategy: "rebase" });
    setGitConfig.mockResolvedValue({ pull_strategy: "rebase" });

    render(<GitForm />);

    const box = await toggle();
    expect(box.checked).toBe(false);

    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(setGitConfig).toHaveBeenCalledWith("rebase");
    });
  });

  it("saves the merge override when the checkbox is ticked", async () => {
    getGitConfig.mockResolvedValue({ pull_strategy: "rebase" });
    setGitConfig.mockResolvedValue({ pull_strategy: "merge" });

    render(<GitForm />);

    const box = await toggle();
    fireEvent.click(box);
    expect(box.checked).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(setGitConfig).toHaveBeenCalledWith("merge");
    });
  });

  it("reflects a persisted merge override from the server", async () => {
    getGitConfig.mockResolvedValue({ pull_strategy: "merge" });

    render(<GitForm />);

    expect((await toggle()).checked).toBe(true);
  });

  it("shows the effective command so the choice is unambiguous", async () => {
    getGitConfig.mockResolvedValue({ pull_strategy: "rebase" });

    render(<GitForm />);
    await toggle();
    expect(screen.getByText(/git pull --rebase/)).toBeTruthy();

    fireEvent.click(await toggle());
    expect(screen.getByText(/git pull --no-rebase/)).toBeTruthy();
  });

  it("surfaces a failed save instead of silently keeping the old value", async () => {
    getGitConfig.mockResolvedValue({ pull_strategy: "rebase" });
    setGitConfig.mockRejectedValue(new Error("boom"));

    render(<GitForm />);
    await toggle();
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    expect(await screen.findByText("boom")).toBeTruthy();
  });
});
