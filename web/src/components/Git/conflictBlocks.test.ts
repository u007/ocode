import { describe, expect, it } from "vitest";
import { parseBlocks, resolveBlockText } from "./conflictBlocks";

const text = ["a", "<<<<<<< HEAD", "o1", "o2", "=======", "t1", ">>>>>>> x", "b", "<<<<<<< HEAD", "o", "||||||| base", "bb", "=======", "t", ">>>>>>> x"].join("\n");

describe("conflict editor blocks", () => {
  it("finds every block, diff3 included", () => {
    expect(parseBlocks(text)).toEqual([
      { start: 2, base: 0, sep: 5, end: 7 },
      { start: 9, base: 11, sep: 13, end: 15 },
    ]);
  });
  it("resolves each choice", () => {
    const lines = text.split("\n");
    const [b1, b2] = parseBlocks(text);
    expect(resolveBlockText(lines, b1, "ours")).toEqual(["o1", "o2"]);
    expect(resolveBlockText(lines, b1, "theirs")).toEqual(["t1"]);
    expect(resolveBlockText(lines, b1, "both")).toEqual(["o1", "o2", "t1"]);
    expect(resolveBlockText(lines, b2, "ours")).toEqual(["o"]);
  });
});
