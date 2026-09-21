import { describe, it, expect, beforeEach } from "vitest";
import {
  fileTreeRootKey,
  loadExpandedDirs,
  saveExpandedDirs,
  FILE_TREE_EXPANSION_STORAGE_KEY,
} from "./fileTreeExpansionPersistence";

const KEY = FILE_TREE_EXPANSION_STORAGE_KEY;

describe("fileTreeExpansionPersistence", () => {
  beforeEach(() => {
    window.localStorage.removeItem(KEY);
  });

  it("returns an empty set when nothing is stored", () => {
    expect(loadExpandedDirs("/proj")).toEqual(new Set());
  });

  it("persists and restores expanded dirs for a root", () => {
    saveExpandedDirs("/proj", ["src", "src/components"]);
    expect(loadExpandedDirs("/proj")).toEqual(new Set(["src", "src/components"]));
  });

  it("isolates roots by path", () => {
    saveExpandedDirs("/proj/a", ["src"]);
    saveExpandedDirs("/proj/b", ["docs"]);
    expect(loadExpandedDirs("/proj/a")).toEqual(new Set(["src"]));
    expect(loadExpandedDirs("/proj/b")).toEqual(new Set(["docs"]));
  });

  it("isolates the same path on different hosts", () => {
    saveExpandedDirs(fileTreeRootKey("~/www/app", "me@host-a", "~/www/app"), ["src"]);
    saveExpandedDirs(fileTreeRootKey("~/www/app", "me@host-b", "~/www/app"), ["docs"]);
    expect(loadExpandedDirs(fileTreeRootKey("~/www/app", "me@host-a", "~/www/app"))).toEqual(
      new Set(["src"]),
    );
    expect(loadExpandedDirs(fileTreeRootKey("~/www/app", "me@host-b", "~/www/app"))).toEqual(
      new Set(["docs"]),
    );
  });

  it("removes the entry when nothing is expanded", () => {
    saveExpandedDirs("/proj", ["src"]);
    saveExpandedDirs("/proj", []);
    expect(loadExpandedDirs("/proj")).toEqual(new Set());
    expect(window.localStorage.getItem(KEY)).not.toContain("/proj");
  });

  it("dedupes and ignores non-string/empty paths", () => {
    saveExpandedDirs("/proj", ["src", "src", "", null as unknown as string]);
    expect(loadExpandedDirs("/proj")).toEqual(new Set(["src"]));
  });

  it("ignores unknown keys and null keys", () => {
    expect(loadExpandedDirs(null)).toEqual(new Set());
    expect(fileTreeRootKey(undefined, undefined, undefined)).toBeNull();
    saveExpandedDirs(null, ["src"]);
    expect(window.localStorage.getItem(KEY)).toBeNull();
  });

  it("uses the explicit root over the project path", () => {
    expect(fileTreeRootKey("/proj", undefined, "/extra")).toBe("/extra");
    expect(fileTreeRootKey("/proj", undefined, "/proj/")).toBe("/proj");
    expect(fileTreeRootKey("/proj", "h", "/proj")).toBe("h\u0000/proj");
  });

  it("tolerates a malformed stored file", () => {
    window.localStorage.setItem(KEY, "{not json");
    expect(loadExpandedDirs("/proj")).toEqual(new Set());
    window.localStorage.setItem(KEY, JSON.stringify({ version: 99, roots: { "/proj": ["src"] } }));
    expect(loadExpandedDirs("/proj")).toEqual(new Set());
    window.localStorage.setItem(KEY, JSON.stringify({ version: 1, roots: { "/proj": "nope" } }));
    expect(loadExpandedDirs("/proj")).toEqual(new Set());
  });
});
