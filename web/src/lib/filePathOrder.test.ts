import { describe, expect, it } from "vitest";
import { compareByShortestPath, pathSegmentCount } from "./filePathOrder";

describe("pathSegmentCount", () => {
  it("counts segments of relative paths", () => {
    expect(pathSegmentCount("main.go")).toBe(1);
    expect(pathSegmentCount("internal/tui/model.go")).toBe(3);
  });
  it("ignores leading ./ and separators", () => {
    expect(pathSegmentCount("./web/src/App.tsx")).toBe(3);
    expect(pathSegmentCount("/abs/path/file.go")).toBe(3);
    expect(pathSegmentCount("internal//tui/model.go")).toBe(3);
  });
  it("normalises backslash separators", () => {
    expect(pathSegmentCount("web\\src\\App.tsx")).toBe(3);
  });
  it("treats empty/blank as zero", () => {
    expect(pathSegmentCount("")).toBe(0);
    expect(pathSegmentCount("./")).toBe(0);
  });
});

describe("compareByShortestPath", () => {
  it("sorts fewer segments first", () => {
    const out = ["a/b/c/deep.go", "web/src/App.tsx", "README.md"].sort(compareByShortestPath);
    expect(out).toEqual(["README.md", "web/src/App.tsx", "a/b/c/deep.go"]);
  });
  it("falls back to lexicographic for equal depth", () => {
    const out = ["internal/zzz.go", "internal/aaa.go"].sort(compareByShortestPath);
    expect(out).toEqual(["internal/aaa.go", "internal/zzz.go"]);
  });
});
