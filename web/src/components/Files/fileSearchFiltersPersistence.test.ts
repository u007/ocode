import { describe, it, expect, beforeEach } from "vitest";
import {
  loadFileSearchFilters,
  saveFileSearchFilters,
  FILE_SEARCH_FILTERS_DEFAULTS,
} from "./fileSearchFiltersPersistence";

const KEY = "ocode.ui.fileSearchFilters.v1";

describe("fileSearchFiltersPersistence", () => {
  beforeEach(() => {
    window.localStorage.removeItem(KEY);
  });

  it("defaults to all empty/false when nothing stored", () => {
    expect(loadFileSearchFilters("/proj/a")).toEqual(FILE_SEARCH_FILTERS_DEFAULTS);
  });

  it("defaults per-project in same file", () => {
    expect(loadFileSearchFilters("/proj/b")).toEqual(FILE_SEARCH_FILTERS_DEFAULTS);
  });

  it("persists and restores per-project", () => {
    saveFileSearchFilters("/proj/a", {
      exts: "*.go,*.ts",
      ignore: "*.log,dist/**",
      regex: true,
      caseSensitive: true,
      wholeWord: true,
      includeIgnored: true,
    });
    expect(loadFileSearchFilters("/proj/a")).toEqual({
      exts: "*.go,*.ts",
      ignore: "*.log,dist/**",
      regex: true,
      caseSensitive: true,
      wholeWord: true,
      includeIgnored: true,
    });
    // isolation: other project unaffected
    expect(loadFileSearchFilters("/proj/b")).toEqual(FILE_SEARCH_FILTERS_DEFAULTS);
  });

  it("overwrites project entry on second save", () => {
    saveFileSearchFilters("/proj/a", { ...FILE_SEARCH_FILTERS_DEFAULTS, exts: "*.go" });
    saveFileSearchFilters("/proj/a", { ...FILE_SEARCH_FILTERS_DEFAULTS, exts: "*.ts" });
    expect(loadFileSearchFilters("/proj/a").exts).toBe("*.ts");
  });

  it("removes default-valued entries (no bloat)", () => {
    saveFileSearchFilters("/proj/a", { ...FILE_SEARCH_FILTERS_DEFAULTS, exts: "*.go" });
    expect(JSON.parse(window.localStorage.getItem(KEY)!).projects["/proj/a"]).toBeTruthy();
    saveFileSearchFilters("/proj/a", FILE_SEARCH_FILTERS_DEFAULTS);
    const raw = window.localStorage.getItem(KEY);
    const parsed = raw ? JSON.parse(raw) : null;
    expect(parsed?.projects["/proj/a"]).toBeUndefined();
  });

  it("tolerates malformed JSON", () => {
    window.localStorage.setItem(KEY, "not-json");
    expect(loadFileSearchFilters("/proj/a")).toEqual(FILE_SEARCH_FILTERS_DEFAULTS);
  });

  it("tolerates projects as array (typeof array is object)", () => {
    window.localStorage.setItem(KEY, JSON.stringify({ version: 1, projects: [] }));
    expect(loadFileSearchFilters("/proj/a")).toEqual(FILE_SEARCH_FILTERS_DEFAULTS);
    // write after corrupt array should recover
    saveFileSearchFilters("/proj/a", { ...FILE_SEARCH_FILTERS_DEFAULTS, exts: "*.go" });
    expect(loadFileSearchFilters("/proj/a").exts).toBe("*.go");
  });

  it("tolerates wrong version", () => {
    window.localStorage.setItem(KEY, JSON.stringify({ version: 2, projects: { "/proj/a": { exts: "*.go" } } }));
    expect(loadFileSearchFilters("/proj/a")).toEqual(FILE_SEARCH_FILTERS_DEFAULTS);
  });

  it("normalizes partial/corrupt per-project entry", () => {
    window.localStorage.setItem(
      KEY,
      JSON.stringify({ version: 1, projects: { "/proj/a": { exts: 123, regex: "yes" } } }),
    );
    expect(loadFileSearchFilters("/proj/a")).toEqual(FILE_SEARCH_FILTERS_DEFAULTS);
  });

  it("reload on project switch returns correct project", () => {
    saveFileSearchFilters("/proj/a", { ...FILE_SEARCH_FILTERS_DEFAULTS, exts: "*.go" });
    saveFileSearchFilters("/proj/b", { ...FILE_SEARCH_FILTERS_DEFAULTS, ignore: "*.log" });
    expect(loadFileSearchFilters("/proj/a").exts).toBe("*.go");
    expect(loadFileSearchFilters("/proj/a").ignore).toBe("");
    expect(loadFileSearchFilters("/proj/b").exts).toBe("");
    expect(loadFileSearchFilters("/proj/b").ignore).toBe("*.log");
  });

  it("handles undefined projectPath as defaults and no-ops on save", () => {
    expect(loadFileSearchFilters(undefined)).toEqual(FILE_SEARCH_FILTERS_DEFAULTS);
    saveFileSearchFilters(undefined, { ...FILE_SEARCH_FILTERS_DEFAULTS, exts: "*.go" });
    expect(window.localStorage.getItem(KEY)).toBeNull();
  });
});
