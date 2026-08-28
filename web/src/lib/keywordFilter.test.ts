import { describe, expect, it } from "vitest";
import { parseKeywords, matchesKeywords } from "./keywordFilter";

describe("parseKeywords", () => {
  it("splits on whitespace", () => {
    expect(parseKeywords("foo bar")).toEqual(["foo", "bar"]);
  });
  it("trims and lowercases", () => {
    expect(parseKeywords("  Foo  BAR  ")).toEqual(["foo", "bar"]);
  });
  it("handles multiple spaces and newlines", () => {
    expect(parseKeywords("a  b\tc\n d")).toEqual(["a", "b", "c", "d"]);
  });
  it("returns empty for blank", () => {
    expect(parseKeywords("   ")).toEqual([]);
  });
});

describe("matchesKeywords", () => {
  it("matches case-insensitive", () => {
    expect(matchesKeywords("Hello World", ["hello"])).toBe(true);
    expect(matchesKeywords("Hello World", ["HELLO"])).toBe(true);
    expect(matchesKeywords("Hello World", ["world"])).toBe(true);
  });
  it("requires all keywords (AND)", () => {
    expect(matchesKeywords("src/components/FileTree.tsx", ["file", "tree"])).toBe(true);
    expect(matchesKeywords("src/components/FileTree.tsx", ["file", "missing"])).toBe(false);
  });
  it("matches name and path uniformly", () => {
    expect(matchesKeywords("assets/photo.png image/png", ["photo"])).toBe(true);
    expect(matchesKeywords("assets/photo.png image/png", ["png"])).toBe(true);
    expect(matchesKeywords("assets/photo.png image/png", ["assets"])).toBe(true);
  });
  it("supports path fragments", () => {
    expect(matchesKeywords("a/b/c.ts", ["b/c"])).toBe(true);
  });
  it("returns true for empty keywords", () => {
    expect(matchesKeywords("anything", [])).toBe(true);
  });
});
