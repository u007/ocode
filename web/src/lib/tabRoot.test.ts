import { describe, expect, it } from "vitest";
import { tabRoot } from "./tabRoot";

describe("tabRoot", () => {
  it("returns the named checkout root when the tab carries one", () => {
    expect(
      tabRoot({ projectPath: "/repo", worktreePath: "/worktrees/feature" }),
    ).toBe("/worktrees/feature");
  });

  it("falls back to the owning project path for a legacy tab", () => {
    expect(tabRoot({ projectPath: "/repo" })).toBe("/repo");
  });

  it("treats an empty worktree path as absent", () => {
    expect(tabRoot({ projectPath: "/repo", worktreePath: "" })).toBe("/repo");
  });
});
