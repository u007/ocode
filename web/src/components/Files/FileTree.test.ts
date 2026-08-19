import { describe, expect, it } from "vitest";
import { treePathForRequest } from "./FileTree";

describe("treePathForRequest", () => {
  it("anchors relative node paths to the selected project", () => {
    expect(treePathForRequest("/projects/active", "src/components")).toBe(
      "/projects/active/src/components",
    );
  });

  it("preserves relative paths when the server default root is used", () => {
    expect(treePathForRequest(undefined, "src")).toBe("src");
  });

  it("does not create duplicate separators at the root boundary", () => {
    expect(treePathForRequest("/projects/active/", "/src")).toBe("/projects/active/src");
  });
});
