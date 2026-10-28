import type { GhosttyCell } from "ghostty-web";
import { describe, expect, it } from "vitest";
import { ghosttyCellsToText } from "./pulseScreenText";

function cell(codepoint: number, over: Partial<GhosttyCell> = {}): GhosttyCell {
  return {
    codepoint,
    fg_r: 0,
    fg_g: 0,
    fg_b: 0,
    bg_r: 0,
    bg_g: 0,
    bg_b: 0,
    flags: 0,
    width: 1,
    hyperlink_id: 0,
    grapheme_len: 0,
    ...over,
  };
}

const noGrapheme = (col: number): string => {
  throw new Error(`unexpected grapheme read at ${col}`);
};

describe("ghosttyCellsToText", () => {
  it("renders codepoints and treats empty cells as spaces", () => {
    const cells = [cell(0x68), cell(0), cell(0x69)];
    expect(ghosttyCellsToText(cells, noGrapheme)).toBe("h i");
  });

  it("trims trailing blanks but keeps interior ones", () => {
    const cells = [cell(0x61), cell(0), cell(0x62), cell(0), cell(0)];
    expect(ghosttyCellsToText(cells, noGrapheme)).toBe("a b");
  });

  it("skips the width-0 tail of a wide character", () => {
    const cells = [cell(0x4e2d, { width: 2 }), cell(0, { width: 0 }), cell(0x41)];
    expect(ghosttyCellsToText(cells, noGrapheme)).toBe("中A");
  });

  it("reads multi-codepoint graphemes through graphemeAt", () => {
    const cells = [cell(0x65, { grapheme_len: 1 }), cell(0x78)];
    const reads: number[] = [];
    const text = ghosttyCellsToText(cells, (col) => {
      reads.push(col);
      return "é";
    });
    expect(text).toBe("éx");
    expect(reads).toEqual([0]);
  });

  it("returns an empty string for an all-blank row", () => {
    expect(ghosttyCellsToText([cell(0), cell(0)], noGrapheme)).toBe("");
  });
});
