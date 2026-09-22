import { describe, expect, it } from "vitest";
import {
  isMutatingTool,
  mutatedPathsFromToolCall,
  normalizePreviewPath,
  previewPathMutated,
} from "./previewLiveMutations";

describe("isMutatingTool", () => {
  it("recognizes the file-mutating tool set", () => {
    for (const t of ["write", "edit", "multiedit", "multi_file_edit", "apply_patch", "replace_lines", "delete"]) {
      expect(isMutatingTool(t)).toBe(true);
    }
  });
  it("ignores read-only tools", () => {
    for (const t of ["read", "grep", "rgrep", "bash", "glob", "preview_open", "computer", ""]) {
      expect(isMutatingTool(t)).toBe(false);
    }
  });
});

describe("mutatedPathsFromToolCall", () => {
  it("extracts path for write/edit/replace_lines/delete", () => {
    expect(mutatedPathsFromToolCall("write", '{"path":"a.md","content":"x"}')).toEqual(["a.md"]);
    expect(mutatedPathsFromToolCall("edit", '{"path":"src/a.ts","search":"a","replace":"b"}')).toEqual(["src/a.ts"]);
    expect(mutatedPathsFromToolCall("replace_lines", '{"path":"c.md","start_line":1,"end_line":2}')).toEqual(["c.md"]);
    expect(mutatedPathsFromToolCall("delete", '{"path":"tmp/x"}')).toEqual(["tmp/x"]);
  });

  it("extracts file_path for multiedit and every edits[].path for multi_file_edit", () => {
    expect(mutatedPathsFromToolCall("multiedit", '{"file_path":"m.md","edits":[]}')).toEqual(["m.md"]);
    expect(
      mutatedPathsFromToolCall("multi_file_edit", '{"edits":[{"path":"a.go"},{"path":"b.go"}]}'),
    ).toEqual(["a.go", "b.go"]);
  });

  it("parses apply_patch hunks", () => {
    const patch = [
      "*** Begin Patch",
      "*** Update File: docs/x.md",
      "-old",
      "+new",
      "*** Add File: docs/y.md",
      "+hello",
      "*** Delete File: old.md",
      "*** End Patch",
    ].join("\n");
    expect(mutatedPathsFromToolCall("apply_patch", JSON.stringify({ patchText: patch }))).toEqual([
      "docs/x.md",
      "docs/y.md",
      "old.md",
    ]);
  });

  it("tolerates malformed/absent args", () => {
    expect(mutatedPathsFromToolCall("write", undefined)).toEqual([]);
    expect(mutatedPathsFromToolCall("write", "not-json{")).toEqual([]);
    expect(mutatedPathsFromToolCall("bash", '{"command":"ls"}')).toEqual([]);
  });
});

describe("normalizePreviewPath", () => {
  it("strips the project root and dot segments", () => {
    expect(normalizePreviewPath("/proj/src/a.md", "/proj")).toBe("src/a.md");
    expect(normalizePreviewPath("./src/a.md", "/proj")).toBe("src/a.md");
    expect(normalizePreviewPath("src/./b.md", "/proj")).toBe("src/b.md");
    expect(normalizePreviewPath("src/../src/c.md", "/proj")).toBe("src/c.md");
  });

  it("leaves foreign absolute paths alone (minus normalization)", () => {
    expect(normalizePreviewPath("/other/src/a.md", "/proj")).toBe("other/src/a.md");
  });
});

describe("previewPathMutated", () => {
  it("matches relative preview path vs absolute tool arg", () => {
    expect(previewPathMutated("docs/x.md", ["/Users/j/www/code/docs/x.md"], "/Users/j/www/code")).toBe(true);
  });
  it("matches identical relative paths", () => {
    expect(previewPathMutated("docs/x.md", ["docs/x.md"], "/proj")).toBe(true);
  });
  it("does not match sibling files or other roots", () => {
    expect(previewPathMutated("docs/x.md", ["docs/y.md"], "/proj")).toBe(false);
    expect(previewPathMutated("docs/x.md", ["/other/docs/x.md"], "/proj")).toBe(false);
  });
  it("empty inputs never match", () => {
    expect(previewPathMutated("", ["a.md"], "/proj")).toBe(false);
    expect(previewPathMutated("a.md", [], "/proj")).toBe(false);
  });
});