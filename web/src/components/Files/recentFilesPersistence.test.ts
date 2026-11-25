import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import {
  loadRecentFiles,
  recordRecentFile,
  rankByRecent,
  RECENT_FILES_LIMIT,
  RECENT_FILES_STORAGE_PREFIX,
} from "./recentFilesPersistence";

const A = "/proj/a";
const B = "/proj/b";

describe("recentFilesPersistence", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("dedupes and moves a re-opened file to the front", () => {
    recordRecentFile(A, "src/a.ts");
    recordRecentFile(A, "src/b.ts");
    recordRecentFile(A, "src/a.ts");
    expect(loadRecentFiles(A)).toEqual(["src/a.ts", "src/b.ts"]);
  });

  it("caps the list at the limit, dropping the oldest", () => {
    for (let i = 0; i < RECENT_FILES_LIMIT + 10; i++) {
      recordRecentFile(A, `f${i}.ts`);
    }
    const list = loadRecentFiles(A);
    expect(list).toHaveLength(RECENT_FILES_LIMIT);
    expect(list[0]).toBe(`f${RECENT_FILES_LIMIT + 9}.ts`);
    expect(list).not.toContain("f0.ts");
  });

  it("isolates recent files per project key", () => {
    recordRecentFile(A, "src/a.ts");
    recordRecentFile(B, "src/b.ts");
    expect(loadRecentFiles(A)).toEqual(["src/a.ts"]);
    expect(loadRecentFiles(B)).toEqual(["src/b.ts"]);
    expect(loadRecentFiles("/proj/none")).toEqual([]);
  });

  it("returns [] and warns on corrupt JSON", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    window.localStorage.setItem(`${RECENT_FILES_STORAGE_PREFIX}${A}`, "{not json");
    expect(loadRecentFiles(A)).toEqual([]);
    expect(warn).toHaveBeenCalled();
  });

  it("returns [] and warns when the stored value is not a string array", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    window.localStorage.setItem(`${RECENT_FILES_STORAGE_PREFIX}${A}`, JSON.stringify({ path: "x" }));
    expect(loadRecentFiles(A)).toEqual([]);
    window.localStorage.setItem(`${RECENT_FILES_STORAGE_PREFIX}${A}`, JSON.stringify(["ok", 3]));
    expect(loadRecentFiles(A)).toEqual([]);
    expect(warn).toHaveBeenCalledTimes(2);
  });
});

describe("rankByRecent", () => {
  const paths = ["readme.md", "src/alpha.ts", "src/beta.ts", "src/gamma.ts"];

  it("moves recent members to the front in recency order", () => {
    expect(rankByRecent(paths, ["src/gamma.ts", "src/beta.ts"])).toEqual([
      "src/gamma.ts",
      "src/beta.ts",
      "readme.md",
      "src/alpha.ts",
    ]);
  });

  it("returns the list unchanged when nothing recent is present", () => {
    expect(rankByRecent(paths, [])).toEqual(paths);
    expect(rankByRecent(paths, ["gone/deleted.ts"])).toEqual(paths);
  });

  it("ignores recent paths that are not in the list", () => {
    expect(rankByRecent(paths, ["gone/deleted.ts", "src/beta.ts"])).toEqual([
      "src/beta.ts",
      "readme.md",
      "src/alpha.ts",
      "src/gamma.ts",
    ]);
  });

  it("normalizes absolute recent paths under the project root", () => {
    expect(rankByRecent(paths, ["/proj/src/gamma.ts"], "/proj")).toEqual([
      "src/gamma.ts",
      "readme.md",
      "src/alpha.ts",
      "src/beta.ts",
    ]);
  });

  it("drops absolute recent paths outside the project root", () => {
    expect(rankByRecent(paths, ["/elsewhere/src/gamma.ts"], "/proj")).toEqual(paths);
  });

  it("counts a file once when it is recorded in both spellings", () => {
    expect(rankByRecent(paths, ["/proj/src/beta.ts", "src/beta.ts", "readme.md"], "/proj")).toEqual([
      "src/beta.ts",
      "readme.md",
      "src/alpha.ts",
      "src/gamma.ts",
    ]);
  });

  it("never moves an excluded path (folders)", () => {
    expect(rankByRecent(paths, ["src/beta.ts"], undefined, new Set(["src/beta.ts"]))).toEqual(paths);
  });
});
