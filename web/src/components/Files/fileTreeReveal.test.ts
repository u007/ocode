import { describe, it, expect } from "vitest";
import {
  ancestorDirs,
  hiddenTreeSegment,
  normalizeTreePath,
  sameTreeRoot,
  treeDirOf,
  treeRelativePath,
} from "./fileTreeReveal";

describe("normalizeTreePath", () => {
  it("canonicalizes separators, duplicate slashes, './' and trailing slashes", () => {
    expect(normalizeTreePath("src\\app\\a.ts")).toBe("src/app/a.ts");
    expect(normalizeTreePath("src//app///a.ts")).toBe("src/app/a.ts");
    expect(normalizeTreePath("./src/app/")).toBe("src/app");
    expect(normalizeTreePath("  src/app  ")).toBe("src/app");
    expect(normalizeTreePath("/proj/")).toBe("/proj");
  });

  it("is identity for a plain tree path", () => {
    expect(normalizeTreePath("src/app/a.ts")).toBe("src/app/a.ts");
  });
});

describe("treeDirOf", () => {
  it("returns the parent directory, and '' for a direct child of the root", () => {
    expect(treeDirOf("src/app/a.ts")).toBe("src/app");
    expect(treeDirOf("a.ts")).toBe("");
    expect(treeDirOf("")).toBe("");
  });
});

describe("ancestorDirs", () => {
  it("lists every ancestor outermost first", () => {
    expect(ancestorDirs("src/app/deep/a.ts")).toEqual(["src", "src/app", "src/app/deep"]);
  });

  it("is empty for a root-level file and for an empty path", () => {
    expect(ancestorDirs("a.ts")).toEqual([]);
    expect(ancestorDirs("")).toEqual([]);
  });
});

describe("treeRelativePath", () => {
  it("strips an absolute path's root", () => {
    expect(treeRelativePath("/proj/src/app/a.ts", "/proj")).toBe("src/app/a.ts");
    expect(treeRelativePath("/proj/src/", "/proj")).toBe("src");
  });

  it("passes an already-relative path through", () => {
    expect(treeRelativePath("src/app/a.ts", "/proj")).toBe("src/app/a.ts");
  });

  it("handles Windows drive roots and separators", () => {
    expect(treeRelativePath("C:\\proj\\src\\a.ts", "c:/proj")).toBe("src/a.ts");
    expect(treeRelativePath("C:/proj/src/a.ts", "C:\\proj\\")).toBe("src/a.ts");
  });

  it("returns null for a path outside the root, and for the root itself", () => {
    expect(treeRelativePath("/other/src/a.ts", "/proj")).toBeNull();
    // A shared prefix that is not a path boundary must not be stripped.
    expect(treeRelativePath("/project/a.ts", "/proj")).toBeNull();
    expect(treeRelativePath("/proj", "/proj")).toBeNull();
  });

  it("treats an absolute path against a ~-rooted remote project as outside", () => {
    // The tree cannot expand a home-relative root into an absolute remote path,
    // so this must report "outside" rather than reveal the wrong row.
    expect(treeRelativePath("/home/james/www/app/src/a.ts", "~/www/app")).toBeNull();
    expect(treeRelativePath("src/a.ts", "~/www/app")).toBe("src/a.ts");
  });

  it("rejects empty input and accepts a missing root", () => {
    expect(treeRelativePath("", "/proj")).toBeNull();
    expect(treeRelativePath("src/a.ts")).toBe("src/a.ts");
  });
});

describe("hiddenTreeSegment", () => {
  it("flags a dot directory or dot file in any position", () => {
    expect(hiddenTreeSegment(".github/workflows/ci.yml")).toBe(".github");
    expect(hiddenTreeSegment("src/.env")).toBe(".env");
  });

  it("flags the server's ignored directories", () => {
    expect(hiddenTreeSegment("node_modules/pkg/index.js")).toBe("node_modules");
    expect(hiddenTreeSegment("web/dist/bundle.js")).toBe("dist");
  });

  it("returns null for a fully visible path", () => {
    expect(hiddenTreeSegment("src/app/a.ts")).toBeNull();
    expect(hiddenTreeSegment("")).toBeNull();
  });
});

describe("sameTreeRoot", () => {
  it("compares normalized forms", () => {
    expect(sameTreeRoot("/proj/", "/proj")).toBe(true);
    expect(sameTreeRoot("/proj", "/other")).toBe(false);
    expect(sameTreeRoot(undefined, undefined)).toBe(true);
    expect(sameTreeRoot("/proj", undefined)).toBe(false);
  });

  it("compares Windows drive roots case-insensitively but POSIX roots exactly", () => {
    expect(sameTreeRoot("C:\\Proj", "c:/proj")).toBe(true);
    expect(sameTreeRoot("/Proj", "/proj")).toBe(false);
  });
});