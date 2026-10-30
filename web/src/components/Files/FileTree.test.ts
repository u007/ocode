import { describe, expect, it } from "vitest";
import { newEntryDir, treePathForRequest } from "./FileTree";

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

describe("newEntryDir", () => {
  // The columns (Miller) view passes ROOT-RELATIVE node paths. A root-level
  // file's parent used to be computed as the file itself
  // (`parentDir("Makefile")` === "Makefile"), so "New file…" sent
  // "Makefile/newfile.txt" and the server refused: the parent was not a
  // directory.
  it("creates beside a root-level file in the project root", () => {
    expect(newEntryDir("/projects/active", "Makefile", false)).toBe("/projects/active");
  });

  it("creates beside a nested file in that file's directory", () => {
    expect(newEntryDir("/projects/active", "src/main.go", false)).toBe("/projects/active/src");
  });

  it("creates inside a focused folder", () => {
    expect(newEntryDir("/projects/active", "src", true)).toBe("/projects/active/src");
  });

  it("creates inside an empty root-level folder without doubling separators", () => {
    expect(newEntryDir("/projects/active/", "docs", true)).toBe("/projects/active/docs");
  });

  // The tree view used to pass a root-anchored path and the columns view a
  // root-relative one; both views now pass the node path through this single
  // helper, so a remote project's "~" root must survive untouched.
  it("keeps a tilde-rooted remote project intact", () => {
    expect(newEntryDir("~/www/aimsweb", "Makefile", false)).toBe("~/www/aimsweb");
    expect(newEntryDir("~/www/aimsweb", "src", true)).toBe("~/www/aimsweb/src");
  });
});
